package tasksvc

import (
	"context"
	"testing"
	"time"
)

func TestRuntime_SubmitFakeLLM(t *testing.T) {
	doneCh := make(chan *Task, 1)
	rt := NewRuntime(RuntimeOptions{
		QueueHighCap:   10,
		QueueNormalCap: 10,
		QueueLowCap:    10,
		WorkerSize:     2,
		OnFinish: func(ctx context.Context, doneTask *Task) {
			select {
			case doneCh <- doneTask:
			default:
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rt.Start(ctx)

	taskObj, err := rt.SubmitFakeLLM(SubmitFakeLLMRequest{
		Priority: PriorityNormal,
		Prompt:   "hello",
		SleepMs:  10,
	})
	if err != nil {
		t.Fatalf("SubmitFakeLLM err=%v", err)
	}

	select {
	case done := <-doneCh:
		if done.ID != taskObj.ID {
			t.Fatalf("unexpected task id: got=%s want=%s", done.ID, taskObj.ID)
		}
		if done.Status != StatusSucceeded {
			t.Fatalf("unexpected status: %s", done.Status)
		}
		if done.Result.Text == nil || *done.Result.Text == "" {
			t.Fatalf("missing result text")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for task to finish")
	}
}

func TestRuntime_SubmitFakeLLM_InvalidPrompt(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{
		QueueHighCap:   1,
		QueueNormalCap: 1,
		QueueLowCap:    1,
		WorkerSize:     1,
	})
	_, err := rt.SubmitFakeLLM(SubmitFakeLLMRequest{Prompt: "   "})
	if err == nil {
		t.Fatalf("expected error")
	}
	if err != ErrInvalidFakeLLMPrompt {
		t.Fatalf("unexpected error: %v", err)
	}
}
