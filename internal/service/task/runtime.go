package tasksvc

import (
	"context"
	"errors"
	"time"
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
	OnFinish                         func(ctx context.Context, doneTask *Task)
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
