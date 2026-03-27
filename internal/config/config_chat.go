package config

import (
	"strings"
	"time"
)

type Chat struct {
	HistoryLimit int `yaml:"history_limit" json:"history_limit"`
}

type Task struct {
	QueueHighCap                int    `yaml:"queue_high_cap" json:"queue_high_cap"`
	QueueNormalCap              int    `yaml:"queue_normal_cap" json:"queue_normal_cap"`
	QueueLowCap                 int    `yaml:"queue_low_cap" json:"queue_low_cap"`
	QueueTransport              string `yaml:"queue_transport" json:"queue_transport"`
	QueueRabbitMQURL            string `yaml:"queue_rabbitmq_url" json:"queue_rabbitmq_url"`
	QueueRabbitMQName           string `yaml:"queue_rabbitmq_name" json:"queue_rabbitmq_name"`
	QueueRabbitMQExchange       string `yaml:"queue_rabbitmq_exchange" json:"queue_rabbitmq_exchange"`
	QueueRabbitMQMaxPriority    int    `yaml:"queue_rabbitmq_max_priority" json:"queue_rabbitmq_max_priority"`
	QueueRabbitMQMaxLength      int    `yaml:"queue_rabbitmq_max_length" json:"queue_rabbitmq_max_length"`
	DoneEventMQURL              string `yaml:"done_event_rabbitmq_url" json:"done_event_rabbitmq_url"`
	DoneEventQueue              string `yaml:"done_event_queue_name" json:"done_event_queue_name"`
	DoneEventQueueMaxLength     int    `yaml:"done_event_queue_max_length" json:"done_event_queue_max_length"`
	DoneEventQueueMessageTTLms  int    `yaml:"done_event_queue_message_ttl_ms" json:"done_event_queue_message_ttl_ms"`
	InputRetryMaxAttempts       int    `yaml:"input_retry_max_attempts" json:"input_retry_max_attempts"`
	InputRetryDelayMs           int    `yaml:"input_retry_delay_ms" json:"input_retry_delay_ms"`
	DonePublishRetryMaxAttempts int    `yaml:"done_publish_retry_max_attempts" json:"done_publish_retry_max_attempts"`
	DonePublishRetryDelayMs     int    `yaml:"done_publish_retry_delay_ms" json:"done_publish_retry_delay_ms"`
	DoneConsumeRetryMaxAttempts int    `yaml:"done_consume_retry_max_attempts" json:"done_consume_retry_max_attempts"`
	DoneConsumeRetryDelayMs     int    `yaml:"done_consume_retry_delay_ms" json:"done_consume_retry_delay_ms"`
	WorkerSize                  int    `yaml:"worker_size" json:"worker_size"`
}

type LLM struct {
	Transport                string `yaml:"transport" json:"transport"`
	RPM                      int    `yaml:"rpm" json:"rpm"`
	TPM                      int    `yaml:"tpm" json:"tpm"`
	RabbitMQURL              string `yaml:"rabbitmq_url" json:"rabbitmq_url"`
	RequestQueue             string `yaml:"request_queue" json:"request_queue"`
	RequestQueueMaxLength    int    `yaml:"request_queue_max_length" json:"request_queue_max_length"`
	RequestQueueMessageTTLms int    `yaml:"request_queue_message_ttl_ms" json:"request_queue_message_ttl_ms"`
	WorkerSize               int    `yaml:"worker_size" json:"worker_size"`
	ResponseTimeoutMs        int    `yaml:"response_timeout_ms" json:"response_timeout_ms"`
	APIKey                   string `yaml:"api_key" json:"api_key"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
	Model                    string `yaml:"model" json:"model"`
}

type AIGC struct {
	Transport                string `yaml:"transport" json:"transport"`
	IPM                      int    `yaml:"ipm" json:"ipm"`
	IPD                      int    `yaml:"ipd" json:"ipd"`
	RabbitMQURL              string `yaml:"rabbitmq_url" json:"rabbitmq_url"`
	RequestQueue             string `yaml:"request_queue" json:"request_queue"`
	RequestQueueMaxLength    int    `yaml:"request_queue_max_length" json:"request_queue_max_length"`
	RequestQueueMessageTTLms int    `yaml:"request_queue_message_ttl_ms" json:"request_queue_message_ttl_ms"`
	WorkerSize               int    `yaml:"worker_size" json:"worker_size"`
	ResponseTimeoutMs        int    `yaml:"response_timeout_ms" json:"response_timeout_ms"`
	APIKey                   string `yaml:"api_key" json:"api_key"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
	Model                    string `yaml:"model" json:"model"`
}

