package agent

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agentservices "ququchat/internal/taskservice/task/agent/services"
	agenttypes "ququchat/internal/taskservice/task/agent/types"
)

type terminationGuardDecision struct {
	CanFinish bool   `json:"can_finish"`
	Reason    string `json:"reason"`
}

type terminationGuardSnapshot struct {
	ToolName      string
	ActionInput   string
	ToolOutput    string
	ToolOutputRaw string
	ToolError     string
}

type terminationGuardVoteResult struct {
	Decision terminationGuardDecision
	Valid    bool
}

const terminationGuardJudgeCount = 3

func RunTerminationGuardNode(ctx context.Context, client ChatClient, state *State) (next string, err error) {
	if client == nil {
		return "", errors.New("termination_guard node client is not configured")
	}
	if state == nil {
		return "", errors.New("termination_guard node state is nil")
	}
	goal := strings.TrimSpace(state.Goal)
	if goal == "" {
		return "", errors.New("agent goal is required")
	}
	snapshot := terminationGuardSnapshot{
		ToolName:      state.ToolName,
		ActionInput:   state.ActionInput,
		ToolOutput:    state.ToolOutput,
		ToolOutputRaw: state.ToolOutputRaw,
		ToolError:     state.ToolError,
	}
	trace := []agentmemory.Observation(nil)
	if state.MemorySession != nil {
		trace = state.MemorySession.Trace()
	}
	prompt := agentservices.BuildTerminationGuardPrompt(agenttypes.TerminationGuardPromptInput{
		Goal:               goal,
		RecentMessagesText: agentmemory.BuildRecentMessagesSnippet(state.RecentMessages, 8),
		TraceText:          agentmemory.BuildTraceSnippet(trace, 8),
		ToolOutputRaw:      strings.TrimSpace(state.ToolOutputRaw),
		ToolOutputSummary:  strings.TrimSpace(state.ToolOutput),
	})
	votes := collectTerminationGuardVotes(ctx, client, prompt, terminationGuardJudgeCount)
	validVotes := 0
	canFinishVotes := 0
	reasons := make([]string, 0, len(votes))
	for _, vote := range votes {
		if !vote.Valid {
			continue
		}
		validVotes++
		if strings.TrimSpace(vote.Decision.Reason) != "" {
			reasons = append(reasons, strings.TrimSpace(vote.Decision.Reason))
		}
		if vote.Decision.CanFinish {
			canFinishVotes++
		}
	}
	canFinishByVote := validVotes > 0 && canFinishVotes*2 > validVotes
	if !canFinishByVote {
		restoreTerminationGuardSnapshot(state, snapshot)
		return "termination_guard.continue", nil
	}
	if state.MemorySession != nil {
		reasonText := ""
		if len(reasons) > 0 {
			reasonText = reasons[0]
		}
		if reasonText == "" {
			reasonText = "多数投票判定可以提前结束"
		}
		reasonText = "votes=" + strconv.Itoa(canFinishVotes) + "/" + strconv.Itoa(validVotes) + "；" + reasonText
		state.MemorySession.AppendObservation(agentmemory.Observation{
			Step:   state.Step,
			Role:   "TerminationGuard",
			Tool:   "termination_guard",
			Input:  agentmemory.ShortText(prompt, 220),
			Output: agentmemory.ShortText(strings.TrimSpace(reasonText), 220),
			Status: "succeeded",
		})
	}
	return "termination_guard.can_finish", nil
}

func collectTerminationGuardVotes(ctx context.Context, client ChatClient, prompt string, judgeCount int) []terminationGuardVoteResult {
	if judgeCount <= 0 {
		return nil
	}
	results := make([]terminationGuardVoteResult, judgeCount)
	var waitGroup sync.WaitGroup
	waitGroup.Add(judgeCount)
	for i := 0; i < judgeCount; i++ {
		index := i
		go func() {
			defer waitGroup.Done()
			raw, chatErr := client.Chat(ctx, prompt)
			if chatErr != nil {
				return
			}
			decision := terminationGuardDecision{}
			if err := agentservices.DecodeJSONFromText(raw, &decision); err != nil {
				return
			}
			results[index] = terminationGuardVoteResult{
				Decision: decision,
				Valid:    true,
			}
		}()
	}
	waitGroup.Wait()
	return results
}

func restoreTerminationGuardSnapshot(state *State, snapshot terminationGuardSnapshot) {
	if state == nil {
		return
	}
	state.ToolName = snapshot.ToolName
	state.ActionInput = snapshot.ActionInput
	state.ToolOutput = snapshot.ToolOutput
	state.ToolOutputRaw = snapshot.ToolOutputRaw
	state.ToolError = snapshot.ToolError
}
