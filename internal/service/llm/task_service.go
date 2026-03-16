package llmsvc

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"ququchat/agent/pkg/llmmsg"
)

var ErrTaskTypeRequired = errors.New("task_type_required")
var ErrUnsupportedTaskType = errors.New("unsupported_task_type")
var ErrTaskInputRequired = errors.New("task_input_required")
var ErrInvalidTaskInput = errors.New("invalid_task_input")
var ErrUserIDRequired = errors.New("user_id_required")
var ErrPromptRequired = errors.New("prompt_required")

type SubmitTaskRequest struct {
	RequestID string          `json:"request_id"`
	UserID    string          `json:"user_id"`
	TaskType  string          `json:"task_type"`
	Priority  string          `json:"priority"`
	Input     json.RawMessage `json:"input"`
}

type TaskResultEnvelope struct {
	Type       string          `json:"type"`
	TaskID     string          `json:"task_id"`
	RequestID  string          `json:"request_id"`
	TaskType   string          `json:"task_type"`
	Status     string          `json:"status"`
	Output     json.RawMessage `json:"output,omitempty"`
	OutputText string          `json:"output_text,omitempty"`
	Error      string          `json:"error,omitempty"`
	StartedAt  int64           `json:"started_at"`
	FinishedAt int64           `json:"finished_at"`
}

type fakeLLMInput struct {
	Prompt  string `json:"prompt"`
	SleepMs int64  `json:"sleep_ms"`
}

type fakeLLMOutput struct {
	Text string `json:"text"`
}

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) BuildIngress(req *SubmitTaskRequest, authUserID string) (*llmmsg.Ingress, string, error) {
	if req == nil {
		return nil, "", ErrTaskTypeRequired
	}
	taskType := strings.ToLower(strings.TrimSpace(req.TaskType))
	if taskType == "" {
		return nil, "", ErrTaskTypeRequired
	}
	input, err := s.normalizeTaskInput(taskType, req.Input)
	if err != nil {
		return nil, "", err
	}
	userID := strings.TrimSpace(authUserID)
	if userID == "" {
		userID = strings.TrimSpace(req.UserID)
	}
	if userID == "" {
		return nil, "", ErrUserIDRequired
	}
	requestID := strings.TrimSpace(req.RequestID)
	taskID := requestID
	if taskID == "" {
		taskID = strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "")
	}
	return &llmmsg.Ingress{
		TaskID:    taskID,
		RequestID: requestID,
		UserID:    userID,
		TaskType:  taskType,
		Priority:  mapPriority(req.Priority),
		Input:     input,
		CreatedAt: time.Now().UnixMilli(),
	}, userID, nil
}

func (s *Service) BuildResultEnvelope(res *llmmsg.Result, fallbackTaskType string) (*TaskResultEnvelope, error) {
	if res == nil {
		return nil, errors.New("llm_result_required")
	}
	taskType := strings.ToLower(strings.TrimSpace(res.TaskType))
	if taskType == "" {
		taskType = strings.ToLower(strings.TrimSpace(fallbackTaskType))
	}
	if taskType == "" {
		taskType = "unknown"
	}
	output, err := s.normalizeResultOutput(taskType, res.Output, res.OutputText)
	if err != nil {
		return nil, err
	}
	return &TaskResultEnvelope{
		Type:       "llm_task_result",
		TaskID:     res.TaskID,
		RequestID:  res.RequestID,
		TaskType:   taskType,
		Status:     res.Status,
		Output:     output,
		OutputText: res.OutputText,
		Error:      res.Error,
		StartedAt:  res.StartedAt,
		FinishedAt: res.FinishedAt,
	}, nil
}

func (s *Service) BuildFakeRequestInput(prompt string, sleepMs int64) (json.RawMessage, error) {
	in := fakeLLMInput{
		Prompt:  strings.TrimSpace(prompt),
		SleepMs: sleepMs,
	}
	if in.Prompt == "" {
		return nil, ErrPromptRequired
	}
	b, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Service) normalizeTaskInput(taskType string, raw json.RawMessage) (json.RawMessage, error) {
	switch taskType {
	case "fake_llm":
		if len(raw) == 0 {
			return nil, ErrTaskInputRequired
		}
		var in fakeLLMInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, ErrInvalidTaskInput
		}
		in.Prompt = strings.TrimSpace(in.Prompt)
		if in.Prompt == "" {
			return nil, ErrPromptRequired
		}
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		return b, nil
	default:
		return nil, ErrUnsupportedTaskType
	}
}

func (s *Service) normalizeResultOutput(taskType string, raw json.RawMessage, outputText string) (json.RawMessage, error) {
	if len(raw) > 0 {
		return raw, nil
	}
	switch taskType {
	case "fake_llm":
		b, err := json.Marshal(fakeLLMOutput{Text: outputText})
		if err != nil {
			return nil, err
		}
		return b, nil
	default:
		if strings.TrimSpace(outputText) == "" {
			return nil, nil
		}
		b, err := json.Marshal(map[string]string{"text": outputText})
		if err != nil {
			return nil, err
		}
		return b, nil
	}
}

func mapPriority(raw string) llmmsg.Priority {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high":
		return llmmsg.PriorityHigh
	case "low":
		return llmmsg.PriorityLow
	default:
		return llmmsg.PriorityNormal
	}
}
