package agent

import (
	"context"
	"errors"
	"strings"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agentrouting "ququchat/internal/taskservice/task/agent/routing"
	"ququchat/internal/taskservice/task/agent/toolruntime"
	"ququchat/internal/taskservice/task/agent/toolruntime/tools"
)

type ChatClient interface {
	Chat(ctx context.Context, prompt string) (string, error)
}

type chatUsageClient interface {
	ChatWithUsage(ctx context.Context, prompt string) (string, int, int, int, error)
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

func chatWithUsage(ctx context.Context, client ChatClient, prompt string) (string, agentmemory.TokenUsage, error) {
	if usageClient, ok := client.(chatUsageClient); ok {
		text, promptTokens, completionTokens, totalTokens, err := usageClient.ChatWithUsage(ctx, prompt)
		usage := agentmemory.TokenUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
		}
		return text, usage, err
	}
	text, err := client.Chat(ctx, prompt)
	return text, agentmemory.TokenUsage{}, err
}

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
	if input.WikiOnlyMode {
		availableToolSpecs = []ToolSpec{
			{Name: "wiki_list_files", Description: "列出 wiki 目录中的 .md 文件", Parameters: map[string]any{"type": "object", "properties": map[string]any{"dir": map[string]any{"type": "string"}}, "additionalProperties": false}},
			{Name: "wiki_read_file", Description: "读取 wiki 文件内容", Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}, "additionalProperties": false}},
			{Name: "wiki_write_file", Description: "写入 wiki 文件", Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "required": []string{"path", "content"}, "additionalProperties": false}},
			{Name: "finish", Description: "完成任务", Parameters: map[string]any{"type": "object", "properties": map[string]any{"final": map[string]any{"type": "string"}}, "required": []string{"final"}, "additionalProperties": false}},
		}
	} else if input.WikiReadOnlyMode {
		availableToolSpecs = []ToolSpec{
			{Name: "wiki_list_files", Description: "列出 wiki 目录中的 .md 文件", Parameters: map[string]any{"type": "object", "properties": map[string]any{"dir": map[string]any{"type": "string"}}, "additionalProperties": false}},
			{Name: "wiki_read_file", Description: "读取 wiki 文件内容", Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}, "additionalProperties": false}},
			{Name: "finish", Description: "完成任务", Parameters: map[string]any{"type": "object", "properties": map[string]any{"final": map[string]any{"type": "string"}}, "required": []string{"final"}, "additionalProperties": false}},
		}
	}
	routingService := newRoutingService(client)
	toolRunner := newToolRuntime(client, input)
	memorySession := agentmemory.NewFacade().NewSession(agentmemory.SessionInput{
		RoomID:                 strings.TrimSpace(input.RoomID),
		Goal:                   goal,
		RecentMessages:         append([]string(nil), recentMessages...),
		MaxRecent:              readRecentDefaultLimit,
		FeedbackOutputMaxChars: feedbackOutputMaxChars,
		OnObservation: func(obs agentmemory.Observation) {
			if input.OnObservation == nil {
				return
			}
			input.OnObservation(ObservationEvent{
				RequestID:        strings.TrimSpace(input.RequestID),
				TaskID:           strings.TrimSpace(input.TaskID),
				RoomID:           strings.TrimSpace(input.RoomID),
				UserID:           strings.TrimSpace(input.UserID),
				Step:             obs.Step,
				Role:             strings.TrimSpace(obs.Role),
				Tool:             strings.TrimSpace(obs.Tool),
				Status:           strings.TrimSpace(obs.Status),
				Input:            strings.TrimSpace(obs.Input),
				Output:           strings.TrimSpace(obs.Output),
				RawOutput:        strings.TrimSpace(obs.RawOutput),
				Error:            strings.TrimSpace(obs.Error),
				DurationMs:       obs.DurationMs,
				PromptTokens:     obs.PromptTokens,
				CompletionTokens: obs.CompletionTokens,
				TotalTokens:      obs.TotalTokens,
			})
		},
	})
	memorySession.AppendMessage(agentmemory.Message{Role: "user", Content: goal})
	wikiContext := ""
	if input.WikiQueryFunc != nil {
		wikiContext = input.WikiQueryFunc(ctx, goal)
	} else if input.WikiStore != nil {
		wikiContext = input.WikiStore.QueryContext(strings.TrimSpace(input.RoomID), goal)
	}
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
			WikiContext:        wikiContext,
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
			{From: "executor", Event: "executor.skill_loaded", To: "coordinator_think"},
			{From: "executor", Event: "executor.skill_context", To: "planner"},
			{From: "final_judge", Event: "final_judge.done", To: "end"},
			{From: "final_judge", Event: "final_judge.retry", To: "coordinator_think"},
		},
	)
	policy := agentrouting.NewAgentPolicy("planner", map[string]int{
		"validator": roleRetryLimit,
	})
	return agentrouting.NewService(router, policy)
}

func newToolRuntime(client ChatClient, input Input) toolruntime.Runtime {
	cfg := toolruntime.Config{
		RAGSearchTopK:   ragSearchTopK,
		RAGSearchVector: ragSearchVector,
	}
	deps := toolruntime.Deps{
		RAGSearch:              toolruntime.RAGSearchFunc(input.RAGSearch),
		AIGCGenerate:           toolruntime.AIGCGenerateFunc(input.AIGCGenerate),
		MCPCallByQualifiedName: toolruntime.MCPCallFunc(input.MCPCallToolByQualifiedName),
	}
	if input.WikiOnlyMode && input.WikiDir != "" {
		return toolruntime.New(cfg, deps,
			tools.NewWikiReadTool(input.WikiDir),
			tools.NewWikiWriteTool(input.WikiDir),
			tools.NewWikiListTool(input.WikiDir),
			tools.NewFinishTool(),
		)
	}
	if input.WikiReadOnlyMode && input.WikiDir != "" {
		return toolruntime.New(cfg, deps,
			tools.NewWikiReadTool(input.WikiDir),
			tools.NewWikiListTool(input.WikiDir),
			tools.NewFinishTool(),
		)
	}
	ts := []toolruntime.Tool{
		tools.NewRAGSearchTool(cfg, deps),
		tools.NewGenerateImageTool(deps),
		tools.NewGetCurrentTimeTool(),
		tools.NewReplanTool(),
		tools.NewFinishTool(),
		tools.NewSkillTool(""),
		tools.NewBashTool(nil),
		tools.NewSubAgentTool(input.AgentDepth, func(ctx context.Context, goal string) (string, error) {
			return Execute(ctx, client, Input{
				Goal:       goal,
				RoomID:     input.RoomID,
				UserID:     input.UserID,
				MaxSteps:   maxStepsDefault,
				AgentDepth: input.AgentDepth + 1,
				WikiStore:  input.WikiStore,
				WikiDir:    input.WikiDir,
				RAGSearch:  input.RAGSearch,
				AIGCGenerate:               input.AIGCGenerate,
				MCPCallToolByQualifiedName: input.MCPCallToolByQualifiedName,
			})
		}),
	}
	return toolruntime.New(cfg, deps, ts...)
}
