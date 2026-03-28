package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agentservices "ququchat/internal/taskservice/task/agent/services"
)

func RunFinalThinkNode(ctx context.Context, client ChatClient, state *State) (next string, err error) {
	if client == nil {
		return "", errors.New("final_think node client is not configured")
	}
	if state == nil {
		return "", errors.New("final_think node state is nil")
	}
	goal := strings.TrimSpace(state.Goal)
	if goal == "" {
		return "", errors.New("agent goal is required")
	}
	trace := []agentmemory.Observation(nil)
	if state.MemorySession != nil {
		trace = state.MemorySession.Trace()
	}
	candidate := strings.TrimSpace(state.ToolOutput)
	if candidate == "" {
		candidate = strings.TrimSpace(state.ToolOutputRaw)
	}
	if candidate == "" {
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(agentmemory.Observation{
				Step:   state.Step,
				Role:   "FinalThink",
				Tool:   "synthesize_final_answer",
				Status: "failed",
				Error:  "缺少可总结的工具输出",
			})
		}
		return "final_think.retry", nil
	}
	finalText, synthErr := agentservices.SynthesizeFinalAnswer(ctx, client, goal, state.RecentMessages, trace, candidate)
	if synthErr != nil {
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(agentmemory.Observation{
				Step:   state.Step,
				Role:   "FinalThink",
				Tool:   "synthesize_final_answer",
				Input:  agentmemory.ShortText(candidate, 220),
				Status: "failed",
				Error:  synthErr.Error(),
			})
		}
		return "final_think.retry", nil
	}
	finalText = strings.TrimSpace(finalText)
	if finalText == "" {
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(agentmemory.Observation{
				Step:   state.Step,
				Role:   "FinalThink",
				Tool:   "synthesize_final_answer",
				Input:  agentmemory.ShortText(candidate, 220),
				Status: "failed",
				Error:  "最终思考结果为空",
			})
		}
		return "final_think.retry", nil
	}
	payload, marshalErr := json.Marshal(map[string]string{
		"final": finalText,
	})
	if marshalErr != nil {
		return "final_think.retry", nil
	}
	finishInput := strings.TrimSpace(string(payload))
	state.ToolName = "finish"
	state.ActionInput = finishInput
	state.Plan.Action.Tool = "finish"
	state.Plan.Action.Input = finishInput
	if state.MemorySession != nil {
		state.MemorySession.AppendObservation(agentmemory.Observation{
			Step:   state.Step,
			Role:   "FinalThink",
			Tool:   "synthesize_final_answer",
			Input:  agentmemory.ShortText(candidate, 220),
			Output: agentmemory.ShortText(finalText, 220),
			Status: "succeeded",
		})
	}
	return "final_think.done", nil
}
