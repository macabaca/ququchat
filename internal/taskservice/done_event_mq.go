package taskservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	tasksvc "ququchat/internal/taskservice/task"
)

const wsCommandRequestIDPrefix = "ws_command"

type DoneEvent struct {
	TaskID           string                 `json:"task_id"`
	RequestID        string                 `json:"request_id,omitempty"`
	EventType        string                 `json:"event_type,omitempty"`
	Status           tasksvc.Status         `json:"status"`
	Final            string                 `json:"final,omitempty"`
	Payload          map[string]interface{} `json:"payload,omitempty"`
	ErrorMessage     string                 `json:"error_message,omitempty"`
	RoomID           string                 `json:"room_id,omitempty"`
	UserID           string                 `json:"user_id,omitempty"`
	ParentMessageID  string                 `json:"parent_message_id,omitempty"`
	ParentSequenceID int64                  `json:"parent_sequence_id,omitempty"`
	Step             int                    `json:"step,omitempty"`
	Role             string                 `json:"role,omitempty"`
	Tool             string                 `json:"tool,omitempty"`
	Content          string                 `json:"content,omitempty"`
	DurationMs       int64                  `json:"duration_ms,omitempty"`
	PromptTokens     int                    `json:"prompt_tokens,omitempty"`
	CompletionTokens int                    `json:"completion_tokens,omitempty"`
	TotalTokens      int                    `json:"total_tokens,omitempty"`
}

type DoneEventPublisher interface {
	Publish(ctx context.Context, doneTask *tasksvc.Task) error
	PublishEvent(ctx context.Context, event DoneEvent) error
	Close() error
}

type RabbitMQDoneEventPublisher struct {
	queueName string
	conn      *amqp.Connection
	channel   *amqp.Channel
	closeOnce sync.Once
}

func NewRabbitMQDoneEventPublisher(url, queueName string, maxLength int, messageTTL time.Duration) (*RabbitMQDoneEventPublisher, error) {
	trimmedURL := strings.TrimSpace(url)
	trimmedQueue := strings.TrimSpace(queueName)
	if trimmedURL == "" || trimmedQueue == "" {
		return nil, errors.New("done event rabbitmq url or queue is empty")
	}
	conn, err := amqp.Dial(trimmedURL)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := ch.QueueDeclare(trimmedQueue, true, false, false, false, resolveDoneEventQueueDeclareArgs(maxLength, messageTTL)); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	return &RabbitMQDoneEventPublisher{
		queueName: trimmedQueue,
		conn:      conn,
		channel:   ch,
	}, nil
}

func resolveDoneEventQueueDeclareArgs(maxLength int, messageTTL time.Duration) amqp.Table {
	args := amqp.Table{
		"x-overflow": "reject-publish",
	}
	if maxLength > 0 {
		args["x-max-length"] = int32(maxLength)
	}
	if messageTTL > 0 {
		args["x-message-ttl"] = int32(messageTTL / time.Millisecond)
	}
	if len(args) == 1 {
		return nil
	}
	return args
}

func (p *RabbitMQDoneEventPublisher) Publish(ctx context.Context, doneTask *tasksvc.Task) error {
	if p == nil || doneTask == nil {
		return nil
	}
	event := BuildDoneEvent(doneTask)
	return p.PublishEvent(ctx, event)
}

func (p *RabbitMQDoneEventPublisher) PublishEvent(ctx context.Context, event DoneEvent) error {
	if p == nil {
		return nil
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.channel.PublishWithContext(ctx, "", p.queueName, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

func (p *RabbitMQDoneEventPublisher) Close() error {
	if p == nil {
		return nil
	}
	var closeErr error
	p.closeOnce.Do(func() {
		if p.channel != nil {
			if err := p.channel.Close(); err != nil {
				closeErr = err
			}
		}
		if p.conn != nil {
			if err := p.conn.Close(); err != nil && closeErr == nil {
				closeErr = err
			}
		}
	})
	return closeErr
}

func normalizeRetryMaxAttempts(attempts int) int {
	if attempts <= 0 {
		return 3
	}
	return attempts
}

func normalizeRetryDelay(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 500 * time.Millisecond
	}
	return delay
}

func sleepWithContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func BuildWSCommandRequestID(userID, roomID, parentMessageID string, parentSequenceID int64) string {
	return fmt.Sprintf("%s|%s|%s|%s|%d|%s",
		wsCommandRequestIDPrefix,
		strings.TrimSpace(userID),
		strings.TrimSpace(roomID),
		strings.TrimSpace(parentMessageID),
		parentSequenceID,
		uuid.NewString(),
	)
}

func ParseWSCommandRequestID(requestID string) (string, string, string, int64, bool) {
	parts := strings.Split(strings.TrimSpace(requestID), "|")
	if len(parts) < 4 {
		return "", "", "", 0, false
	}
	if parts[0] != wsCommandRequestIDPrefix {
		return "", "", "", 0, false
	}
	userID := strings.TrimSpace(parts[1])
	roomID := strings.TrimSpace(parts[2])
	if userID == "" || roomID == "" {
		return "", "", "", 0, false
	}
	parentMessageID := ""
	var parentSequenceID int64
	if len(parts) >= 6 {
		parentMessageID = strings.TrimSpace(parts[3])
		parsedSeq, err := strconv.ParseInt(strings.TrimSpace(parts[4]), 10, 64)
		if err == nil && parsedSeq > 0 {
			parentSequenceID = parsedSeq
		}
	}
	return userID, roomID, parentMessageID, parentSequenceID, true
}

func BuildDoneEvent(doneTask *tasksvc.Task) DoneEvent {
	event := DoneEvent{}
	if doneTask == nil {
		return event
	}
	final, payload := extractDoneResult(doneTask)
	event = DoneEvent{
		TaskID:       doneTask.ID,
		RequestID:    doneTask.RequestID,
		EventType:    "agent.done",
		Status:       doneTask.Status,
		Final:        strings.TrimSpace(final),
		Payload:      payload,
		ErrorMessage: strings.TrimSpace(doneTask.ErrorMessage),
	}
	if doneTask.Status == tasksvc.StatusFailed {
		event.EventType = "agent.error"
	}
	if userID, roomID, parentMessageID, parentSequenceID, ok := ParseWSCommandRequestID(doneTask.RequestID); ok {
		event.UserID = userID
		event.RoomID = roomID
		event.ParentMessageID = parentMessageID
		event.ParentSequenceID = parentSequenceID
	}
	return event
}
