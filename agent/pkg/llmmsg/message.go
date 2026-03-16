package llmmsg

import "encoding/json"

type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

type Ingress struct {
	TaskID    string          `json:"task_id"`
	RequestID string          `json:"request_id"`
	UserID    string          `json:"user_id"`
	TaskType  string          `json:"task_type"`
	Priority  Priority        `json:"priority"`
	Input     json.RawMessage `json:"input,omitempty"`
	CreatedAt int64           `json:"created_at"`
}

type Result struct {
	TaskID      string          `json:"task_id"`
	RequestID   string          `json:"request_id"`
	TaskType    string          `json:"task_type"`
	Status      string          `json:"status"`
	Output      json.RawMessage `json:"output,omitempty"`
	OutputText  string          `json:"output_text"`
	Error       string          `json:"error"`
	StartedAt   int64           `json:"started_at"`
	FinishedAt  int64           `json:"finished_at"`
	WorkerLabel string          `json:"worker_label"`
}
