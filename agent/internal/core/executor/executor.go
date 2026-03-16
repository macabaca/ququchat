package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ququchat/agent/internal/core/task"
)

var ErrUnsupportedTask = errors.New("unsupported task type")

type Executor interface {
	Execute(ctx context.Context, t *task.Task) (task.Result, error)
}

type DefaultExecutor struct{}

func NewDefaultExecutor() *DefaultExecutor {
	return &DefaultExecutor{}
}

func (e *DefaultExecutor) Execute(ctx context.Context, t *task.Task) (task.Result, error) {
	switch t.Type {
	case task.TypeAdd:
		if t.Payload.Add == nil {
			return task.Result{}, errors.New("missing add payload")
		}
		sum := t.Payload.Add.A + t.Payload.Add.B
		return task.Result{AddSum: &sum}, nil
	case task.TypeFakeLLM:
		if t.Payload.FakeLLM == nil {
			return task.Result{}, errors.New("missing fake llm payload")
		}
		sleepMs := t.Payload.FakeLLM.SleepMs
		if sleepMs <= 0 {
			sleepMs = 800
		}
		select {
		case <-time.After(time.Duration(sleepMs) * time.Millisecond):
		case <-ctx.Done():
			return task.Result{}, ctx.Err()
		}
		text := fmt.Sprintf("fake-llm-response:%s", strings.TrimSpace(t.Payload.FakeLLM.Prompt))
		return task.Result{Text: &text}, nil
	default:
		return task.Result{}, ErrUnsupportedTask
	}
}
