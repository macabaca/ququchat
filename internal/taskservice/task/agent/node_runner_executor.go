package agent

import (
	"context"
	"errors"
	"strings"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
)

func RunExecutorNode(ctx context.Context, state *State) (next string, err error) {
	if state == nil {
		return "", errors.New("executor node state is nil")
	}
	toolName := strings.ToLower(strings.TrimSpace(state.ToolName))
	if toolName == "" {
		toolName = strings.ToLower(strings.TrimSpace(state.Plan.Action.Tool))
	}
	actionInput := strings.TrimSpace(state.ActionInput)
	if actionInput == "" {
		actionInput = strings.TrimSpace(state.Plan.Action.Input)
	}
	state.ToolName = toolName
	state.ActionInput = actionInput
	if toolName == "" {
		return "", errors.New("executor node tool name is empty")
	}
	if toolName == "finish" {
		return "executor.finish", nil
	}
	if toolName == "replan" {
		return "executor.replan", nil
	}
	toolOutput, toolErr := state.ToolRuntime.Run(ctx, toolName, actionInput, strings.TrimSpace(state.RoomID))
	record := agentmemory.Observation{
		Step:  state.Step,
		Role:  "Executor",
		Tool:  toolName,
		Input: actionInput,
	}
	if toolErr != nil {
		record.Status = "failed"
		record.Error = toolErr.Error()
		state.ToolOutput = ""
		state.ToolError = toolErr.Error()
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(record)
		}
		return "executor.failed", nil
	}
	record.Status = "succeeded"
	record.Output = toolOutput
	state.ToolOutput = toolOutput
	state.ToolError = ""
	if state.MemorySession != nil {
		state.MemorySession.AppendObservation(record)
	}
	if state.OutlineIndex < len(state.Outline.Steps)-1 {
		state.OutlineIndex++
	}
	return "executor.done", nil
}
