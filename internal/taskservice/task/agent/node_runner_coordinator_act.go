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

func RunCoordinatorActNode(ctx context.Context, client ChatClient, state *State) (next string, err error) {
	if client == nil {
		return "", errors.New("coordinator_act node client is not configured")
	}
	if state == nil {
		return "", errors.New("coordinator_act node state is nil")
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
	currentThought := strings.TrimSpace(state.CoordinatorThought)
	if currentThought == "" {
		return "", errors.New("coordinator_act 缺少 thought，请先执行 coordinator_think")
	}
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
		CurrentThought:     currentThought,
		Feedback:           coordinatorFeedback,
		RecentMessageCount: len(recentMessages),
	}
	actPrompt := agentservices.BuildCoordinatorActPrompt(promptInput)
	actionRaw, actErr := client.Chat(ctx, actPrompt)
	if actErr != nil {
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(agentmemory.Observation{
				Step:   step,
				Role:   "CoordinatorAct",
				Tool:   "act",
				Input:  agentmemory.ShortText(actPrompt, 220),
				Output: agentmemory.ShortText(actionRaw, 220),
				Status: "failed",
				Error:  actErr.Error(),
			})
		}
		return "", fmt.Errorf("coordinator_act 调用失败: %w", actErr)
	}
	toolName, actionInput, parseErr := parseCoordinatorAction(actionRaw)
	if parseErr != nil {
		return "", parseErr
	}
	coordinatorPayload := map[string]any{
		"thought": currentThought,
		"action": map[string]string{
			"tool":  toolName,
			"input": actionInput,
		},
	}
	payloadBytes, marshalErr := json.Marshal(coordinatorPayload)
	if marshalErr != nil {
		return "", fmt.Errorf("coordinator输出组装失败: %w", marshalErr)
	}
	if state.MemorySession != nil {
		state.MemorySession.AppendObservation(agentmemory.Observation{
			Step:   step,
			Role:   "CoordinatorAct",
			Tool:   "act",
			Input:  agentmemory.ShortText(actPrompt, 220),
			Output: agentmemory.ShortText(strings.TrimSpace(actionRaw), 220),
			Status: "succeeded",
		})
	}
	state.MaxSteps = maxSteps
	state.Step = step
	state.RecentMessages = append([]string(nil), recentMessages...)
	state.OutlineIndex = outlineIndex
	state.CurrentTask = currentTask
	state.CoordinatorRaw = strings.TrimSpace(string(payloadBytes))
	state.FormattedRaw = ""
	return "coordinator.act_done", nil
}

func parseCoordinatorAction(raw string) (string, string, error) {
	candidate, issue := agentservices.ExtractJSONObjectText(raw)
	if issue != nil {
		return "", "", errors.New("coordinator行动阶段输出不是合法 JSON 对象")
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(candidate), &root); err != nil {
		return "", "", errors.New("coordinator行动阶段输出不是合法 JSON 对象")
	}
	actionObj := root
	if nested, ok := root["action"].(map[string]any); ok && len(nested) > 0 {
		actionObj = nested
	}
	toolName := strings.TrimSpace(fmt.Sprint(actionObj["tool"]))
	actionInput := strings.TrimSpace(fmt.Sprint(actionObj["input"]))
	if toolName == "" {
		return "", "", errors.New("coordinator行动阶段缺少 action.tool")
	}
	if actionInput == "" {
		return "", "", errors.New("coordinator行动阶段缺少 action.input")
	}
	return toolName, actionInput, nil
}
