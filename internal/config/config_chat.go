package config

import (
	"strings"
	"time"
)

type Chat struct {
	HistoryLimit int `yaml:"history_limit" json:"history_limit"`
}

type Task struct {
	QueueHighCap   int `yaml:"queue_high_cap" json:"queue_high_cap"`
	QueueNormalCap int `yaml:"queue_normal_cap" json:"queue_normal_cap"`
	QueueLowCap    int `yaml:"queue_low_cap" json:"queue_low_cap"`
	WorkerSize     int `yaml:"worker_size" json:"worker_size"`
}

type LLM struct {
	Transport         string `yaml:"transport" json:"transport"`
	RPM               int    `yaml:"rpm" json:"rpm"`
	TPM               int    `yaml:"tpm" json:"tpm"`
	RabbitMQURL       string `yaml:"rabbitmq_url" json:"rabbitmq_url"`
	RequestQueue      string `yaml:"request_queue" json:"request_queue"`
	WorkerSize        int    `yaml:"worker_size" json:"worker_size"`
	ResponseTimeoutMs int    `yaml:"response_timeout_ms" json:"response_timeout_ms"`
	APIKey            string `yaml:"api_key" json:"api_key"`
	BaseURL           string `yaml:"base_url" json:"base_url"`
	Model             string `yaml:"model" json:"model"`
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
