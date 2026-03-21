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

type RabbitMQConsumer struct {
	conn        *amqp.Connection
	channel     *amqp.Channel
	queueName   string
	consumerTag string
	deliveries  <-chan amqp.Delivery
	mu          sync.Mutex
	closeOnce   sync.Once
}

type rabbitMQTaskMessage struct {
	TaskID   string   `json:"task_id"`
	Priority Priority `json:"priority,omitempty"`
	Attempt  int      `json:"attempt,omitempty"`
}

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
	if _, err := ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-max-priority": int32(maxPriority),
		},
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

func NewRabbitMQConsumer(opts RabbitMQQueueOptions) (*RabbitMQConsumer, error) {
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
	if _, err := ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-max-priority": int32(maxPriority),
		},
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
	prefetch := opts.Prefetch
	if prefetch <= 0 {
		prefetch = 1
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	consumerTag := strings.TrimSpace(opts.ConsumerName)
	if consumerTag == "" {
		consumerTag = "task-queue-consumer"
	}
	deliveries, err := ch.Consume(
		queueName,
		consumerTag,
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	return &RabbitMQConsumer{
		conn:        conn,
		channel:     ch,
		queueName:   queueName,
		consumerTag: consumerTag,
		deliveries:  deliveries,
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

func (q *RabbitMQConsumer) Pop(ctx context.Context) (QueueMessage, error) {
	if q == nil || q.deliveries == nil {
		return nil, errors.New("rabbitmq queue is not initialized")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case d, ok := <-q.deliveries:
		if !ok {
			return nil, errors.New("rabbitmq delivery channel is closed")
		}
		var payload rabbitMQTaskMessage
		if err := json.Unmarshal(d.Body, &payload); err != nil {
			_ = d.Ack(false)
			return nil, errors.New("invalid task queue message")
		}
		taskID := strings.TrimSpace(payload.TaskID)
		if taskID == "" {
			_ = d.Ack(false)
			return nil, errors.New("task_id is required")
		}
		return &rabbitQueueMessage{
			task: &Task{
				ID:       taskID,
				Priority: payload.Priority,
			},
			delivery: d,
			queue:    q,
		}, nil
	}
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

func (q *RabbitMQConsumer) Close() error {
	if q == nil {
		return nil
	}
	var firstErr error
	q.closeOnce.Do(func() {
		if q.channel != nil && strings.TrimSpace(q.consumerTag) != "" {
			if err := q.channel.Cancel(q.consumerTag, false); err != nil {
				firstErr = err
			}
		}
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

type rabbitQueueMessage struct {
	task     *Task
	delivery amqp.Delivery
	queue    *RabbitMQConsumer
}

func (m *rabbitQueueMessage) Task() *Task {
	if m == nil || m.task == nil {
		return nil
	}
	return m.task.Clone()
}

func (m *rabbitQueueMessage) Ack() error {
	if m == nil {
		return nil
	}
	if m.queue != nil {
		m.queue.mu.Lock()
		defer m.queue.mu.Unlock()
	}
	return m.delivery.Ack(false)
}

func (m *rabbitQueueMessage) Nack(requeue bool) error {
	if m == nil {
		return nil
	}
	if m.queue != nil {
		m.queue.mu.Lock()
		defer m.queue.mu.Unlock()
	}
	return m.delivery.Nack(false, requeue)
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
