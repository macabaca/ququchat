package taskservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	tasksvc "ququchat/internal/taskservice/task"
)

const wsCommandRequestIDPrefix = "ws_command"

type DoneEvent struct {
	TaskID       string                 `json:"task_id"`
	RequestID    string                 `json:"request_id,omitempty"`
	Status       tasksvc.Status         `json:"status"`
	Final        string                 `json:"final,omitempty"`
	Payload      map[string]interface{} `json:"payload,omitempty"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	RoomID       string                 `json:"room_id,omitempty"`
	UserID       string                 `json:"user_id,omitempty"`
}

type DoneEventPublisher interface {
	Publish(ctx context.Context, doneTask *tasksvc.Task) error
	Close() error
}

type RabbitMQDoneEventPublisher struct {
	queueName string
	conn      *amqp.Connection
	channel   *amqp.Channel
	closeOnce sync.Once
}

func NewRabbitMQDoneEventPublisher(url, queueName string) (*RabbitMQDoneEventPublisher, error) {
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
	if _, err := ch.QueueDeclare(trimmedQueue, true, false, false, false, nil); err != nil {
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

func (p *RabbitMQDoneEventPublisher) Publish(ctx context.Context, doneTask *tasksvc.Task) error {
	if p == nil || doneTask == nil {
		return nil
	}
	event := BuildDoneEvent(doneTask)
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

func BuildWSCommandRequestID(userID, roomID string) string {
	return fmt.Sprintf("%s|%s|%s|%s", wsCommandRequestIDPrefix, strings.TrimSpace(userID), strings.TrimSpace(roomID), uuid.NewString())
}

func ParseWSCommandRequestID(requestID string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(requestID), "|")
	if len(parts) < 4 {
		return "", "", false
	}
	if parts[0] != wsCommandRequestIDPrefix {
		return "", "", false
	}
	userID := strings.TrimSpace(parts[1])
	roomID := strings.TrimSpace(parts[2])
	if userID == "" || roomID == "" {
		return "", "", false
	}
	return userID, roomID, true
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
		Status:       doneTask.Status,
		Final:        strings.TrimSpace(final),
		Payload:      payload,
		ErrorMessage: strings.TrimSpace(doneTask.ErrorMessage),
	}
	if userID, roomID, ok := ParseWSCommandRequestID(doneTask.RequestID); ok {
		event.UserID = userID
		event.RoomID = roomID
	}
	return event
}
