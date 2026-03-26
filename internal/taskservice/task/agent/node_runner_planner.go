package agent

import (
	"context"
	"errors"
	"strings"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	"ququchat/internal/taskservice/task/agent/toolruntime"
)

func RunPlannerNode(ctx context.Context, client ChatClient, state *State) (next string, err error) {
	if client == nil {
		return "", errors.New("planner node client is not configured")
	}
	if state == nil {
		return "", errors.New("planner node state is nil")
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
	recentMessages := agentmemory.NormalizeRecentMessages(state.RecentMessages)
	replanReason := ""
	if strings.ToLower(strings.TrimSpace(state.ToolName)) == "replan" {
		args, argsErr := toolruntime.ParseActionInputJSONObject(strings.TrimSpace(state.ActionInput))
		if argsErr == nil {
			replanReason = strings.TrimSpace(toolruntime.ReadStringArg(args, "reason"))
		}
		if replanReason == "" {
			replanReason = strings.TrimSpace(state.Plan.Thought)
		}
	}
	outline, outlineErr := generatePlannerOutline(ctx, client, goal, recentMessages, maxSteps, replanReason, strings.TrimSpace(state.CurrentTask), state.AvailableToolSpecs)
	if outlineErr != nil {
		return "", outlineErr
	}
	state.MaxSteps = maxSteps
	state.RecentMessages = append([]string(nil), recentMessages...)
	state.Outline = plannerOutline{Steps: outline}
	state.OutlineIndex = 0
	state.CurrentTask = currentPlannerTask(state.Outline.Steps, 0)
	if state.MemorySession != nil {
		state.MemorySession.AppendObservation(agentmemory.Observation{
			Step:   state.Step,
			Role:   "Planner",
			Tool:   "generate_execution_outline",
			Status: "succeeded",
			Output: agentmemory.ShortText(formatPlannerOutline(state.Outline.Steps, 0), 220),
		})
	}
	return "planner.done", nil
}
