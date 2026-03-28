package agent

import (
	"context"
	"errors"
	"strings"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agentrouting "ququchat/internal/taskservice/task/agent/routing"
	"ququchat/internal/taskservice/task/agent/toolruntime"
)

type ChatClient interface {
	Chat(ctx context.Context, prompt string) (string, error)
}

type RAGSearchTool func(ctx context.Context, roomID string, query string, topK int, vector string) (string, error)
type AIGCTool func(ctx context.Context, prompt string) (string, error)
type MCPCallToolByQualifiedName func(ctx context.Context, qualifiedToolName string, arguments map[string]any) (string, error)

const (
	maxStepsDefault        = 4
	maxStepsLimit          = 20
	roleRetryLimit         = 2
	finalScorePass         = 50
	readRecentDefaultLimit = 10
	feedbackOutputMaxChars = 4000
	ragSearchTopK          = 3
	ragSearchVector        = "summary"
)

func Execute(ctx context.Context, client ChatClient, input Input) (string, error) {
	if client == nil {
		return "", errors.New("llm client is not configured")
	}
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		return "", errors.New("agent goal is required")
	}
	recentMessages := agentmemory.NormalizeRecentMessages(input.RecentMessages)
	maxSteps := input.MaxSteps
	if maxSteps <= 0 {
		maxSteps = maxStepsDefault
	}
	if maxSteps > maxStepsLimit {
		maxSteps = maxStepsLimit
	}
	availableToolSpecs := listToolSpecsWithDynamic(input.DynamicToolSpecs)
	toolRunner := newToolRuntime(input)
	memorySession := agentmemory.NewFacade().NewSession(agentmemory.SessionInput{
		RoomID:                 strings.TrimSpace(input.RoomID),
		Goal:                   goal,
		RecentMessages:         append([]string(nil), recentMessages...),
		MaxRecent:              readRecentDefaultLimit,
		FeedbackOutputMaxChars: feedbackOutputMaxChars,
	})
	state := &State{
		DomainState: DomainState{
			Goal:               goal,
			RoomID:             strings.TrimSpace(input.RoomID),
			RecentMessages:     append([]string(nil), recentMessages...),
			Outline:            plannerOutline{Steps: nil},
			OutlineIndex:       0,
			CurrentTask:        "",
			CoordinatorThought: "",
			Feedback:           strings.TrimSpace(memorySession.BuildFeedback()),
			URLAliasToValue:    map[string]string{},
			URLValueToAlias:    map[string]string{},
			URLAliasOrder:      make([]string, 0),
			NextURLAliasIndex:  1,
			AvailableToolSpecs: availableToolSpecs,
			MemorySession:      memorySession,
			ToolRuntime:        toolRunner,
		},
		ControlState: ControlState{
			CurrentNode: "planner",
			LastEvent:   "",
			Retry:       map[string]int{},
			Step:        0,
			MaxSteps:    maxSteps,
			Done:        false,
			Failed:      false,
			FailReason:  "",
		},
	}
	routingService := newRoutingService(client)
	if runErr := routingService.Run(ctx, state); runErr != nil {
		reason := strings.TrimSpace(state.FailReason)
		if reason == "" {
			reason = runErr.Error()
		}
		return "", errors.New(agentmemory.FormatFailureReport(memorySession.Trace(), reason))
	}
	finalAnswer := strings.TrimSpace(state.FinalAnswer)
	if finalAnswer == "" {
		return "", errors.New(agentmemory.FormatFailureReport(memorySession.Trace(), "流程结束但未生成最终答案"))
	}
	return agentmemory.FormatSuccessReport(memorySession.Trace(), finalAnswer), nil
}

func newRoutingService(client ChatClient) *agentrouting.Service {
	router := agentrouting.NewRouter(
		[]agentrouting.Node{
			agentrouting.NewFuncNode("planner", func(ctx context.Context, state *State) (string, error) {
				return RunPlannerNode(ctx, client, state)
			}),
			agentrouting.NewFuncNode("coordinator_think", func(ctx context.Context, state *State) (string, error) {
				return RunCoordinatorThinkNode(ctx, client, state)
			}),
			agentrouting.NewFuncNode("coordinator_act", func(ctx context.Context, state *State) (string, error) {
				return RunCoordinatorActNode(ctx, client, state)
			}),
			agentrouting.NewFuncNode("formatter", func(ctx context.Context, state *State) (string, error) {
				return RunFormatterNode(ctx, client, state)
			}),
			agentrouting.NewFuncNode("validator", func(ctx context.Context, state *State) (string, error) {
				return RunValidatorNode(ctx, state)
			}),
			agentrouting.NewFuncNode("executor", func(ctx context.Context, state *State) (string, error) {
				return RunExecutorNode(ctx, state)
			}),
			agentrouting.NewFuncNode("tool_response", func(ctx context.Context, state *State) (string, error) {
				return RunToolResponseNode(ctx, client, state)
			}),
			agentrouting.NewFuncNode("termination_guard", func(ctx context.Context, state *State) (string, error) {
				return RunTerminationGuardNode(ctx, client, state)
			}),
			agentrouting.NewFuncNode("final_think", func(ctx context.Context, state *State) (string, error) {
				return RunFinalThinkNode(ctx, client, state)
			}),
			agentrouting.NewFuncNode("final_judge", func(ctx context.Context, state *State) (string, error) {
				return RunFinalJudgeNode(ctx, client, state)
			}),
		},
		[]agentrouting.Transition{
			{From: "planner", Event: "planner.done", To: "coordinator_think"},
			{From: "coordinator_think", Event: "coordinator.think_done", To: "coordinator_act"},
			{From: "coordinator_act", Event: "coordinator.act_done", To: "executor"},
			{From: "executor", Event: "executor.done_mcp", To: "tool_response"},
			{From: "executor", Event: "executor.done_local", To: "termination_guard"},
			{From: "tool_response", Event: "tool_response.done", To: "termination_guard"},
			{From: "termination_guard", Event: "termination_guard.can_finish", To: "final_think"},
			{From: "termination_guard", Event: "termination_guard.continue", To: "coordinator_think"},
			{From: "final_think", Event: "final_think.done", To: "final_judge"},
			{From: "final_think", Event: "final_think.retry", To: "coordinator_think"},
			{From: "executor", Event: "executor.failed", To: "coordinator_think"},
			{From: "executor", Event: "executor.finish", To: "final_judge"},
			{From: "executor", Event: "executor.replan", To: "planner"},
			{From: "final_judge", Event: "final_judge.done", To: "end"},
			{From: "final_judge", Event: "final_judge.retry", To: "coordinator_think"},
		},
	)
	policy := agentrouting.NewAgentPolicy("planner", map[string]int{
		"validator": roleRetryLimit,
	})
	return agentrouting.NewService(router, policy)
}

func newToolRuntime(input Input) toolruntime.Runtime {
	return toolruntime.New(
		toolruntime.Config{
			RAGSearchTopK:   ragSearchTopK,
			RAGSearchVector: ragSearchVector,
		},
		toolruntime.Deps{
			RAGSearch:              toolruntime.RAGSearchFunc(input.RAGSearch),
			AIGCGenerate:           toolruntime.AIGCGenerateFunc(input.AIGCGenerate),
			MCPCallByQualifiedName: toolruntime.MCPCallFunc(input.MCPCallToolByQualifiedName),
		},
	)
}
