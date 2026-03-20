package tasksvc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"ququchat/internal/service/task/aigcmq"
	"ququchat/internal/service/task/llmmq"
	"ququchat/internal/service/task/mcpclient"
	"ququchat/internal/service/task/openaicompat"
)

type RuntimeOptions struct {
	QueueHighCap          int
	QueueNormalCap        int
	QueueLowCap           int
	WorkerSize            int
	Store                 Store
	LLMClient             LLMClient
	LLMTransport          string
	LLMMQURL              string
	LLMMQQueue            string
	LLMMQTimeout          time.Duration
	LLMAPIKey             string
	LLMBaseURL            string
	LLMModel              string
	AIGCClient            AIGCClient
	AIGCTransport         string
	AIGCMQURL             string
	AIGCMQQueue           string
	AIGCMQTimeout         time.Duration
	EmbeddingProvider     EmbeddingProvider
	VectorStore           VectorStore
	EmbeddingModelRaw     string
	EmbeddingModelSummary string
	SummaryVectorDim      int
	RAGHandler            RAGHandler
	MCPMultiClient        *mcpclient.MultiClient
	OnFinish              func(ctx context.Context, doneTask *Task)
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
		if strings.EqualFold(strings.TrimSpace(opts.LLMTransport), "rabbitmq") &&
			strings.TrimSpace(opts.LLMMQURL) != "" &&
			strings.TrimSpace(opts.LLMMQQueue) != "" {
			client, err := llmmq.NewClient(llmmq.ClientOptions{
				URL:             opts.LLMMQURL,
				RequestQueue:    opts.LLMMQQueue,
				ResponseTimeout: opts.LLMMQTimeout,
			})
			if err == nil {
				llmClient = client
			}
		}
		if llmClient == nil && strings.TrimSpace(opts.LLMAPIKey) != "" && strings.TrimSpace(opts.LLMBaseURL) != "" && strings.TrimSpace(opts.LLMModel) != "" {
			client, err := openaicompat.NewLLMClient(openaicompat.LLMOptions{
				APIKey:  opts.LLMAPIKey,
				BaseURL: opts.LLMBaseURL,
				Model:   opts.LLMModel,
			})
			if err == nil {
				llmClient = client
			}
		}
	}
	aigcClient := opts.AIGCClient
	if aigcClient == nil {
		if strings.EqualFold(strings.TrimSpace(opts.AIGCTransport), "rabbitmq") &&
			strings.TrimSpace(opts.AIGCMQURL) != "" &&
			strings.TrimSpace(opts.AIGCMQQueue) != "" {
			client, err := aigcmq.NewClient(aigcmq.ClientOptions{
				URL:             opts.AIGCMQURL,
				RequestQueue:    opts.AIGCMQQueue,
				ResponseTimeout: opts.AIGCMQTimeout,
			})
			if err == nil {
				aigcClient = client
			}
		}
	}
	mcpMultiClient := opts.MCPMultiClient
	if mcpMultiClient == nil {
		client, err := mcpclient.NewMultiClientFromDefaultConfig(context.Background())
		if err == nil {
			mcpMultiClient = client
		}
	}
	exec := NewDefaultExecutor(ExecutorOptions{
		LLMClient:      llmClient,
		RAGHandler:     opts.RAGHandler,
		AIGCClient:     aigcClient,
		MCPMultiClient: mcpMultiClient,
	})
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
	RoomID         string
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
				RoomID:         strings.TrimSpace(req.RoomID),
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

func (r *Runtime) SubmitRAG(req SubmitRAGRequest) (*Task, error) {
	now := time.Now()
	taskID := strings.TrimSpace(req.RequestID)
	if taskID == "" {
		taskID = uuid.NewString()
	}
	t := &Task{
		ID:        taskID,
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      TypeRAG,
		Priority:  req.Priority,
		Status:    StatusPending,
		Payload: Payload{
			RAG: &RAGPayload{
				RoomID:               strings.TrimSpace(req.RoomID),
				SegmentGapSeconds:    req.SegmentGapSeconds,
				MaxCharsPerSegment:   req.MaxCharsPerSegment,
				MaxMessagesPerSeg:    req.MaxMessagesPerSeg,
				OverlapMessages:      req.OverlapMessages,
				MinMessageSequenceID: req.MinMessageSequenceID,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.Payload.RAG.RoomID == "" {
		return nil, ErrInvalidRAGRoomID
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

func (r *Runtime) SubmitRAGSearch(req SubmitRAGSearchRequest) (*Task, error) {
	now := time.Now()
	taskID := strings.TrimSpace(req.RequestID)
	if taskID == "" {
		taskID = uuid.NewString()
	}
	t := &Task{
		ID:        taskID,
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      TypeRAGSearch,
		Priority:  req.Priority,
		Status:    StatusPending,
		Payload: Payload{
			RAGSearch: &RAGSearchPayload{
				RoomID: strings.TrimSpace(req.RoomID),
				Query:  strings.TrimSpace(req.Query),
				TopK:   req.TopK,
				Vector: strings.TrimSpace(req.Vector),
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.Payload.RAGSearch.RoomID == "" {
		return nil, ErrInvalidRAGRoomID
	}
	if t.Payload.RAGSearch.Query == "" {
		return nil, ErrInvalidRAGSearchQuery
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

func (r *Runtime) SubmitRAGAddMemory(req SubmitRAGAddMemoryRequest) (*Task, error) {
	now := time.Now()
	taskID := strings.TrimSpace(req.RequestID)
	if taskID == "" {
		taskID = uuid.NewString()
	}
	t := &Task{
		ID:        taskID,
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      TypeRAGAddMem,
		Priority:  req.Priority,
		Status:    StatusPending,
		Payload: Payload{
			RAGAddMem: &RAGAddMemoryPayload{
				RoomID:             strings.TrimSpace(req.RoomID),
				StartSequenceID:    req.StartSequenceID,
				EndSequenceID:      req.EndSequenceID,
				SegmentGapSeconds:  req.SegmentGapSeconds,
				MaxCharsPerSegment: req.MaxCharsPerSegment,
				MaxMessagesPerSeg:  req.MaxMessagesPerSeg,
				OverlapMessages:    req.OverlapMessages,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.Payload.RAGAddMem.RoomID == "" {
		return nil, ErrInvalidRAGRoomID
	}
	if t.Payload.RAGAddMem.StartSequenceID <= 0 || t.Payload.RAGAddMem.EndSequenceID <= 0 || t.Payload.RAGAddMem.StartSequenceID > t.Payload.RAGAddMem.EndSequenceID {
		return nil, ErrInvalidRAGMemorySequenceRange
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
var ErrInvalidRAGRoomID = errors.New("invalid rag room id")
var ErrInvalidRAGSearchQuery = errors.New("invalid rag search query")
var ErrInvalidRAGMemorySequenceRange = errors.New("invalid rag memory sequence range")
