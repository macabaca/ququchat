package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agentservices "ququchat/internal/taskservice/task/agent/services"
	agenttypes "ququchat/internal/taskservice/task/agent/types"
)

func RunCoordinatorNode(ctx context.Context, client ChatClient, state *State) (next string, err error) {
	if client == nil {
		return "", errors.New("coordinator node client is not configured")
	}
	if state == nil {
		return "", errors.New("coordinator node state is nil")
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
	currentCoordinatorPrompt := agentservices.BuildCoordinatorPrompt(agenttypes.CoordinatorPromptInput{
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
	})
	nextCoordinatorRaw, chatErr := client.Chat(ctx, currentCoordinatorPrompt)
	if chatErr != nil {
		return "", fmt.Errorf("coordinator调用失败: %w", chatErr)
	}
	state.MaxSteps = maxSteps
	state.Step = step
	state.RecentMessages = append([]string(nil), recentMessages...)
	state.OutlineIndex = outlineIndex
	state.CurrentTask = currentTask
	state.CoordinatorRaw = strings.TrimSpace(nextCoordinatorRaw)
	state.FormattedRaw = ""
	state.Plan = plan{}
	state.ToolName = ""
	state.ActionInput = ""
	state.ToolOutput = ""
	state.ToolError = ""
	return "coordinator.done", nil
}
