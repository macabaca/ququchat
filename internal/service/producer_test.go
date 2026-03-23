package taskservice

import (
	"testing"
	"time"

	tasksvc "ququchat/internal/service/task"
)

type testQueue struct {
	pushCount int
}

func (q *testQueue) Push(*tasksvc.Task) error {
	q.pushCount++
	return nil
}

func TestCreateAndEnqueue_DuplicateRequestIDReturnsExistingTask(t *testing.T) {
	store := tasksvc.NewMemoryStore()
	q := &testQueue{}
	p := &Producer{
		store:                 store,
		highQueue:             q,
		normalQueue:           q,
		lowQueue:              q,
		inputRetryMaxAttempts: 1,
		inputRetryDelay:       time.Millisecond,
	}
	first := &tasksvc.Task{
		ID:        "task-1",
		RequestID: "request-1",
		Priority:  tasksvc.PriorityHigh,
		Status:    tasksvc.StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	created, err := p.createAndEnqueue(first)
	if err != nil {
		t.Fatalf("create first task failed: %v", err)
	}
	if created.ID != "task-1" {
		t.Fatalf("unexpected first task id: %s", created.ID)
	}
	second := &tasksvc.Task{
		ID:        "task-2",
		RequestID: "request-1",
		Priority:  tasksvc.PriorityHigh,
		Status:    tasksvc.StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	existing, err := p.createAndEnqueue(second)
	if err != nil {
		t.Fatalf("create duplicate request_id task failed: %v", err)
	}
	if existing.ID != "task-1" {
		t.Fatalf("expected existing task id task-1, got %s", existing.ID)
	}
	if _, ok := store.Get("task-2"); ok {
		t.Fatalf("unexpected duplicate task persisted")
	}
	if q.pushCount != 1 {
		t.Fatalf("expected queue push count 1, got %d", q.pushCount)
	}
}
