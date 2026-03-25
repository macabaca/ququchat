package agent

import (
	"context"
	"errors"
	"strings"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agentservices "ququchat/internal/taskservice/task/agent/services"
)

func RunFormatterNode(ctx context.Context, client ChatClient, state *State) (next string, err error) {
	if client == nil {
		return "", errors.New("formatter node client is not configured")
	}
	if state == nil {
		return "", errors.New("formatter node state is nil")
	}
	coordinatorRaw := strings.TrimSpace(state.CoordinatorRaw)
	if coordinatorRaw == "" {
		return "", errors.New("formatter node coordinator raw is empty")
	}
	formatterPrompt := agentservices.BuildJSONFormatterPrompt(coordinatorRaw, coordinatorSchemaTemplateTextFromSpecs(state.AvailableToolSpecs))
	formatterRaw, formatterErr := client.Chat(ctx, formatterPrompt)
	state.FormattedRaw = strings.TrimSpace(formatterRaw)
	if state.MemorySession != nil {
		formatterRecord := agentmemory.Observation{
			Step:   state.Step,
			Role:   "Formatter",
			Tool:   "normalize_coordinator_output",
			Input:  agentmemory.ShortText(coordinatorRaw, 220),
			Output: agentmemory.ShortText(formatterRaw, 220),
		}
		if formatterErr != nil {
			formatterRecord.Status = "failed"
			formatterRecord.Error = formatterErr.Error()
		} else {
			formatterRecord.Status = "succeeded"
		}
		state.MemorySession.AppendObservation(formatterRecord)
	}
	if formatterErr != nil {
		return "", formatterErr
	}
	return "formatter.done", nil
}
