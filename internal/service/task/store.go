package tasksvc

import (
	"errors"
	"sync"
	"time"
)

var ErrTaskNotFound = errors.New("task not found")

type Store interface {
	Create(t *Task) error
	Get(taskID string) (*Task, bool)
	MarkRunning(taskID string) (*Task, error)
	MarkSucceeded(taskID string, result Result) (*Task, error)
	MarkFailed(taskID string, message string) (*Task, error)
}

type MemoryStore struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{tasks: make(map[string]*Task)}
}

func (s *MemoryStore) Create(t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t.Clone()
	return nil
}

func (s *MemoryStore) Get(taskID string) (*Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, false
	}
	return t.Clone(), true
}

func (s *MemoryStore) MarkRunning(taskID string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	t.Status = StatusRunning
	t.UpdatedAt = time.Now()
	return t.Clone(), nil
}

func (s *MemoryStore) MarkSucceeded(taskID string, result Result) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	t.Status = StatusSucceeded
	t.Result = result
	t.ErrorMessage = ""
	t.UpdatedAt = time.Now()
	return t.Clone(), nil
}

func (s *MemoryStore) MarkFailed(taskID string, message string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	t.Status = StatusFailed
	t.ErrorMessage = message
	t.UpdatedAt = time.Now()
	return t.Clone(), nil
}
