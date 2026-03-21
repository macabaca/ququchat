package taskservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	tasksvc "ququchat/internal/service/task"
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

type DoneEventHandler func(ctx context.Context, event DoneEvent) error

type RabbitMQDoneEventConsumer struct {
	queueName        string
	consumerTag      string
	retryMaxAttempts int
	retryDelay       time.Duration
	onMaxRetry       DoneEventHandler
	conn             *amqp.Connection
	channel          *amqp.Channel
}

type RabbitMQDoneEventConsumerOptions struct {
	URL              string
	QueueName        string
	ConsumerTag      string
	Prefetch         int
	RetryMaxAttempts int
	RetryDelay       time.Duration
	OnMaxRetry       DoneEventHandler
}

func NewRabbitMQDoneEventConsumer(opts RabbitMQDoneEventConsumerOptions) (*RabbitMQDoneEventConsumer, error) {
	url := strings.TrimSpace(opts.URL)
	queueName := strings.TrimSpace(opts.QueueName)
	if url == "" || queueName == "" {
		return nil, errors.New("done event rabbitmq url or queue is empty")
	}
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	prefetch := opts.Prefetch
	if prefetch <= 0 {
		prefetch = 1
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	consumerTag := strings.TrimSpace(opts.ConsumerTag)
	if consumerTag == "" {
		consumerTag = "ququchat.done.consumer." + uuid.NewString()
	}
	return &RabbitMQDoneEventConsumer{
		queueName:        queueName,
		consumerTag:      consumerTag,
		retryMaxAttempts: normalizeRetryMaxAttempts(opts.RetryMaxAttempts),
		retryDelay:       normalizeRetryDelay(opts.RetryDelay),
		onMaxRetry:       opts.OnMaxRetry,
		conn:             conn,
		channel:          ch,
	}, nil
}

func (c *RabbitMQDoneEventConsumer) Start(ctx context.Context, handler DoneEventHandler) error {
	if c == nil {
		return nil
	}
	deliveries, err := c.channel.Consume(c.queueName, c.consumerTag, false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			_ = c.channel.Cancel(c.consumerTag, false)
			_ = c.channel.Close()
			_ = c.conn.Close()
			return nil
		case msg, ok := <-deliveries:
			if !ok {
				return nil
			}
			var event DoneEvent
			if err := json.Unmarshal(msg.Body, &event); err != nil {
				_ = msg.Ack(false)
				continue
			}
			if handler == nil {
				_ = msg.Ack(false)
				continue
			}
			if err := handler(ctx, event); err != nil {
				if retryErr := c.retryOrFinalize(ctx, msg, event, err); retryErr != nil {
					log.Printf("[done-event-consumer] retry/finalize failed task=%s err=%v", event.TaskID, retryErr)
				}
				continue
			}
			_ = msg.Ack(false)
		}
	}
}

func (c *RabbitMQDoneEventConsumer) retryOrFinalize(ctx context.Context, msg amqp.Delivery, event DoneEvent, handlerErr error) error {
	retryCount := deliveryRetryCount(msg.Headers)
	nextRetryCount := retryCount + 1
	log.Printf("[done-event-consumer] task=%s retry=%d/%d err=%v", event.TaskID, nextRetryCount, c.retryMaxAttempts, handlerErr)
	if nextRetryCount > c.retryMaxAttempts {
		_ = msg.Ack(false)
		if c.onMaxRetry != nil {
			return c.onMaxRetry(ctx, event)
		}
		return nil
	}
	if !sleepWithContext(ctx, c.retryDelay) {
		return ctx.Err()
	}
	headers := cloneHeaders(msg.Headers)
	headers["x-retry-count"] = int32(nextRetryCount)
	headers["x-last-error"] = handlerErr.Error()
	if err := c.channel.PublishWithContext(ctx, "", c.queueName, false, false, amqp.Publishing{
		ContentType:  msg.ContentType,
		DeliveryMode: msg.DeliveryMode,
		Body:         msg.Body,
		Timestamp:    time.Now(),
		Headers:      headers,
	}); err != nil {
		return err
	}
	return msg.Ack(false)
}

func deliveryRetryCount(headers amqp.Table) int {
	if headers == nil {
		return 0
	}
	value, ok := headers["x-retry-count"]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return n
		}
	}
	return 0
}

func cloneHeaders(src amqp.Table) amqp.Table {
	if len(src) == 0 {
		return amqp.Table{}
	}
	dst := amqp.Table{}
	for k, v := range src {
		dst[k] = v
	}
	return dst
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
