package taskservice

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	tasksvc "ququchat/internal/service/task"
)

type Producer struct {
	store                 tasksvc.Store
	highQueue             tasksvc.ProducerQueue
	normalQueue           tasksvc.ProducerQueue
	lowQueue              tasksvc.ProducerQueue
	inputRetryMaxAttempts int
	inputRetryDelay       time.Duration
}

func NewProducer(db *gorm.DB, opts tasksvc.RuntimeOptions) *Producer {
	store := opts.Store
	if store == nil && db != nil {
		store = tasksvc.NewGormStore(db)
	}
	if store == nil {
		store = tasksvc.NewMemoryStore()
	}
	queueTransport := strings.ToLower(strings.TrimSpace(opts.QueueTransport))
	if queueTransport == "" {
		queueTransport = "rabbitmq"
	}
	highQueue := tasksvc.ProducerQueue(localUnavailableQueue{reason: fmt.Errorf("unsupported queue transport: %s", queueTransport)})
	normalQueue := tasksvc.ProducerQueue(localUnavailableQueue{reason: fmt.Errorf("unsupported queue transport: %s", queueTransport)})
	lowQueue := tasksvc.ProducerQueue(localUnavailableQueue{reason: fmt.Errorf("unsupported queue transport: %s", queueTransport)})
	if queueTransport == "rabbitmq" {
		topology := resolveRabbitMQPriorityTopology(opts)
		newQueue := func(queueName string, exchangeName string) tasksvc.ProducerQueue {
			rmqQueue, err := tasksvc.NewRabbitMQProducer(tasksvc.RabbitMQQueueOptions{
				URL:          opts.QueueRabbitMQURL,
				QueueName:    queueName,
				ExchangeName: exchangeName,
				MaxPriority:  opts.QueueRabbitMQMaxPriority,
				MaxLength:    opts.QueueRabbitMQMaxLength,
			})
			if err == nil {
				return rmqQueue
			}
			log.Printf("init producer rabbitmq queue failed queue=%s: %v", queueName, err)
			return localUnavailableQueue{reason: fmt.Errorf("init producer rabbitmq queue failed queue=%s: %w", queueName, err)}
		}
		highQueue = newQueue(topology.HighQueueName, topology.HighExchangeName)
		normalQueue = newQueue(topology.NormalQueueName, topology.NormalExchangeName)
		lowQueue = newQueue(topology.LowQueueName, topology.LowExchangeName)
	}
	inputRetryMaxAttempts := opts.InputRetryMaxAttempts
	if inputRetryMaxAttempts <= 0 {
		inputRetryMaxAttempts = 3
	}
	inputRetryDelay := opts.InputRetryDelay
	if inputRetryDelay <= 0 {
		inputRetryDelay = 500 * time.Millisecond
	}
	return &Producer{
		store:                 store,
		highQueue:             highQueue,
		normalQueue:           normalQueue,
		lowQueue:              lowQueue,
		inputRetryMaxAttempts: inputRetryMaxAttempts,
		inputRetryDelay:       inputRetryDelay,
	}
}

