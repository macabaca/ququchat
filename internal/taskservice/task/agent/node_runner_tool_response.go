package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agentservices "ququchat/internal/taskservice/task/agent/services"
	agenttypes "ququchat/internal/taskservice/task/agent/types"
)

func RunToolResponseNode(ctx context.Context, client ChatClient, state *State) (next string, err error) {
	if client == nil {
		return "", errors.New("tool_response node client is not configured")
	}
	if state == nil {
		return "", errors.New("tool_response node state is nil")
	}
	goal := strings.TrimSpace(state.Goal)
	if goal == "" {
		return "", errors.New("agent goal is required")
	}
	toolName := strings.TrimSpace(state.ToolName)
	if toolName == "" {
		toolName = strings.TrimSpace(state.Plan.Action.Tool)
	}
	actionInput := strings.TrimSpace(state.ActionInput)
	if actionInput == "" {
		actionInput = strings.TrimSpace(state.Plan.Action.Input)
	}
	rawOutput := strings.TrimSpace(state.ToolOutputRaw)
	if rawOutput == "" {
		rawOutput = strings.TrimSpace(state.ToolOutput)
	}
	if rawOutput == "" {
		return "tool_response.done", nil
	}
	prompt := agentservices.BuildToolResponsePrompt(agenttypes.ToolResponsePromptInput{
		Goal:             goal,
		ToolName:         toolName,
		ToolInput:        normalizeToolInputForPrompt(actionInput),
		ToolRawOutput:    rawOutput,
		RealtimeGuidance: agentservices.BuildRealtimePlanningGuidance(state.AvailableToolSpecs, time.Now()),
	})
	summaryRaw, chatErr := client.Chat(ctx, prompt)
	summary := strings.TrimSpace(summaryRaw)
	if summary == "" {
		summary = rawOutput
	}
	rememberObservedURLsInState(state, summary)
	state.ToolOutputRaw = rawOutput
	state.ToolOutput = summary
	if state.MemorySession != nil {
		assistantToolCall := "{\"tool\":\"" + strings.TrimSpace(toolName) + "\",\"input\":" + normalizeToolInputForPrompt(actionInput) + "}"
		record := agentmemory.Observation{
			Step:      state.Step,
			Role:      "ToolResponse",
			Tool:      "summarize_tool_result",
			Input:     agentmemory.ShortText(assistantToolCall, 220),
			RawOutput: agentmemory.ShortText(rawOutput, 220),
			Output:    agentmemory.ShortText(summary, 220),
			Status:    "succeeded",
		}
		if chatErr != nil {
			record.Tool = "summarize_tool_result_fallback"
		}
		state.MemorySession.AppendObservation(record)
	}
	return "tool_response.done", nil
}

func normalizeToolInputForPrompt(actionInput string) string {
	trimmed := strings.TrimSpace(actionInput)
	if trimmed == "" {
		return "{}"
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}
	data, err := json.Marshal(trimmed)
	if err != nil {
		return "{}"
	}
	return strings.TrimSpace(string(data))
}
