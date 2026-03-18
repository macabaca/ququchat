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

type ExecutorOptions struct {
	LLMClient  LLMClient
	RAGHandler RAGHandler
}

type DefaultExecutor struct {
	llmClient  LLMClient
	ragHandler RAGHandler
}

func NewDefaultExecutor(opts ExecutorOptions) *DefaultExecutor {
	return &DefaultExecutor{
		llmClient:  opts.LLMClient,
		ragHandler: opts.RAGHandler,
	}
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
		final := text
		return Result{Text: &text, Final: &final}, nil
	case TypeLLM:
		if t.Payload.LLM == nil {
			return Result{}, errors.New("missing llm payload")
		}
		if e.llmClient == nil {
			return Result{}, errors.New("llm client is not configured")
		}
		text, err := e.llmClient.Chat(ctx, t.Payload.LLM.Prompt)
		if err != nil {
			return Result{}, err
		}
		final := text
		return Result{Text: &text, Final: &final}, nil
	case TypeSummary:
		if t.Payload.Summary == nil {
			return Result{}, errors.New("missing summary payload")
		}
		if e.llmClient == nil {
			return Result{}, errors.New("llm client is not configured")
		}
		text, err := e.llmClient.Chat(ctx, t.Payload.Summary.Prompt)
		if err != nil {
			return Result{}, err
		}
		final := text
		return Result{Text: &text, Final: &final}, nil
	case TypeAgent:
		return e.executeAgent(ctx, t)
	case TypeRAG:
		if t.Payload.RAG == nil {
			return Result{}, errors.New("missing rag payload")
		}
		if e.ragHandler == nil {
			return Result{}, errors.New("rag handler is not configured")
		}
		return e.ragHandler.ExecuteRAG(ctx, t.Payload.RAG)
	default:
		return Result{}, ErrUnsupportedTask
	}
}
