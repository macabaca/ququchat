package tasksvc

import (
	"errors"
	"sync"
	"time"
)

var ErrTaskNotFound = errors.New("task not found")
var ErrTaskDuplicateRequestID = errors.New("task duplicate request id")
var ErrTaskAlreadyStarted = errors.New("task already started")
var ErrTaskAlreadyCompleted = errors.New("task already completed")

type Store interface {
	Create(t *Task) error
	Get(taskID string) (*Task, bool)
	GetByRequestID(requestID string) (*Task, bool)
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
	if t != nil {
		for _, existing := range s.tasks {
			if existing != nil && existing.RequestID != "" && existing.RequestID == t.RequestID && existing.ID != t.ID {
				return ErrTaskDuplicateRequestID
			}
		}
	}
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

func (s *MemoryStore) GetByRequestID(requestID string) (*Task, bool) {
	target := requestID
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tasks {
		if t != nil && t.RequestID == target {
			return t.Clone(), true
		}
	}
	return nil, false
}

func (s *MemoryStore) MarkRunning(taskID string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	if t.Status == StatusRunning {
		return t.Clone(), ErrTaskAlreadyStarted
	}
	if t.Status == StatusSucceeded || t.Status == StatusFailed {
		return t.Clone(), ErrTaskAlreadyCompleted
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
	if t.Status == StatusSucceeded {
		return t.Clone(), nil
	}
	if t.Status == StatusFailed {
		return t.Clone(), ErrTaskAlreadyCompleted
	}
	if t.Status != StatusRunning {
		return t.Clone(), ErrTaskAlreadyStarted
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
	if t.Status == StatusFailed {
		return t.Clone(), nil
	}
	if t.Status == StatusSucceeded {
		return t.Clone(), ErrTaskAlreadyCompleted
	}
	if t.Status != StatusRunning {
		return t.Clone(), ErrTaskAlreadyStarted
	}
	t.Status = StatusFailed
	t.ErrorMessage = message
	t.UpdatedAt = time.Now()
	return t.Clone(), nil
}
