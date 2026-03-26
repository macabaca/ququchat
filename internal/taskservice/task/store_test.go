package tasksvc

import (
	"errors"
	"testing"
	"time"
)

func TestMemoryStore_StatusTransitionsAreIdempotent(t *testing.T) {
	store := NewMemoryStore()
	task := &Task{
		ID:        "task-1",
		RequestID: "request-1",
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.Create(task); err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	if _, err := store.MarkRunning("task-1"); err != nil {
		t.Fatalf("first mark running failed: %v", err)
	}
	if _, err := store.MarkRunning("task-1"); !errors.Is(err, ErrTaskAlreadyStarted) {
		t.Fatalf("expected ErrTaskAlreadyStarted, got %v", err)
	}
	if _, err := store.MarkSucceeded("task-1", Result{}); err != nil {
		t.Fatalf("mark succeeded failed: %v", err)
	}
	if _, err := store.MarkSucceeded("task-1", Result{}); err != nil {
		t.Fatalf("second mark succeeded should be idempotent, got %v", err)
	}
	if _, err := store.MarkRunning("task-1"); !errors.Is(err, ErrTaskAlreadyCompleted) {
		t.Fatalf("expected ErrTaskAlreadyCompleted, got %v", err)
	}
}

func TestMemoryStore_RequestIDIsUnique(t *testing.T) {
	store := NewMemoryStore()
	first := &Task{
		ID:        "task-1",
		RequestID: "request-1",
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	second := &Task{
		ID:        "task-2",
		RequestID: "request-1",
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.Create(first); err != nil {
		t.Fatalf("create first task failed: %v", err)
	}
	if err := store.Create(second); !errors.Is(err, ErrTaskDuplicateRequestID) {
		t.Fatalf("expected ErrTaskDuplicateRequestID, got %v", err)
	}
}
