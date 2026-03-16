package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"ququchat/agent/internal/core/scheduler"
	"ququchat/agent/internal/core/task"
	"ququchat/agent/internal/store"
	"ququchat/agent/pkg/llmmsg"
)

type Service struct {
	store      store.Store
	dispatcher *scheduler.Dispatcher
}

func NewService(store store.Store, dispatcher *scheduler.Dispatcher) *Service {
	return &Service{
		store:      store,
		dispatcher: dispatcher,
	}
}

func (s *Service) SubmitIngress(msg *llmmsg.Ingress) (*task.Task, error) {
	if msg == nil {
		return nil, errors.New("llm ingress message is required")
	}
	now := time.Now()
	taskID := strings.TrimSpace(msg.TaskID)
	if taskID == "" {
		taskID = uuid.NewString()
	}
	taskType := strings.ToLower(strings.TrimSpace(msg.TaskType))
	if taskType == "" {
		taskType = string(task.TypeFakeLLM)
	}
	var fakePayload *task.FakeLLMPayload
	switch taskType {
	case string(task.TypeFakeLLM):
		nextFakePayload, err := parseFakeLLMPayload(msg)
		if err != nil {
			return nil, err
		}
		fakePayload = nextFakePayload
	default:
		return nil, fmt.Errorf("unsupported task_type: %s", taskType)
	}
	t := &task.Task{
		ID:        taskID,
		RequestID: strings.TrimSpace(msg.RequestID),
		Type:      task.Type(taskType),
		Priority:  fromPriority(msg.Priority),
		Status:    task.StatusPending,
		Payload: task.Payload{
			FakeLLM: fakePayload,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.Create(t); err != nil {
		return nil, err
	}
	if err := s.dispatcher.Dispatch(t.Clone()); err != nil {
		_, _ = s.store.MarkFailed(t.ID, err.Error())
		return nil, err
	}
	return t, nil
}

func parseFakeLLMPayload(msg *llmmsg.Ingress) (*task.FakeLLMPayload, error) {
	if msg == nil {
		return nil, errors.New("llm ingress message is required")
	}
	if len(msg.Input) == 0 {
		return nil, errors.New("input is required for fake_llm")
	}
	type fakeInput struct {
		Prompt  string `json:"prompt"`
		SleepMs int64  `json:"sleep_ms"`
	}
	var in fakeInput
	if err := json.Unmarshal(msg.Input, &in); err != nil {
		return nil, errors.New("invalid input for fake_llm")
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	if strings.TrimSpace(in.Prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	return &task.FakeLLMPayload{
		Prompt:  strings.TrimSpace(in.Prompt),
		SleepMs: in.SleepMs,
	}, nil
}

func fromPriority(p llmmsg.Priority) task.Priority {
	switch p {
	case llmmsg.PriorityHigh:
		return task.PriorityHigh
	case llmmsg.PriorityLow:
		return task.PriorityLow
	default:
		return task.PriorityNormal
	}
}
