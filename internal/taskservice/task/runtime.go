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
	DoneEventQueueMaxLength          int
	DoneEventQueueMessageTTL         time.Duration
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
	LLMMQMaxLength                   int
	LLMMQMessageTTL                  time.Duration
	LLMMQTimeout                     time.Duration
	LLMAPIKey                        string
	LLMBaseURL                       string
	LLMModel                         string
	AIGCClient                       AIGCClient
	AIGCTransport                    string
	AIGCMQURL                        string
	AIGCMQQueue                      string
	AIGCMQMaxLength                  int
	AIGCMQMessageTTL                 time.Duration
	AIGCMQTimeout                    time.Duration
	EmbeddingProvider                EmbeddingProvider
	VectorStore                      VectorStore
	EmbeddingModelRaw                string
	EmbeddingModelSummary            string
	SummaryVectorDim                 int
	RAGStopPhrases                   []string
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
		baseQueueName := strings.TrimSpace(opts.QueueRabbitMQName)
		if baseQueueName == "" {
			baseQueueName = "ququchat.task.queue"
		}
		baseExchangeName := strings.TrimSpace(opts.QueueRabbitMQExchange)
		if baseExchangeName == "" {
			baseExchangeName = baseQueueName + ".exchange"
		}
		queueSpecs := []struct {
			queueName    string
			exchangeName string
		}{
			{queueName: baseQueueName, exchangeName: baseExchangeName},
		}
		seen := make(map[string]struct{}, len(queueSpecs))
		for _, spec := range queueSpecs {
			queueName := strings.TrimSpace(spec.queueName)
			exchangeName := strings.TrimSpace(spec.exchangeName)
			if queueName == "" {
				continue
			}
			if exchangeName == "" {
				exchangeName = queueName + ".exchange"
			}
			key := queueName + "|" + exchangeName
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			queueOpts := RabbitMQQueueOptions{
				URL:          opts.QueueRabbitMQURL,
				QueueName:    queueName,
				ExchangeName: exchangeName,
				MaxPriority:  opts.QueueRabbitMQMaxPriority,
				MaxLength:    opts.QueueRabbitMQMaxLength,
				Prefetch:     workerSize,
			}
			var (
				rmqConsumer *RabbitMQConsumer
				consumerErr error
			)
			rmqConsumer, consumerErr = NewRabbitMQConsumer(queueOpts)
			if consumerErr == nil {
				consumerQueues = append(consumerQueues, rmqConsumer)
				log.Printf("init rabbitmq task consumer ok queue=%s exchange=%s prefetch=%d", queueName, exchangeName, workerSize)
				continue
			}
			log.Printf("init rabbitmq task consumer failed queue=%s exchange=%s: %v", queueName, exchangeName, consumerErr)
			consumerQueues = append(consumerQueues, unavailableQueue{reason: fmt.Errorf("init rabbitmq task consumer failed queue=%s exchange=%s: %w", queueName, exchangeName, consumerErr)})
		}
	} else {
		consumerQueues = append(consumerQueues, unavailableQueue{reason: fmt.Errorf("unsupported queue transport: %s", queueTransport)})
	}
	store := opts.Store
	if store == nil {
		store = NewMemoryStore()
	}
	llmClient := opts.LLMClient
	if llmClient != nil && strings.EqualFold(strings.TrimSpace(opts.LLMTransport), "rabbitmq") && strings.TrimSpace(opts.LLMMQQueue) != "" {
		log.Printf("LLM 请求队列启动成功，queue=%s transport=rabbitmq", strings.TrimSpace(opts.LLMMQQueue))
	}
	if llmClient == nil {
		if strings.EqualFold(strings.TrimSpace(opts.LLMTransport), "rabbitmq") &&
			strings.TrimSpace(opts.LLMMQURL) != "" &&
			strings.TrimSpace(opts.LLMMQQueue) != "" {
			client, err := llmmq.NewClient(llmmq.ClientOptions{
				URL:             opts.LLMMQURL,
				RequestQueue:    opts.LLMMQQueue,
				MaxLength:       opts.LLMMQMaxLength,
				MessageTTL:      opts.LLMMQMessageTTL,
				ResponseTimeout: opts.LLMMQTimeout,
			})
			if err == nil {
				llmClient = client
				log.Printf("LLM 请求队列启动成功，queue=%s transport=rabbitmq", strings.TrimSpace(opts.LLMMQQueue))
			} else {
				log.Printf("LLM 请求队列启动失败，queue=%s transport=rabbitmq err=%v", strings.TrimSpace(opts.LLMMQQueue), err)
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
	if aigcClient != nil && strings.EqualFold(strings.TrimSpace(opts.AIGCTransport), "rabbitmq") && strings.TrimSpace(opts.AIGCMQQueue) != "" {
		log.Printf("AIGC 请求队列启动成功，queue=%s transport=rabbitmq", strings.TrimSpace(opts.AIGCMQQueue))
	}
	if aigcClient == nil {
		if strings.EqualFold(strings.TrimSpace(opts.AIGCTransport), "rabbitmq") &&
			strings.TrimSpace(opts.AIGCMQURL) != "" &&
			strings.TrimSpace(opts.AIGCMQQueue) != "" {
			client, err := aigcmq.NewClient(aigcmq.ClientOptions{
				URL:             opts.AIGCMQURL,
				RequestQueue:    opts.AIGCMQQueue,
				MaxLength:       opts.AIGCMQMaxLength,
				MessageTTL:      opts.AIGCMQMessageTTL,
				ResponseTimeout: opts.AIGCMQTimeout,
			})
			if err == nil {
				aigcClient = client
				log.Printf("AIGC 请求队列启动成功，queue=%s transport=rabbitmq", strings.TrimSpace(opts.AIGCMQQueue))
			} else {
				log.Printf("AIGC 请求队列启动失败，queue=%s transport=rabbitmq err=%v", strings.TrimSpace(opts.AIGCMQQueue), err)
			}
		} else if strings.EqualFold(strings.TrimSpace(opts.AIGCTransport), "rabbitmq") {
			log.Printf("AIGC 请求队列启动失败，queue=%s transport=rabbitmq err=missing mq url or queue", strings.TrimSpace(opts.AIGCMQQueue))
		}
	}
	mcpMultiClient := opts.MCPMultiClient
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
