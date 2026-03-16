package tasksvc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type RuntimeOptions struct {
	QueueHighCap   int
	QueueNormalCap int
	QueueLowCap    int
	WorkerSize     int
	Store          Store
	LLMClient      LLMClient
	LLMAPIKey      string
	LLMBaseURL     string
	LLMModel       string
	OnFinish       func(ctx context.Context, doneTask *Task)
}

type Runtime struct {
	store Store
	queue Queue
	pool  *Pool
}

func NewRuntime(opts RuntimeOptions) *Runtime {
	queue := NewMemoryPriorityQueue(opts.QueueHighCap, opts.QueueNormalCap, opts.QueueLowCap)
	store := opts.Store
	if store == nil {
		store = NewMemoryStore()
	}
	llmClient := opts.LLMClient
	if llmClient == nil {
		if strings.TrimSpace(opts.LLMAPIKey) != "" && strings.TrimSpace(opts.LLMBaseURL) != "" && strings.TrimSpace(opts.LLMModel) != "" {
			client, err := NewOpenAICompatClient(OpenAICompatOptions{
				APIKey:  opts.LLMAPIKey,
				BaseURL: opts.LLMBaseURL,
				Model:   opts.LLMModel,
			})
			if err == nil {
				llmClient = client
			}
		}
	}
	exec := NewDefaultExecutor(ExecutorOptions{LLMClient: llmClient})
	pool := NewPool(queue, store, exec, opts.WorkerSize, opts.OnFinish)
	return &Runtime{
		store: store,
		queue: queue,
		pool:  pool,
	}
}

func (r *Runtime) Start(ctx context.Context) {
	r.pool.Start(ctx)
}

func (r *Runtime) Get(taskID string) (*Task, bool) {
	return r.store.Get(strings.TrimSpace(taskID))
}

type SubmitFakeLLMRequest struct {
	RequestID string
	Priority  Priority
	Prompt    string
	SleepMs   int64
}

type SubmitLLMRequest struct {
	RequestID string
	Priority  Priority
	Prompt    string
}

type SubmitSummaryRequest struct {
	RequestID string
	Priority  Priority
	Prompt    string
}

type SubmitAgentRequest struct {
	RequestID      string
	Priority       Priority
	Goal           string
	RecentMessages []string
	MaxSteps       int
}

func (r *Runtime) SubmitFakeLLM(req SubmitFakeLLMRequest) (*Task, error) {
	now := time.Now()
	taskID := strings.TrimSpace(req.RequestID)
	if taskID == "" {
		taskID = uuid.NewString()
	}
	t := &Task{
		ID:        taskID,
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      TypeFakeLLM,
		Priority:  req.Priority,
		Status:    StatusPending,
		Payload: Payload{
			FakeLLM: &FakeLLMPayload{
				Prompt:  strings.TrimSpace(req.Prompt),
				SleepMs: req.SleepMs,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.Payload.FakeLLM.Prompt == "" {
		return nil, ErrInvalidFakeLLMPrompt
	}
	if err := r.store.Create(t); err != nil {
		return nil, err
	}
	if err := r.queue.Push(t.Clone()); err != nil {
		_, _ = r.store.MarkFailed(t.ID, err.Error())
		return nil, err
	}
	return t.Clone(), nil
}

func (r *Runtime) SubmitLLM(req SubmitLLMRequest) (*Task, error) {
	now := time.Now()
	taskID := strings.TrimSpace(req.RequestID)
	if taskID == "" {
		taskID = uuid.NewString()
	}
	t := &Task{
		ID:        taskID,
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      TypeLLM,
		Priority:  req.Priority,
		Status:    StatusPending,
		Payload: Payload{
			LLM: &LLMPayload{
				Prompt: strings.TrimSpace(req.Prompt),
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.Payload.LLM.Prompt == "" {
		return nil, ErrInvalidLLMPrompt
	}
	if err := r.store.Create(t); err != nil {
		return nil, err
	}
	if err := r.queue.Push(t.Clone()); err != nil {
		_, _ = r.store.MarkFailed(t.ID, err.Error())
		return nil, err
	}
	return t.Clone(), nil
}

func (r *Runtime) SubmitSummary(req SubmitSummaryRequest) (*Task, error) {
	now := time.Now()
	taskID := strings.TrimSpace(req.RequestID)
	if taskID == "" {
		taskID = uuid.NewString()
	}
	t := &Task{
		ID:        taskID,
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      TypeSummary,
		Priority:  req.Priority,
		Status:    StatusPending,
		Payload: Payload{
			Summary: &SummaryPayload{
				Prompt: strings.TrimSpace(req.Prompt),
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.Payload.Summary.Prompt == "" {
		return nil, ErrInvalidSummaryPrompt
	}
	if err := r.store.Create(t); err != nil {
		return nil, err
	}
	if err := r.queue.Push(t.Clone()); err != nil {
		_, _ = r.store.MarkFailed(t.ID, err.Error())
		return nil, err
	}
	return t.Clone(), nil
}

func (r *Runtime) SubmitAgent(req SubmitAgentRequest) (*Task, error) {
	now := time.Now()
	taskID := strings.TrimSpace(req.RequestID)
	if taskID == "" {
		taskID = uuid.NewString()
	}
	goal := strings.TrimSpace(req.Goal)
	t := &Task{
		ID:        taskID,
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      TypeAgent,
		Priority:  req.Priority,
		Status:    StatusPending,
		Payload: Payload{
			Agent: &AgentPayload{
				Goal:           goal,
				RecentMessages: append([]string(nil), req.RecentMessages...),
				MaxSteps:       req.MaxSteps,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.Payload.Agent.Goal == "" {
		return nil, ErrInvalidAgentGoal
	}
	if err := r.store.Create(t); err != nil {
		return nil, err
	}
	if err := r.queue.Push(t.Clone()); err != nil {
		_, _ = r.store.MarkFailed(t.ID, err.Error())
		return nil, err
	}
	return t.Clone(), nil
}

var ErrInvalidFakeLLMPrompt = errors.New("invalid fake llm prompt")
var ErrInvalidLLMPrompt = errors.New("invalid llm prompt")
var ErrInvalidSummaryPrompt = errors.New("invalid summary prompt")
var ErrInvalidAgentGoal = errors.New("invalid agent goal")
