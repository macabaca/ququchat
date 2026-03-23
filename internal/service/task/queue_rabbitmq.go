package tasksvc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQQueueOptions struct {
	URL          string
	QueueName    string
	ExchangeName string
	MaxPriority  int
	MaxLength    int
	Prefetch     int
	ConsumerName string
}

type RabbitMQProducer struct {
	conn        *amqp.Connection
	channel     *amqp.Channel
	queueName   string
	exchange    string
	maxPriority uint8
	mu          sync.Mutex
	closeOnce   sync.Once
}

type rabbitMQTaskMessage struct {
	TaskID   string   `json:"task_id"`
	Priority Priority `json:"priority,omitempty"`
	Attempt  int      `json:"attempt,omitempty"`
}

const (
	DefaultTaskDLQExchange   = "ququchat.task.dlq.exchange"
	DefaultTaskDLQQueue      = "ququchat.task.dlq"
	DefaultTaskDLQRoutingKey = "task.dead"
)

func NewRabbitMQProducer(opts RabbitMQQueueOptions) (*RabbitMQProducer, error) {
	url := strings.TrimSpace(opts.URL)
	queueName := strings.TrimSpace(opts.QueueName)
	exchangeName := strings.TrimSpace(opts.ExchangeName)
	if url == "" {
		return nil, errors.New("rabbitmq url is empty")
	}
	if queueName == "" {
		return nil, errors.New("rabbitmq queue name is empty")
	}
	if exchangeName == "" {
		exchangeName = queueName + ".exchange"
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
	maxPriority := opts.MaxPriority
	if maxPriority <= 0 {
		maxPriority = 10
	}
	if err := ch.ExchangeDeclare(
		exchangeName,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.ExchangeDeclare(
		DefaultTaskDLQExchange,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if _, err := ch.QueueDeclare(
		DefaultTaskDLQQueue,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.QueueBind(DefaultTaskDLQQueue, DefaultTaskDLQRoutingKey, DefaultTaskDLQExchange, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if _, err := ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		resolveTaskQueueDeclareArgs(maxPriority, opts.MaxLength),
	); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.QueueBind(queueName, queueName, exchangeName, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	return &RabbitMQProducer{
		conn:        conn,
		channel:     ch,
		queueName:   queueName,
		exchange:    exchangeName,
		maxPriority: uint8(maxPriority),
	}, nil
}

func (q *RabbitMQProducer) Push(t *Task) error {
	if q == nil || q.channel == nil {
		return errors.New("rabbitmq queue is not initialized")
	}
	if t == nil {
		return errors.New("task is nil")
	}
	taskID := strings.TrimSpace(t.ID)
	if taskID == "" {
		return errors.New("task id is empty")
	}
	msg := rabbitMQTaskMessage{
		TaskID:   taskID,
		Priority: t.Priority,
		Attempt:  1,
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	q.mu.Lock()
	err = q.channel.PublishWithContext(context.Background(), q.exchange, q.queueName, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
		Timestamp:    time.Now(),
		Priority:     q.toAMQPPriority(t.Priority),
	})
	q.mu.Unlock()
	return err
}

func (q *RabbitMQProducer) Close() error {
	if q == nil {
		return nil
	}
	var firstErr error
	q.closeOnce.Do(func() {
		if q.channel != nil {
			if err := q.channel.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if q.conn != nil {
			if err := q.conn.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	})
	return firstErr
}

func (q *RabbitMQProducer) toAMQPPriority(p Priority) uint8 {
	if q == nil || q.maxPriority == 0 {
		return 0
	}
	switch p {
	case PriorityHigh:
		return q.maxPriority
	case PriorityLow:
		if q.maxPriority <= 2 {
			return 1
		}
		return 1
	default:
		if q.maxPriority <= 1 {
			return 1
		}
		return q.maxPriority / 2
	}
}

func resolveTaskQueueDeclareArgs(maxPriority int, maxLength int) amqp.Table {
	args := amqp.Table{
		"x-max-priority":            int32(maxPriority),
		"x-overflow":                "reject-publish-dlx",
		"x-dead-letter-exchange":    DefaultTaskDLQExchange,
		"x-dead-letter-routing-key": DefaultTaskDLQRoutingKey,
	}
	if maxLength > 0 {
		args["x-max-length"] = int32(maxLength)
	}
	return args
}
