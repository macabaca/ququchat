package agent

import (
	"context"
	"errors"
	"strings"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agentservices "ququchat/internal/taskservice/task/agent/services"
)

func RunValidatorNode(_ context.Context, state *State) (next string, err error) {
	if state == nil {
		return "", errors.New("validator node state is nil")
	}
	candidateRaw := strings.TrimSpace(state.FormattedRaw)
	if candidateRaw == "" {
		candidateRaw = strings.TrimSpace(state.CoordinatorRaw)
	}
	if candidateRaw == "" {
		return "", errors.New("validator node candidate raw is empty")
	}
	normalizedPlan, normalizedJSON, validationIssues := agentservices.ValidateCoordinatorOutput(candidateRaw, state.AvailableToolSpecs, getCoordinatorSchemaConfig())
	if state.MemorySession != nil {
		validateRecord := agentmemory.Observation{
			Step:   state.Step,
			Role:   "Validator",
			Tool:   "validate_coordinator_output",
			Input:  agentmemory.ShortText(candidateRaw, 220),
			Output: agentmemory.ShortText(normalizedJSON, 220),
		}
		if len(validationIssues) == 0 {
			validateRecord.Status = "succeeded"
			if strings.TrimSpace(validateRecord.Output) == "" {
				validateRecord.Output = "valid"
			}
		} else {
			validateRecord.Status = "failed"
			validateRecord.Error = agentservices.JoinValidationIssueTexts(validationIssues, "；")
		}
		state.MemorySession.AppendObservation(validateRecord)
	}
	if len(validationIssues) == 0 {
		state.Plan = normalizedPlan
		state.ToolName = strings.ToLower(strings.TrimSpace(normalizedPlan.Action.Tool))
		state.ActionInput = strings.TrimSpace(normalizedPlan.Action.Input)
		if state.MemorySession != nil {
			state.Feedback = strings.TrimSpace(state.MemorySession.BuildFeedback())
		}
		return "validator.done", nil
	}
	if state.MemorySession != nil {
		state.Feedback = agentservices.BuildValidationRetryFeedback(
			strings.TrimSpace(state.MemorySession.BuildFeedback()),
			validationIssues,
			coordinatorSchemaTemplateTextFromSpecs(state.AvailableToolSpecs),
			"上一轮规则校验未通过，错误如下：",
		)
	}
	return "validator.retry", nil
}
