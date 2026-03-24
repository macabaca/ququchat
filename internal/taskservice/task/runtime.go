package tasksvc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"ququchat/internal/taskservice/task/aigcmq"
	"ququchat/internal/taskservice/task/llmmq"
	"ququchat/internal/taskservice/task/mcpclient"
	"ququchat/internal/taskservice/task/openaicompat"
)

type RuntimeOptions struct {
	QueueHighCap                     int
	QueueNormalCap                   int
	QueueLowCap                      int
	QueueTransport                   string
	QueueRabbitMQURL                 string
	QueueRabbitMQName                string
	QueueRabbitMQExchange            string
	QueueRabbitMQHighName            string
	QueueRabbitMQNormalName          string
	QueueRabbitMQLowName             string
	QueueRabbitMQHighExchange        string
	QueueRabbitMQNormalExchange      string
	QueueRabbitMQLowExchange         string
	QueueRabbitMQMaxPriority         int
	QueueRabbitMQMaxLength           int
	DoneEventRabbitMQURL             string
	DoneEventQueueName               string
	DoneEventPublishRetryMaxAttempts int
	DoneEventPublishRetryDelay       time.Duration
	DoneEventConsumeRetryMaxAttempts int
	DoneEventConsumeRetryDelay       time.Duration
	InputRetryMaxAttempts            int
	InputRetryDelay                  time.Duration
	WorkerSize                       int
	Store                            Store
	LLMClient                        LLMClient
	LLMTransport                     string
	LLMMQURL                         string
	LLMMQQueue                       string
	LLMMQTimeout                     time.Duration
	LLMAPIKey                        string
	LLMBaseURL                       string
	LLMModel                         string
	AIGCClient                       AIGCClient
	AIGCTransport                    string
	AIGCMQURL                        string
	AIGCMQQueue                      string
	AIGCMQTimeout                    time.Duration
	EmbeddingProvider                EmbeddingProvider
	VectorStore                      VectorStore
	EmbeddingModelRaw                string
	EmbeddingModelSummary            string
	SummaryVectorDim                 int
	RAGHandler                       RAGHandler
	MCPMultiClient                   *mcpclient.MultiClient
	OnFinish                         func(ctx context.Context, doneTask *Task)
}

type Runtime struct {
	store  Store
	queues []ConsumerQueue
	pools  []*Pool
}

func NewRuntime(opts RuntimeOptions) *Runtime {
	queueTransport := strings.ToLower(strings.TrimSpace(opts.QueueTransport))
	if queueTransport == "" {
		queueTransport = "rabbitmq"
	}
	workerSize := opts.WorkerSize
	if workerSize <= 0 {
		workerSize = 1
	}
	consumerQueues := make([]ConsumerQueue, 0, 3)
	if queueTransport == "rabbitmq" {
		topology := resolveRabbitMQPriorityTopology(opts)
		queueSpecs := []struct {
			queueName    string
			exchangeName string
		}{
			{queueName: topology.HighQueueName, exchangeName: topology.HighExchangeName},
			{queueName: topology.NormalQueueName, exchangeName: topology.NormalExchangeName},
			{queueName: topology.LowQueueName, exchangeName: topology.LowExchangeName},
		}
		for _, spec := range queueSpecs {
			rmqConsumer, consumerErr := NewRabbitMQConsumer(RabbitMQQueueOptions{
				URL:          opts.QueueRabbitMQURL,
				QueueName:    spec.queueName,
				ExchangeName: spec.exchangeName,
				MaxPriority:  opts.QueueRabbitMQMaxPriority,
				MaxLength:    opts.QueueRabbitMQMaxLength,
				Prefetch:     workerSize,
			})
			if consumerErr == nil {
				consumerQueues = append(consumerQueues, rmqConsumer)
				continue
			}
			log.Printf("init rabbitmq task consumer failed queue=%s: %v", spec.queueName, consumerErr)
			consumerQueues = append(consumerQueues, unavailableQueue{reason: fmt.Errorf("init rabbitmq task consumer failed queue=%s: %w", spec.queueName, consumerErr)})
		}
	} else {
		consumerQueues = append(consumerQueues, unavailableQueue{reason: fmt.Errorf("unsupported queue transport: %s", queueTransport)})
	}
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
	pools := make([]*Pool, 0, len(consumerQueues))
	for _, queue := range consumerQueues {
		pools = append(pools, NewPool(queue, store, exec, workerSize, opts.OnFinish, opts.InputRetryMaxAttempts, opts.InputRetryDelay))
	}
	return &Runtime{
		store:  store,
		queues: consumerQueues,
		pools:  pools,
	}
}

func (r *Runtime) Start(ctx context.Context) {
	if r == nil {
		return
	}
	var wg sync.WaitGroup
	for _, pool := range r.pools {
		if pool == nil {
			continue
		}
		wg.Add(1)
		go func(p *Pool) {
			defer wg.Done()
			p.Start(ctx)
		}(pool)
	}
	<-ctx.Done()
	wg.Wait()
	for _, queue := range r.queues {
		if closer, ok := any(queue).(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
}

func (r *Runtime) Get(taskID string) (*Task, bool) {
	return r.store.Get(strings.TrimSpace(taskID))
}

func (r *Runtime) MarkFailed(taskID string, message string) (*Task, error) {
	return r.store.MarkFailed(strings.TrimSpace(taskID), strings.TrimSpace(message))
}

type rabbitMQPriorityTopology struct {
	HighQueueName      string
	NormalQueueName    string
	LowQueueName       string
	HighExchangeName   string
	NormalExchangeName string
	LowExchangeName    string
}

func resolveRabbitMQPriorityTopology(opts RuntimeOptions) rabbitMQPriorityTopology {
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

var ErrInvalidFakeLLMPrompt = errors.New("invalid fake llm prompt")
var ErrInvalidLLMPrompt = errors.New("invalid llm prompt")
var ErrInvalidSummaryPrompt = errors.New("invalid summary prompt")
var ErrInvalidAgentGoal = errors.New("invalid agent goal")
var ErrInvalidRAGRoomID = errors.New("invalid rag room id")
var ErrInvalidRAGSearchQuery = errors.New("invalid rag search query")
var ErrInvalidRAGMemorySequenceRange = errors.New("invalid rag memory sequence range")
