package tasksvc

import (
	"context"
	"errors"
	"strings"

	taskagent "ququchat/internal/service/task/agent"
)

func (e *DefaultExecutor) executeAgent(ctx context.Context, t *Task) (string, error) {
	if t.Payload.Agent == nil {
		return "", errors.New("missing agent payload")
	}
	if e.llmClient == nil {
		return "", errors.New("llm client is not configured")
	}
	goal := strings.TrimSpace(t.Payload.Agent.Goal)
	if goal == "" {
		return "", errors.New("agent goal is required")
	}
	return taskagent.Execute(ctx, e.llmClient, taskagent.Input{
		Goal:           goal,
		RecentMessages: append([]string(nil), t.Payload.Agent.RecentMessages...),
		MaxSteps:       t.Payload.Agent.MaxSteps,
	})
}
