package taskservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"

	"ququchat/internal/models"
	tasksvc "ququchat/internal/service/task"
)

const wsCommandRequestIDPrefix = "ws_command"

const (
	DefaultDoneEventDLQExchange   = "ququchat.done_event.dlq.exchange"
	DefaultDoneEventDLQQueue      = "ququchat.done_event.dlq"
	DefaultDoneEventDLQRoutingKey = "done_event.dead"
)

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
	dlqExchange      string
	dlqQueue         string
	dlqRouting       string
	consumerTag      string
	retryMaxAttempts int
	retryDelay       time.Duration
	onMaxRetry       DoneEventHandler
	conn             *amqp.Connection
	channel          *amqp.Channel
	mu               sync.Mutex
}

type RabbitMQDoneEventConsumerOptions struct {
	URL              string
	QueueName        string
	QueueMaxLength   int
	QueueMessageTTL  time.Duration
	ConsumerTag      string
	Prefetch         int
	RetryMaxAttempts int
	RetryDelay       time.Duration
	OnMaxRetry       DoneEventHandler
}

type doneEventDeadLetterMessage struct {
	SourceQueue   string    `json:"source_queue"`
	Reason        string    `json:"reason"`
	RawBody       string    `json:"raw_body"`
	ContentType   string    `json:"content_type,omitempty"`
	MessageID     string    `json:"message_id,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
}

type RabbitMQDoneEventDeadLetterConsumer struct {
	queueName   string
	consumerTag string
	conn        *amqp.Connection
	channel     *amqp.Channel
	db          *gorm.DB
}

type RabbitMQDoneEventDeadLetterConsumerOptions struct {
	URL         string
	QueueName   string
	ConsumerTag string
	DB          *gorm.DB
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
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, resolveDoneEventQueueDeclareArgs(opts.QueueMaxLength, opts.QueueMessageTTL)); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.ExchangeDeclare(DefaultDoneEventDLQExchange, "direct", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if _, err := ch.QueueDeclare(DefaultDoneEventDLQQueue, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.QueueBind(DefaultDoneEventDLQQueue, DefaultDoneEventDLQRoutingKey, DefaultDoneEventDLQExchange, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.Qos(normalizePrefetch(opts.Prefetch), 0, false); err != nil {
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
		dlqExchange:      DefaultDoneEventDLQExchange,
		dlqQueue:         DefaultDoneEventDLQQueue,
		dlqRouting:       DefaultDoneEventDLQRoutingKey,
		consumerTag:      consumerTag,
		retryMaxAttempts: normalizeRetryMaxAttempts(opts.RetryMaxAttempts),
		retryDelay:       normalizeRetryDelay(opts.RetryDelay),
		onMaxRetry:       opts.OnMaxRetry,
		conn:             conn,
		channel:          ch,
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
				if publishErr := c.publishDeadLetter(ctx, msg, "invalid_done_event_message"); publishErr != nil {
					log.Printf("[done-event-consumer] publish dead letter failed, drop message err=%v message_id=%s correlation_id=%s queue=%s",
						publishErr,
						strings.TrimSpace(msg.MessageId),
						strings.TrimSpace(msg.CorrelationId),
						strings.TrimSpace(c.queueName),
					)
				}
				_ = msg.Ack(false)
				continue
			}
			if handler == nil {
				_ = msg.Ack(false)
				continue
			}
			if err := handler(ctx, event); err != nil {
				if retryErr := c.retryOrFinalize(ctx, msg, event, err); retryErr != nil {
					if publishErr := c.publishDeadLetter(ctx, msg, "done_event_retry_finalize_failed"); publishErr != nil {
						log.Printf("[done-event-consumer] retry/finalize and dead letter publish failed, drop message task=%s request_id=%s err=%v dlq_err=%v queue=%s",
							strings.TrimSpace(event.TaskID),
							strings.TrimSpace(event.RequestID),
							retryErr,
							publishErr,
							strings.TrimSpace(c.queueName),
						)
					} else {
						log.Printf("[done-event-consumer] retry/finalize failed, moved to dead letter task=%s request_id=%s err=%v queue=%s",
							strings.TrimSpace(event.TaskID),
							strings.TrimSpace(event.RequestID),
							retryErr,
							strings.TrimSpace(c.queueName),
						)
					}
					_ = msg.Ack(false)
				}
				continue
			}
			_ = msg.Ack(false)
		}
	}
}

func (c *RabbitMQDoneEventConsumer) publishDeadLetter(ctx context.Context, msg amqp.Delivery, reason string) error {
	if c == nil || c.channel == nil {
		return errors.New("done event consumer is not initialized")
	}
	if strings.TrimSpace(c.dlqExchange) == "" || strings.TrimSpace(c.dlqRouting) == "" {
		return errors.New("done event dead letter route is not configured")
	}
	dlqBody, err := json.Marshal(doneEventDeadLetterMessage{
		SourceQueue:   strings.TrimSpace(c.queueName),
		Reason:        strings.TrimSpace(reason),
		RawBody:       string(msg.Body),
		ContentType:   strings.TrimSpace(msg.ContentType),
		MessageID:     strings.TrimSpace(msg.MessageId),
		CorrelationID: strings.TrimSpace(msg.CorrelationId),
		OccurredAt:    time.Now(),
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.channel.PublishWithContext(ctx, c.dlqExchange, c.dlqRouting, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         dlqBody,
		Timestamp:    time.Now(),
	})
}

func NewRabbitMQDoneEventDeadLetterConsumer(opts RabbitMQDoneEventDeadLetterConsumerOptions) (*RabbitMQDoneEventDeadLetterConsumer, error) {
	url := strings.TrimSpace(opts.URL)
	if url == "" {
		return nil, errors.New("done event dead letter rabbitmq url is empty")
	}
	queueName := strings.TrimSpace(opts.QueueName)
	if queueName == "" {
		queueName = DefaultDoneEventDLQQueue
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
	if err := ch.ExchangeDeclare(DefaultDoneEventDLQExchange, "direct", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.QueueBind(queueName, DefaultDoneEventDLQRoutingKey, DefaultDoneEventDLQExchange, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.Qos(1, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	consumerTag := strings.TrimSpace(opts.ConsumerTag)
	if consumerTag == "" {
		consumerTag = "ququchat.done_event.dlq.consumer." + uuid.NewString()
	}
	return &RabbitMQDoneEventDeadLetterConsumer{
		queueName:   queueName,
		consumerTag: consumerTag,
		conn:        conn,
		channel:     ch,
		db:          opts.DB,
	}, nil
}

func (c *RabbitMQDoneEventDeadLetterConsumer) Start(ctx context.Context) error {
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
			if err := c.persist(msg); err != nil {
				log.Printf("[done-event-dlq-consumer] persist failed, drop message err=%v message_id=%s correlation_id=%s queue=%s",
					err,
					strings.TrimSpace(msg.MessageId),
					strings.TrimSpace(msg.CorrelationId),
					strings.TrimSpace(c.queueName),
				)
				_ = msg.Ack(false)
				continue
			}
			_ = msg.Ack(false)
		}
	}
}

func (c *RabbitMQDoneEventDeadLetterConsumer) persist(msg amqp.Delivery) error {
	if c == nil || c.db == nil {
		return nil
	}
	payload := doneEventDeadLetterMessage{
		SourceQueue:   strings.TrimSpace(c.queueName),
		Reason:        "unknown",
		RawBody:       string(msg.Body),
		ContentType:   strings.TrimSpace(msg.ContentType),
		MessageID:     strings.TrimSpace(msg.MessageId),
		CorrelationID: strings.TrimSpace(msg.CorrelationId),
		OccurredAt:    time.Now(),
	}
	if err := json.Unmarshal(msg.Body, &payload); err != nil {
		payload.Reason = "invalid_done_event_dlq_message"
	}
	now := time.Now()
	row := models.TaskDeadLetter{
		ID:            uuid.NewString(),
		SourceQueue:   defaultDoneEventSourceQueue(payload.SourceQueue, c.queueName),
		Reason:        defaultDoneEventReason(payload.Reason),
		Status:        models.TaskDeadLetterStatusPending,
		RawBody:       payload.RawBody,
		ContentType:   strings.TrimSpace(payload.ContentType),
		MessageID:     strings.TrimSpace(payload.MessageID),
		CorrelationID: strings.TrimSpace(payload.CorrelationID),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	return c.db.Create(&row).Error
}

func defaultDoneEventSourceQueue(sourceQueue string, fallback string) string {
	trimmed := strings.TrimSpace(sourceQueue)
	if trimmed != "" {
		return trimmed
	}
	trimmedFallback := strings.TrimSpace(fallback)
	if trimmedFallback != "" {
		return trimmedFallback
	}
	return "done_event_dlq"
}

func defaultDoneEventReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
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

func normalizePrefetch(prefetch int) int {
	if prefetch <= 0 {
		return 1
	}
	return prefetch
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
