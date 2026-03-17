package tasksvc

import (
	"encoding/json"
	"time"
)

type Type string

const (
	TypeFakeLLM Type = "fake_llm"
	TypeLLM     Type = "llm"
	TypeSummary Type = "summary"
	TypeAgent   Type = "agent"
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

type SummaryPayload struct {
	Prompt string
}

type AgentPayload struct {
	Goal           string
	RecentMessages []string
	MaxSteps       int
}

type Payload struct {
	FakeLLM *FakeLLMPayload
	LLM     *LLMPayload
	Summary *SummaryPayload
	Agent   *AgentPayload
}

type Result struct {
	Text    *string
	Final   *string
	Payload map[string]interface{}
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
	if t.Payload.Summary != nil {
		payloadCopy := *t.Payload.Summary
		next.Payload.Summary = &payloadCopy
	}
	if t.Payload.Agent != nil {
		payloadCopy := *t.Payload.Agent
		if t.Payload.Agent.RecentMessages != nil {
			payloadCopy.RecentMessages = append([]string(nil), t.Payload.Agent.RecentMessages...)
		}
		next.Payload.Agent = &payloadCopy
	}
	if t.Result.Text != nil {
		textCopy := *t.Result.Text
		next.Result.Text = &textCopy
	}
	if t.Result.Final != nil {
		finalCopy := *t.Result.Final
		next.Result.Final = &finalCopy
	}
	if t.Result.Payload != nil {
		payloadCopy := cloneResultPayload(t.Result.Payload)
		next.Result.Payload = payloadCopy
	}
	return &next
}

func cloneResultPayload(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	b, err := json.Marshal(src)
	if err != nil {
		dst := make(map[string]interface{}, len(src))
		for k, v := range src {
			dst[k] = v
		}
		return dst
	}
	var dst map[string]interface{}
	if err := json.Unmarshal(b, &dst); err != nil {
		dst = make(map[string]interface{}, len(src))
		for k, v := range src {
			dst[k] = v
		}
	}
	return dst
}
