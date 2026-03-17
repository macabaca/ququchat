package tasksvc

import (
	"context"
	"errors"
	"strings"

	taskagent "ququchat/internal/service/task/agent"
)

func (e *DefaultExecutor) executeAgent(ctx context.Context, t *Task) (Result, error) {
	if t.Payload.Agent == nil {
		return Result{}, errors.New("missing agent payload")
	}
	if e.llmClient == nil {
		return Result{}, errors.New("llm client is not configured")
	}
	goal := strings.TrimSpace(t.Payload.Agent.Goal)
	if goal == "" {
		return Result{}, errors.New("agent goal is required")
	}
	text, err := taskagent.Execute(ctx, e.llmClient, taskagent.Input{
		Goal:           goal,
		RecentMessages: append([]string(nil), t.Payload.Agent.RecentMessages...),
		MaxSteps:       t.Payload.Agent.MaxSteps,
	})
	if err != nil {
		return Result{}, err
	}
	final, memory := splitAgentFinalAndMemory(text)
	payload := map[string]interface{}{}
	if strings.TrimSpace(memory) != "" {
		payload["memory"] = strings.TrimSpace(memory)
	}
	return Result{
		Text:    &text,
		Final:   stringPtr(strings.TrimSpace(final)),
		Payload: payload,
	}, nil
}

func splitAgentFinalAndMemory(text string) (string, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", ""
	}
	const finalMarker = "最终结果："
	idx := strings.LastIndex(trimmed, finalMarker)
	if idx < 0 {
		return trimmed, ""
	}
	memory := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(trimmed[:idx]), "工具调用记录："))
	final := strings.TrimSpace(trimmed[idx+len(finalMarker):])
	if final == "" {
		final = trimmed
	}
	return final, memory
}

func stringPtr(s string) *string {
	v := strings.TrimSpace(s)
	return &v
}
