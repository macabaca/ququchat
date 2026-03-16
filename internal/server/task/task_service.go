package task

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	tasksvc "ququchat/internal/service/task"
)

var ErrServiceNotInitialized = errors.New("task service not initialized")
var ErrCommandRequired = errors.New("command required")
var ErrUnsupportedCommand = errors.New("unsupported command")

type TaskCallback func(ctx context.Context, doneTask *tasksvc.Task)

type Service struct {
	runtime    *tasksvc.Runtime
	callbackMu sync.Mutex
	callbacks  map[string]TaskCallback
}

func NewService(db *gorm.DB, opts tasksvc.RuntimeOptions) *Service {
	s := &Service{callbacks: make(map[string]TaskCallback)}
	upstreamOnFinish := opts.OnFinish
	opts.OnFinish = func(ctx context.Context, doneTask *tasksvc.Task) {
		s.dispatchCallback(ctx, doneTask)
		if upstreamOnFinish != nil {
			upstreamOnFinish(ctx, doneTask)
		}
	}
	if opts.Store == nil && db != nil {
		opts.Store = tasksvc.NewGormStore(db)
	}
	s.runtime = tasksvc.NewRuntime(opts)
	return s
}

func (s *Service) Start(ctx context.Context) {
	if s == nil || s.runtime == nil {
		return
	}
	go s.runtime.Start(ctx)
}

func (s *Service) SubmitFakeLLM(req tasksvc.SubmitFakeLLMRequest) (*tasksvc.Task, error) {
	if s == nil || s.runtime == nil {
		return nil, ErrServiceNotInitialized
	}
	return s.runtime.SubmitFakeLLM(req)
}

func (s *Service) Get(taskID string) (*tasksvc.Task, bool) {
	if s == nil || s.runtime == nil {
		return nil, false
	}
	return s.runtime.Get(taskID)
}

type SubmitCommandRequest struct {
	UserID  string
	RoomID  string
	Content string
}

func (s *Service) SubmitCommand(req SubmitCommandRequest, cb TaskCallback) (string, error) {
	if s == nil || s.runtime == nil {
		return "", ErrServiceNotInitialized
	}
	raw := strings.TrimSpace(req.Content)
	if raw == "" {
		return "", ErrCommandRequired
	}
	if !strings.HasPrefix(raw, "\\") {
		return "", ErrUnsupportedCommand
	}
	cmd := strings.TrimSpace(strings.TrimPrefix(raw, "\\"))
	var (
		t   *tasksvc.Task
		err error
	)
	if strings.HasPrefix(cmd, "task:fake_llm") {
		prompt := strings.TrimSpace(strings.TrimPrefix(cmd, "task:fake_llm"))
		if prompt == "" {
			prompt = cmd
		}
		t, err = s.runtime.SubmitFakeLLM(tasksvc.SubmitFakeLLMRequest{
			Priority: tasksvc.PriorityNormal,
			Prompt:   prompt,
			SleepMs:  800,
		})
	} else {
		t, err = s.runtime.SubmitFakeLLM(tasksvc.SubmitFakeLLMRequest{
			RequestID: strings.TrimSpace(req.UserID) + "-" + strings.TrimSpace(req.RoomID) + "-" + time.Now().Format("20060102150405.000000000"),
			Priority:  tasksvc.PriorityNormal,
			Prompt:    cmd,
			SleepMs:   800,
		})
	}
	if err != nil {
		return "", err
	}
	s.registerCallback(t.ID, cb)
	return t.ID, nil
}

func (s *Service) registerCallback(taskID string, cb TaskCallback) {
	if cb == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	s.callbackMu.Lock()
	s.callbacks[taskID] = cb
	s.callbackMu.Unlock()
}

func (s *Service) dispatchCallback(ctx context.Context, doneTask *tasksvc.Task) {
	if doneTask == nil {
		return
	}
	s.callbackMu.Lock()
	cb := s.callbacks[doneTask.ID]
	delete(s.callbacks, doneTask.ID)
	s.callbackMu.Unlock()
	if cb != nil {
		cb(ctx, doneTask.Clone())
	}
}
