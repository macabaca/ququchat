package taskservice

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"

	"ququchat/internal/models"
	tasksvc "ququchat/internal/taskservice/task"
)

type TaskDeadLetterMessage struct {
	SourceQueue   string    `json:"source_queue"`
	Reason        string    `json:"reason"`
	RawBody       string    `json:"raw_body"`
	ContentType   string    `json:"content_type,omitempty"`
	MessageID     string    `json:"message_id,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
}

type RabbitMQTaskDeadLetterConsumer struct {
	queueName   string
	consumerTag string
	conn        *amqp.Connection
	channel     *amqp.Channel
	db          *gorm.DB
}

type RabbitMQTaskDeadLetterConsumerOptions struct {
	URL         string
	QueueName   string
	ConsumerTag string
	DB          *gorm.DB
}

func NewRabbitMQTaskDeadLetterConsumer(opts RabbitMQTaskDeadLetterConsumerOptions) (*RabbitMQTaskDeadLetterConsumer, error) {
	url := strings.TrimSpace(opts.URL)
	if url == "" {
		return nil, errors.New("task dead letter rabbitmq url is empty")
	}
	queueName := strings.TrimSpace(opts.QueueName)
	if queueName == "" {
		queueName = tasksvc.DefaultTaskDLQQueue
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
	if err := ch.ExchangeDeclare(tasksvc.DefaultTaskDLQExchange, "direct", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.QueueBind(queueName, tasksvc.DefaultTaskDLQRoutingKey, tasksvc.DefaultTaskDLQExchange, false, nil); err != nil {
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
		consumerTag = "ququchat.task.dlq.consumer." + uuid.NewString()
	}
	return &RabbitMQTaskDeadLetterConsumer{
		queueName:   queueName,
		consumerTag: consumerTag,
		conn:        conn,
		channel:     ch,
		db:          opts.DB,
	}, nil
}

func (c *RabbitMQTaskDeadLetterConsumer) Start(ctx context.Context) error {
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
				log.Printf("[task-dlq-consumer] persist failed err=%v", err)
				_ = msg.Nack(false, true)
				continue
			}
			_ = msg.Ack(false)
		}
	}
}

func (c *RabbitMQTaskDeadLetterConsumer) persist(msg amqp.Delivery) error {
	if c == nil || c.db == nil {
		return nil
	}
	payload := TaskDeadLetterMessage{
		RawBody:       string(msg.Body),
		ContentType:   strings.TrimSpace(msg.ContentType),
		MessageID:     strings.TrimSpace(msg.MessageId),
		CorrelationID: strings.TrimSpace(msg.CorrelationId),
		OccurredAt:    time.Now(),
	}
	if err := json.Unmarshal(msg.Body, &payload); err != nil {
		payload.SourceQueue = ""
		payload.Reason = "invalid_dlq_message"
	}
	row := models.TaskDeadLetter{
		ID:            uuid.NewString(),
		SourceQueue:   strings.TrimSpace(payload.SourceQueue),
		Reason:        defaultReason(payload.Reason),
		Status:        models.TaskDeadLetterStatusPending,
		RawBody:       payload.RawBody,
		ContentType:   strings.TrimSpace(payload.ContentType),
		MessageID:     strings.TrimSpace(payload.MessageID),
		CorrelationID: strings.TrimSpace(payload.CorrelationID),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	return c.db.Create(&row).Error
}

func defaultReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}