type Embedding struct {
	Transport                string `yaml:"transport" json:"transport"`
	RPM                      int    `yaml:"rpm" json:"rpm"`
	TPM                      int    `yaml:"tpm" json:"tpm"`
	RabbitMQURL              string `yaml:"rabbitmq_url" json:"rabbitmq_url"`
	RequestQueue             string `yaml:"request_queue" json:"request_queue"`
	RequestQueueMaxLength    int    `yaml:"request_queue_max_length" json:"request_queue_max_length"`
	RequestQueueMessageTTLms int    `yaml:"request_queue_message_ttl_ms" json:"request_queue_message_ttl_ms"`
	WorkerSize               int    `yaml:"worker_size" json:"worker_size"`
	ResponseTimeoutMs        int    `yaml:"response_timeout_ms" json:"response_timeout_ms"`
	APIKey                   string `yaml:"api_key" json:"api_key"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
	Model                    string `yaml:"model" json:"model"`
}

type Vector struct {
	Provider         string `yaml:"provider" json:"provider"`
	QdrantURL        string `yaml:"qdrant_url" json:"qdrant_url"`
	APIKey           string `yaml:"api_key" json:"api_key"`
	Collection       string `yaml:"collection" json:"collection"`
	TimeoutMs        int    `yaml:"timeout_ms" json:"timeout_ms"`
	RawVectorDim     int    `yaml:"raw_vector_dim" json:"raw_vector_dim"`
	SummaryVectorDim int    `yaml:"summary_vector_dim" json:"summary_vector_dim"`
	Distance         string `yaml:"distance" json:"distance"`
}

func (l LLM) TransportOrDefault() string {
	if strings.TrimSpace(l.Transport) != "" {
		return strings.TrimSpace(l.Transport)
	}
	return "direct"
}

func (l LLM) BaseURLOrDefault() string {
	if strings.TrimSpace(l.BaseURL) != "" {
		return strings.TrimSpace(l.BaseURL)
	}
	return "https://api.openai.com/v1"
}

func (l LLM) ModelOrDefault() string {
	if strings.TrimSpace(l.Model) != "" {
		return strings.TrimSpace(l.Model)
	}
	return "gpt-4o-mini"
}

func (l LLM) RequestQueueOrDefault() string {
	if strings.TrimSpace(l.RequestQueue) != "" {
		return strings.TrimSpace(l.RequestQueue)
	}
	return "ququchat.llm.request"
}

func (l LLM) RequestQueueMaxLengthOrDefault() int {
	if l.RequestQueueMaxLength > 0 {
		return l.RequestQueueMaxLength
	}
	return 0
}

func (l LLM) RequestQueueMessageTTLOrDefault() time.Duration {
	if l.RequestQueueMessageTTLms > 0 {
		return time.Duration(l.RequestQueueMessageTTLms) * time.Millisecond
	}
	return 0
}

func (l LLM) WorkerSizeOrDefault() int {
	if l.WorkerSize > 0 {
		return l.WorkerSize
	}
	return 1
}

func (l LLM) RPMOrDefault() int {
	if l.RPM > 0 {
		return l.RPM
	}
	return 0
}

func (l LLM) TPMOrDefault() int {
	if l.TPM > 0 {
		return l.TPM
	}
	return 0
}

func (l LLM) ResponseTimeoutOrDefault() time.Duration {
	if l.ResponseTimeoutMs > 0 {
		return time.Duration(l.ResponseTimeoutMs) * time.Millisecond
	}
	return 60 * time.Second
}

func (a AIGC) TransportOrDefault() string {
	if strings.TrimSpace(a.Transport) != "" {
		return strings.TrimSpace(a.Transport)
	}
	return "direct"
}

func (a AIGC) BaseURLOrDefault() string {
	if strings.TrimSpace(a.BaseURL) != "" {
		return strings.TrimSpace(a.BaseURL)
	}
	return "https://api.openai.com/v1"
}

func (a AIGC) ModelOrDefault() string {
	if strings.TrimSpace(a.Model) != "" {
		return strings.TrimSpace(a.Model)
	}
	return "Kwai-Kolors/Kolors"
}

func (a AIGC) RequestQueueOrDefault() string {
	if strings.TrimSpace(a.RequestQueue) != "" {
		return strings.TrimSpace(a.RequestQueue)
	}
	return "ququchat.aigc.request"
}

func (a AIGC) RequestQueueMaxLengthOrDefault() int {
	if a.RequestQueueMaxLength > 0 {
		return a.RequestQueueMaxLength
	}
	return 0
}

func (a AIGC) RequestQueueMessageTTLOrDefault() time.Duration {
	if a.RequestQueueMessageTTLms > 0 {
		return time.Duration(a.RequestQueueMessageTTLms) * time.Millisecond
	}
	return 0
}

func (a AIGC) WorkerSizeOrDefault() int {
	if a.WorkerSize > 0 {
		return a.WorkerSize
	}
	return 1
}

func (a AIGC) IPMOrDefault() int {
	if a.IPM > 0 {
		return a.IPM
	}
	return 0
}

func (a AIGC) IPDOrDefault() int {
	if a.IPD > 0 {
		return a.IPD
	}
	return 0
}

func (a AIGC) ResponseTimeoutOrDefault() time.Duration {
	if a.ResponseTimeoutMs > 0 {
		return time.Duration(a.ResponseTimeoutMs) * time.Millisecond
	}
	return 120 * time.Second
}

func (e Embedding) TransportOrDefault() string {
	if strings.TrimSpace(e.Transport) != "" {
		return strings.TrimSpace(e.Transport)
	}
	return "direct"
}

func (e Embedding) BaseURLOrDefault() string {
	if strings.TrimSpace(e.BaseURL) != "" {
		return strings.TrimSpace(e.BaseURL)
	}
	return "https://api.openai.com/v1"
}

func (e Embedding) ModelOrDefault() string {
	if strings.TrimSpace(e.Model) != "" {
		return strings.TrimSpace(e.Model)
	}
	return "text-embedding-3-large"
}

func (e Embedding) RequestQueueOrDefault() string {
	if strings.TrimSpace(e.RequestQueue) != "" {
		return strings.TrimSpace(e.RequestQueue)
	}
	return "ququchat.embedding.request"
}

func (e Embedding) RequestQueueMaxLengthOrDefault() int {
	if e.RequestQueueMaxLength > 0 {
		return e.RequestQueueMaxLength
	}
	return 0
}

func (e Embedding) RequestQueueMessageTTLOrDefault() time.Duration {
	if e.RequestQueueMessageTTLms > 0 {
		return time.Duration(e.RequestQueueMessageTTLms) * time.Millisecond
	}
	return 0
}

func (e Embedding) WorkerSizeOrDefault() int {
	if e.WorkerSize > 0 {
		return e.WorkerSize
	}
	return 1
}

func (e Embedding) RPMOrDefault() int {
	if e.RPM > 0 {
		return e.RPM
	}
	return 0
}

func (e Embedding) TPMOrDefault() int {
	if e.TPM > 0 {
		return e.TPM
	}
	return 0
}

func (e Embedding) ResponseTimeoutOrDefault() time.Duration {
	if e.ResponseTimeoutMs > 0 {
		return time.Duration(e.ResponseTimeoutMs) * time.Millisecond
	}
	return 60 * time.Second
}

func (v Vector) ProviderOrDefault() string {
	if strings.TrimSpace(v.Provider) != "" {
		return strings.TrimSpace(v.Provider)
	}
	return "qdrant"
}

func (v Vector) QdrantURLOrDefault() string {
	if strings.TrimSpace(v.QdrantURL) != "" {
		return strings.TrimSpace(v.QdrantURL)
	}
	return "http://localhost:6333"
}

func (v Vector) CollectionOrDefault() string {
	if strings.TrimSpace(v.Collection) != "" {
		return strings.TrimSpace(v.Collection)
	}
	return "chat_segments"
}

func (v Vector) TimeoutOrDefault() time.Duration {
	if v.TimeoutMs > 0 {
		return time.Duration(v.TimeoutMs) * time.Millisecond
	}
	return 3 * time.Second
}

func (v Vector) RawVectorDimOrDefault() int {
	if v.RawVectorDim > 0 {
		return v.RawVectorDim
	}
	return 1024
}

func (v Vector) SummaryVectorDimOrDefault() int {
	if v.SummaryVectorDim > 0 {
		return v.SummaryVectorDim
	}
	return v.RawVectorDimOrDefault()
}

func (v Vector) DistanceOrDefault() string {
	if strings.TrimSpace(v.Distance) != "" {
		return strings.TrimSpace(v.Distance)
	}
	return "Cosine"
}

func (t Task) QueueHighCapOrDefault() int {
	if t.QueueHighCap > 0 {
		return t.QueueHighCap
	}
	return 256
}

func (t Task) QueueNormalCapOrDefault() int {
	if t.QueueNormalCap > 0 {
		return t.QueueNormalCap
	}
	return 512
}

func (t Task) QueueLowCapOrDefault() int {
	if t.QueueLowCap > 0 {
		return t.QueueLowCap
	}
	return 256
}

func (t Task) WorkerSizeOrDefault() int {
	if t.WorkerSize > 0 {
		return t.WorkerSize
	}
	return 2
}

func (t Task) QueueTransportOrDefault() string {
	if strings.TrimSpace(t.QueueTransport) != "" {
		return strings.TrimSpace(t.QueueTransport)
	}
	return "rabbitmq"
}

func (t Task) QueueRabbitMQNameOrDefault() string {
	if strings.TrimSpace(t.QueueRabbitMQName) != "" {
		return strings.TrimSpace(t.QueueRabbitMQName)
	}
	return "ququchat.task.queue"
}

func (t Task) QueueRabbitMQExchangeOrDefault() string {
	if strings.TrimSpace(t.QueueRabbitMQExchange) != "" {
		return strings.TrimSpace(t.QueueRabbitMQExchange)
	}
	return t.QueueRabbitMQNameOrDefault() + ".exchange"
}

func (t Task) QueueRabbitMQMaxPriorityOrDefault() int {
	if t.QueueRabbitMQMaxPriority > 0 {
		return t.QueueRabbitMQMaxPriority
	}
	return 10
}

func (t Task) QueueRabbitMQMaxLengthOrDefault() int {
	if t.QueueRabbitMQMaxLength > 0 {
		return t.QueueRabbitMQMaxLength
	}
	return 50000
}

func (t Task) DoneEventQueueOrDefault() string {
	if strings.TrimSpace(t.DoneEventQueue) != "" {
		return strings.TrimSpace(t.DoneEventQueue)
	}
	return "ququchat.task.done"
}

func (t Task) DoneEventQueueMaxLengthOrDefault() int {
	if t.DoneEventQueueMaxLength > 0 {
		return t.DoneEventQueueMaxLength
	}
	return 0
}

func (t Task) DoneEventQueueMessageTTLOrDefault() time.Duration {
	if t.DoneEventQueueMessageTTLms > 0 {
		return time.Duration(t.DoneEventQueueMessageTTLms) * time.Millisecond
	}
	return 0
}

func (t Task) DoneEventMQURLOrDefault() string {
	if strings.TrimSpace(t.DoneEventMQURL) != "" {
		return strings.TrimSpace(t.DoneEventMQURL)
	}
	return strings.TrimSpace(t.QueueRabbitMQURL)
}

func (t Task) InputRetryMaxAttemptsOrDefault() int {
	if t.InputRetryMaxAttempts > 0 {
		return t.InputRetryMaxAttempts
	}
	return 3
}

func (t Task) InputRetryDelayOrDefault() time.Duration {
	if t.InputRetryDelayMs > 0 {
		return time.Duration(t.InputRetryDelayMs) * time.Millisecond
	}
	return 500 * time.Millisecond
}

func (t Task) DonePublishRetryMaxAttemptsOrDefault() int {
	if t.DonePublishRetryMaxAttempts > 0 {
		return t.DonePublishRetryMaxAttempts
	}
	return 3
}

func (t Task) DonePublishRetryDelayOrDefault() time.Duration {
	if t.DonePublishRetryDelayMs > 0 {
		return time.Duration(t.DonePublishRetryDelayMs) * time.Millisecond
	}
	return 500 * time.Millisecond
}

func (t Task) DoneConsumeRetryMaxAttemptsOrDefault() int {
	if t.DoneConsumeRetryMaxAttempts > 0 {
		return t.DoneConsumeRetryMaxAttempts
	}
	return 3
}

func (t Task) DoneConsumeRetryDelayOrDefault() time.Duration {
	if t.DoneConsumeRetryDelayMs > 0 {
		return time.Duration(t.DoneConsumeRetryDelayMs) * time.Millisecond
	}
	return 500 * time.Millisecond
}

type File struct {
	UploadDir    string    `yaml:"upload_dir" json:"upload_dir"`
	MaxSizeBytes int64     `yaml:"max_size_bytes" json:"max_size_bytes"`
	Retention    string    `yaml:"retention" json:"retention"`
	Thumbnail    Thumbnail `yaml:"thumbnail" json:"thumbnail"`
}

func (f File) RetentionDuration() time.Duration {
	const defaultRetention = 30 * 24 * time.Hour
	if strings.TrimSpace(f.Retention) == "" {
		return defaultRetention
	}
	if d, err := time.ParseDuration(strings.TrimSpace(f.Retention)); err == nil && d > 0 {
		return d
	}
	return defaultRetention
}

type Thumbnail struct {
	MaxDimension   int    `yaml:"max_dimension" json:"max_dimension"`
	JPEGQuality    int    `yaml:"jpeg_quality" json:"jpeg_quality"`
	RetryCount     int    `yaml:"retry_count" json:"retry_count"`
	RetryDelay     string `yaml:"retry_delay" json:"retry_delay"`
	MaxSourceBytes int64  `yaml:"max_source_bytes" json:"max_source_bytes"`
}

func (t Thumbnail) MaxDimensionOrDefault() int {
	if t.MaxDimension > 0 {
		return t.MaxDimension
	}
	return 320
}

func (t Thumbnail) JPEGQualityOrDefault() int {
	if t.JPEGQuality > 0 && t.JPEGQuality <= 100 {
		return t.JPEGQuality
	}
	return 80
}

func (t Thumbnail) RetryCountOrDefault() int {
	if t.RetryCount > 0 {
		return t.RetryCount
	}
	return 3
}

func (t Thumbnail) RetryDelayDuration() time.Duration {
	if strings.TrimSpace(t.RetryDelay) == "" {
		return 200 * time.Millisecond
	}
	if d, err := time.ParseDuration(strings.TrimSpace(t.RetryDelay)); err == nil && d > 0 {
		return d
	}
	return 200 * time.Millisecond
}

func (t Thumbnail) MaxSourceBytesOrDefault() int64 {
	if t.MaxSourceBytes > 0 {
		return t.MaxSourceBytes
	}
	return int64(10 * 1024 * 1024)
}

type Avatar struct {
	MaxSizeBytes int64  `yaml:"max_size_bytes" json:"max_size_bytes"`
	Retention    string `yaml:"retention" json:"retention"`
	Permanent    *bool  `yaml:"permanent" json:"permanent"`
}

func (a Avatar) MaxSizeOrDefault() int64 {
	const defaultMax = int64(5 * 1024 * 1024)
	if a.MaxSizeBytes > 0 {
		return a.MaxSizeBytes
	}
	return defaultMax
}

func (a Avatar) PermanentOrDefault() bool {
	if a.Permanent != nil {
		return *a.Permanent
	}
	if strings.TrimSpace(a.Retention) == "" {
		return true
	}
	return false
}

func (a Avatar) RetentionDuration() time.Duration {
	if a.PermanentOrDefault() {
		return 0
	}
	const defaultRetention = 10 * 365 * 24 * time.Hour
	if strings.TrimSpace(a.Retention) == "" {
		return defaultRetention
	}
	if d, err := time.ParseDuration(strings.TrimSpace(a.Retention)); err == nil && d > 0 {
		return d
	}
	return defaultRetention
}
