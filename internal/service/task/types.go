package tasksvc

import "time"

type Type string

const (
	TypeFakeLLM Type = "fake_llm"
	TypeLLM     Type = "llm"
)

type Priority int

const (
	PriorityHigh   Priority = 1
	PriorityNormal Priority = 2
	PriorityLow    Priority = 3
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

type FakeLLMPayload struct {
	Prompt  string
	SleepMs int64
}

type LLMPayload struct {
	Prompt string
}

type Payload struct {
	FakeLLM *FakeLLMPayload
	LLM     *LLMPayload
}

type Result struct {
	Text *string
}

type Task struct {
	ID           string
	RequestID    string
	Type         Type
	Priority     Priority
	Status       Status
	Payload      Payload
	Result       Result
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (t *Task) Clone() *Task {
	if t == nil {
		return nil
	}
	next := *t
	if t.Payload.FakeLLM != nil {
		payloadCopy := *t.Payload.FakeLLM
		next.Payload.FakeLLM = &payloadCopy
	}
	if t.Payload.LLM != nil {
		payloadCopy := *t.Payload.LLM
		next.Payload.LLM = &payloadCopy
	}
	if t.Result.Text != nil {
		textCopy := *t.Result.Text
		next.Result.Text = &textCopy
	}
	return &next
}
