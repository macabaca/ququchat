package task

import "time"

type Type string

const (
	TypeAdd     Type = "add"
	TypeFakeLLM Type = "fake_llm"
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

type AddPayload struct {
	A int64
	B int64
}

type FakeLLMPayload struct {
	Prompt  string
	SleepMs int64
}

type Payload struct {
	Add     *AddPayload
	FakeLLM *FakeLLMPayload
}

type Result struct {
	AddSum *int64
	Text   *string
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
	if t.Payload.Add != nil {
		addCopy := *t.Payload.Add
		next.Payload.Add = &addCopy
	}
	if t.Payload.FakeLLM != nil {
		fakeLLMCopy := *t.Payload.FakeLLM
		next.Payload.FakeLLM = &fakeLLMCopy
	}
	if t.Result.AddSum != nil {
		sumCopy := *t.Result.AddSum
		next.Result.AddSum = &sumCopy
	}
	if t.Result.Text != nil {
		textCopy := *t.Result.Text
		next.Result.Text = &textCopy
	}
	return &next
}
