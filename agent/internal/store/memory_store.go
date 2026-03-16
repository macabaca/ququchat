package store

import (
	"errors"
	"sync"
	"time"

	"ququchat/agent/internal/core/task"
)

var ErrTaskNotFound = errors.New("task not found")

type MemoryStore struct {
	mu    sync.RWMutex
	tasks map[string]*task.Task
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tasks: make(map[string]*task.Task),
	}
}

func (s *MemoryStore) Create(t *task.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t.Clone()
	return nil
}

func (s *MemoryStore) Get(taskID string) (*task.Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, false
	}
	return t.Clone(), true
}

func (s *MemoryStore) MarkRunning(taskID string) (*task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	t.Status = task.StatusRunning
	t.UpdatedAt = time.Now()
	return t.Clone(), nil
}

func (s *MemoryStore) MarkSucceeded(taskID string, result task.Result) (*task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	t.Status = task.StatusSucceeded
	t.Result = result
	t.ErrorMessage = ""
	t.UpdatedAt = time.Now()
	return t.Clone(), nil
}

func (s *MemoryStore) MarkFailed(taskID string, message string) (*task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	t.Status = task.StatusFailed
	t.ErrorMessage = message
	t.UpdatedAt = time.Now()
	return t.Clone(), nil
}
