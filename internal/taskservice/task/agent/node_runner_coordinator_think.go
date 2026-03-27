package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agentservices "ququchat/internal/taskservice/task/agent/services"
	agenttypes "ququchat/internal/taskservice/task/agent/types"
)

func RunCoordinatorThinkNode(ctx context.Context, client ChatClient, state *State) (next string, err error) {
	if client == nil {
		return "", errors.New("coordinator_think node client is not configured")
	}
	if state == nil {
		return "", errors.New("coordinator_think node state is nil")
	}
	goal := strings.TrimSpace(state.Goal)
	if goal == "" {
		return "", errors.New("agent goal is required")
	}
	maxSteps := state.MaxSteps
	if maxSteps <= 0 {
		maxSteps = maxStepsDefault
	}
	if maxSteps > maxStepsLimit {
		maxSteps = maxStepsLimit
	}
	step := state.Step
	if step <= 0 {
		step = 1
	}
	recentMessages := agentmemory.NormalizeRecentMessages(state.RecentMessages)
	outlineIndex := state.OutlineIndex
	if outlineIndex < 0 {
		outlineIndex = 0
	}
	currentTask := strings.TrimSpace(state.CurrentTask)
	if currentTask == "" {
		currentTask = currentPlannerTask(state.Outline.Steps, outlineIndex)
	}
	coordinatorFeedback := strings.TrimSpace(state.Feedback)
	if coordinatorFeedback == "" && state.MemorySession != nil {
		coordinatorFeedback = strings.TrimSpace(state.MemorySession.BuildFeedback())
	}
	coordinatorFeedback = appendURLAliasFeedback(coordinatorFeedback, state)
	promptInput := agenttypes.CoordinatorPromptInput{
		Goal:               goal,
		RealtimeGuidance:   agentservices.BuildRealtimePlanningGuidance(state.AvailableToolSpecs, time.Now()),
		AgentIdentity:      buildAgentIdentityPrompt(),
		ToolSection:        buildCoordinatorToolSectionFromSpecs(state.AvailableToolSpecs),
		RuleLines:          coordinatorPromptRuleLinesFromSpecs(state.AvailableToolSpecs),
		Step:               step,
		MaxSteps:           maxSteps,
		OutlineText:        formatPlannerOutline(state.Outline.Steps, outlineIndex),
		CurrentTask:        currentTask,
		Feedback:           coordinatorFeedback,
		RecentMessageCount: len(recentMessages),
	}
	thinkPrompt := agentservices.BuildCoordinatorThinkPrompt(promptInput)
	thoughtRaw, thinkErr := client.Chat(ctx, thinkPrompt)
	if thinkErr != nil {
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(agentmemory.Observation{
				Step:   step,
				Role:   "CoordinatorThink",
				Tool:   "think",
				Input:  agentmemory.ShortText(thinkPrompt, 220),
				Output: agentmemory.ShortText(thoughtRaw, 220),
				Status: "failed",
				Error:  thinkErr.Error(),
			})
		}
		return "", fmt.Errorf("coordinator_think 调用失败: %w", thinkErr)
	}
	thought := normalizeCoordinatorThought(thoughtRaw)
	if strings.TrimSpace(thought) == "" {
		return "", errors.New("coordinator_think 未产出 thought")
	}
	if state.MemorySession != nil {
		state.MemorySession.AppendObservation(agentmemory.Observation{
			Step:   step,
			Role:   "CoordinatorThink",
			Tool:   "think",
			Input:  agentmemory.ShortText(thinkPrompt, 220),
			Output: agentmemory.ShortText(thought, 220),
			Status: "succeeded",
		})
	}
	state.MaxSteps = maxSteps
	state.Step = step
	state.RecentMessages = append([]string(nil), recentMessages...)
	state.OutlineIndex = outlineIndex
	state.CurrentTask = currentTask
	state.CoordinatorThought = thought
	state.CoordinatorRaw = ""
	state.FormattedRaw = ""
	state.Plan = plan{}
	state.ToolName = ""
	state.ActionInput = ""
	state.ToolOutput = ""
	state.ToolError = ""
	return "coordinator.think_done", nil
}

func normalizeCoordinatorThought(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	candidate, issue := agentservices.ExtractJSONObjectText(trimmed)
	if issue != nil || strings.TrimSpace(candidate) == "" {
		return trimmed
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(candidate), &root); err != nil {
		return trimmed
	}
	if thought, ok := root["thought"]; ok {
		return strings.TrimSpace(fmt.Sprint(thought))
	}
	return trimmed
}