func (p *Producer) Close() {
	if p == nil {
		return
	}
	for _, queue := range []tasksvc.ProducerQueue{p.highQueue, p.normalQueue, p.lowQueue} {
		if queue == nil {
			continue
		}
		if closer, ok := queue.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
}

func (p *Producer) Get(taskID string) (*tasksvc.Task, bool) {
	if p == nil || p.store == nil {
		return nil, false
	}
	return p.store.Get(strings.TrimSpace(taskID))
}

func (p *Producer) MarkFailed(taskID string, message string) (*tasksvc.Task, error) {
	if p == nil || p.store == nil {
		return nil, errors.New("producer store is nil")
	}
	return p.store.MarkFailed(strings.TrimSpace(taskID), strings.TrimSpace(message))
}

func (p *Producer) SubmitFakeLLM(req tasksvc.SubmitFakeLLMRequest) (*tasksvc.Task, error) {
	now := time.Now()
	t := &tasksvc.Task{
		ID:        uuid.NewString(),
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      tasksvc.TypeFakeLLM,
		Priority:  req.Priority,
		Status:    tasksvc.StatusPending,
		Payload: tasksvc.Payload{
			FakeLLM: &tasksvc.FakeLLMPayload{
				Prompt:  strings.TrimSpace(req.Prompt),
				SleepMs: req.SleepMs,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.Payload.FakeLLM.Prompt == "" {
		return nil, tasksvc.ErrInvalidFakeLLMPrompt
	}
	doneTask, err := p.createAndEnqueue(t)
	if err != nil {
		return nil, err
	}
	return doneTask.Clone(), nil
}

func (p *Producer) SubmitLLM(req tasksvc.SubmitLLMRequest) (*tasksvc.Task, error) {
	now := time.Now()
	t := &tasksvc.Task{
		ID:        uuid.NewString(),
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      tasksvc.TypeLLM,
		Priority:  req.Priority,
		Status:    tasksvc.StatusPending,
		Payload: tasksvc.Payload{
			LLM: &tasksvc.LLMPayload{
				Prompt: strings.TrimSpace(req.Prompt),
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.Payload.LLM.Prompt == "" {
		return nil, tasksvc.ErrInvalidLLMPrompt
	}
	doneTask, err := p.createAndEnqueue(t)
	if err != nil {
		return nil, err
	}
	return doneTask.Clone(), nil
}

func (p *Producer) SubmitSummary(req tasksvc.SubmitSummaryRequest) (*tasksvc.Task, error) {
	now := time.Now()
	t := &tasksvc.Task{
		ID:        uuid.NewString(),
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      tasksvc.TypeSummary,
		Priority:  req.Priority,
		Status:    tasksvc.StatusPending,
		Payload: tasksvc.Payload{
			Summary: &tasksvc.SummaryPayload{
				Prompt: strings.TrimSpace(req.Prompt),
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.Payload.Summary.Prompt == "" {
		return nil, tasksvc.ErrInvalidSummaryPrompt
	}
	doneTask, err := p.createAndEnqueue(t)
	if err != nil {
		return nil, err
	}
	return doneTask.Clone(), nil
}

func (p *Producer) SubmitAgent(req tasksvc.SubmitAgentRequest) (*tasksvc.Task, error) {
	now := time.Now()
	t := &tasksvc.Task{
		ID:        uuid.NewString(),
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      tasksvc.TypeAgent,
		Priority:  req.Priority,
		Status:    tasksvc.StatusPending,
		Payload: tasksvc.Payload{
			Agent: &tasksvc.AgentPayload{
				Goal:           strings.TrimSpace(req.Goal),
				RecentMessages: append([]string(nil), req.RecentMessages...),
				MaxSteps:       req.MaxSteps,
				RoomID:         strings.TrimSpace(req.RoomID),
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if t.Payload.Agent.Goal == "" {
		return nil, tasksvc.ErrInvalidAgentGoal
	}
	doneTask, err := p.createAndEnqueue(t)
	if err != nil {
		return nil, err
	}
	return doneTask.Clone(), nil
}

func (p *Producer) SubmitRAG(req tasksvc.SubmitRAGRequest) (*tasksvc.Task, error) {
	now := time.Now()
	t := &tasksvc.Task{
		ID:        uuid.NewString(),
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      tasksvc.TypeRAG,
		Priority:  req.Priority,
		Status:    tasksvc.StatusPending,
		Payload: tasksvc.Payload{
			RAG: &tasksvc.RAGPayload{
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
		return nil, tasksvc.ErrInvalidRAGRoomID
	}
	doneTask, err := p.createAndEnqueue(t)
	if err != nil {
		return nil, err
	}
	return doneTask.Clone(), nil
}

func (p *Producer) SubmitRAGSearch(req tasksvc.SubmitRAGSearchRequest) (*tasksvc.Task, error) {
	now := time.Now()
	t := &tasksvc.Task{
		ID:        uuid.NewString(),
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      tasksvc.TypeRAGSearch,
		Priority:  req.Priority,
		Status:    tasksvc.StatusPending,
		Payload: tasksvc.Payload{
			RAGSearch: &tasksvc.RAGSearchPayload{
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
		return nil, tasksvc.ErrInvalidRAGRoomID
	}
	if t.Payload.RAGSearch.Query == "" {
		return nil, tasksvc.ErrInvalidRAGSearchQuery
	}
	doneTask, err := p.createAndEnqueue(t)
	if err != nil {
		return nil, err
	}
	return doneTask.Clone(), nil
}

func (p *Producer) SubmitRAGAddMemory(req tasksvc.SubmitRAGAddMemoryRequest) (*tasksvc.Task, error) {
	now := time.Now()
	t := &tasksvc.Task{
		ID:        uuid.NewString(),
		RequestID: strings.TrimSpace(req.RequestID),
		Type:      tasksvc.TypeRAGAddMem,
		Priority:  req.Priority,
		Status:    tasksvc.StatusPending,
		Payload: tasksvc.Payload{
			RAGAddMem: &tasksvc.RAGAddMemoryPayload{
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
		return nil, tasksvc.ErrInvalidRAGRoomID
	}
	if t.Payload.RAGAddMem.StartSequenceID <= 0 || t.Payload.RAGAddMem.EndSequenceID <= 0 || t.Payload.RAGAddMem.StartSequenceID > t.Payload.RAGAddMem.EndSequenceID {
		return nil, tasksvc.ErrInvalidRAGMemorySequenceRange
	}
	doneTask, err := p.createAndEnqueue(t)
	if err != nil {
		return nil, err
	}
	return doneTask.Clone(), nil
}

func (p *Producer) createAndEnqueue(t *tasksvc.Task) (*tasksvc.Task, error) {
	if p == nil || p.store == nil || t == nil {
		return nil, errors.New("producer not initialized")
	}
	t.RequestID = normalizeRequestID(t.RequestID, t.ID)
	if err := p.store.Create(t); err != nil {
		if errors.Is(err, tasksvc.ErrTaskDuplicateRequestID) {
			existing, ok := p.store.GetByRequestID(t.RequestID)
			if ok && existing != nil {
				return existing.Clone(), nil
			}
		}
		return nil, err
	}
	if err := p.enqueueWithRetry(t.Clone()); err != nil {
		_, _ = p.store.MarkFailed(t.ID, err.Error())
		return nil, err
	}
	return t.Clone(), nil
}

func (p *Producer) enqueueWithRetry(task *tasksvc.Task) error {
	maxAttempts := p.inputRetryMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	delay := p.inputRetryDelay
	if delay <= 0 {
		delay = 100 * time.Millisecond
	}
	queue := p.queueForPriority(task.Priority)
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = queue.Push(task)
		if lastErr == nil {
			return nil
		}
		log.Printf("[task-producer-enqueue-retry] task=%s priority=%d attempt=%d/%d err=%v", task.ID, task.Priority, attempt, maxAttempts, lastErr)
		if attempt == maxAttempts {
			break
		}
		time.Sleep(delay)
	}
	return fmt.Errorf("enqueue failed after retries task=%s priority=%d: %w", task.ID, task.Priority, lastErr)
}

func (p *Producer) queueForPriority(priority tasksvc.Priority) tasksvc.ProducerQueue {
	if p == nil {
		return localUnavailableQueue{reason: errors.New("producer is nil")}
	}
	switch priority {
	case tasksvc.PriorityHigh:
		if p.highQueue != nil {
			return p.highQueue
		}
	case tasksvc.PriorityLow:
		if p.lowQueue != nil {
			return p.lowQueue
		}
	default:
		if p.normalQueue != nil {
			return p.normalQueue
		}
	}
	if p.normalQueue != nil {
		return p.normalQueue
	}
	if p.highQueue != nil {
		return p.highQueue
	}
	if p.lowQueue != nil {
		return p.lowQueue
	}
	return localUnavailableQueue{reason: errors.New("producer queue is unavailable")}
}

type rabbitMQPriorityTopology struct {
	HighQueueName      string
	NormalQueueName    string
	LowQueueName       string
	HighExchangeName   string
	NormalExchangeName string
	LowExchangeName    string
}

func resolveRabbitMQPriorityTopology(opts tasksvc.RuntimeOptions) rabbitMQPriorityTopology {
	baseQueueName := strings.TrimSpace(opts.QueueRabbitMQName)
	if baseQueueName == "" {
		baseQueueName = "ququchat.task.queue"
	}
	baseExchangeName := strings.TrimSpace(opts.QueueRabbitMQExchange)
	if baseExchangeName == "" {
		baseExchangeName = baseQueueName + ".exchange"
	}
	return rabbitMQPriorityTopology{
		HighQueueName:      firstNonEmpty(opts.QueueRabbitMQHighName, baseQueueName+".high"),
		NormalQueueName:    firstNonEmpty(opts.QueueRabbitMQNormalName, baseQueueName+".normal"),
		LowQueueName:       firstNonEmpty(opts.QueueRabbitMQLowName, baseQueueName+".low"),
		HighExchangeName:   firstNonEmpty(opts.QueueRabbitMQHighExchange, baseExchangeName+".high"),
		NormalExchangeName: firstNonEmpty(opts.QueueRabbitMQNormalExchange, baseExchangeName+".normal"),
		LowExchangeName:    firstNonEmpty(opts.QueueRabbitMQLowExchange, baseExchangeName+".low"),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeRequestID(requestID string, fallbackTaskID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID != "" {
		return requestID
	}
	if strings.TrimSpace(fallbackTaskID) != "" {
		return strings.TrimSpace(fallbackTaskID)
	}
	return uuid.NewString()
}

type localUnavailableQueue struct {
	reason error
}

func (q localUnavailableQueue) Push(t *tasksvc.Task) error {
	if q.reason != nil {
		return q.reason
	}
	return fmt.Errorf("queue is unavailable")
}
