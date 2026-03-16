package tasksvc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrUnsupportedTask = errors.New("unsupported task type")

type Executor interface {
	Execute(ctx context.Context, t *Task) (Result, error)
}

type DefaultExecutor struct{}

func NewDefaultExecutor() *DefaultExecutor {
	return &DefaultExecutor{}
}

func (e *DefaultExecutor) Execute(ctx context.Context, t *Task) (Result, error) {
	switch t.Type {
	case TypeFakeLLM:
		if t.Payload.FakeLLM == nil {
			return Result{}, errors.New("missing fake llm payload")
		}
		sleepMs := t.Payload.FakeLLM.SleepMs
		if sleepMs <= 0 {
			sleepMs = 800
		}
		select {
		case <-time.After(time.Duration(sleepMs) * time.Millisecond):
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
		text := fmt.Sprintf("fake-llm-response:%s", strings.TrimSpace(t.Payload.FakeLLM.Prompt))
		return Result{Text: &text}, nil
	default:
		return Result{}, ErrUnsupportedTask
	}
}
