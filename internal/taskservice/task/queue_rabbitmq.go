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

type RabbitMQConsumer struct {
	conn        *amqp.Connection
	channel     *amqp.Channel
	url         string
	queueName   string
	exchange    string
	maxPriority int
	maxLength   int
	prefetch    int
	dlqExchange string
	dlqQueue    string
	dlqRouting  string
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

type rabbitMQDeadLetterMessage struct {
	SourceQueue   string    `json:"source_queue"`
	Reason        string    `json:"reason"`
	RawBody       string    `json:"raw_body"`
	ContentType   string    `json:"content_type,omitempty"`
	MessageID     string    `json:"message_id,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
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
	maxPriority := opts.MaxPriority
	if maxPriority <= 0 {
		maxPriority = 10
	}
	prefetch := opts.Prefetch
	if prefetch <= 0 {
		prefetch = 1
	}
	consumerTag := strings.TrimSpace(opts.ConsumerName)
	if consumerTag == "" {
		consumerTag = "task-queue-consumer"
	}
	conn, ch, deliveries, err := initRabbitMQConsumerConnection(url, queueName, exchangeName, maxPriority, opts.MaxLength, prefetch, consumerTag)
	if err != nil {
		return nil, err
	}
	return &RabbitMQConsumer{
		conn:        conn,
		channel:     ch,
		url:         url,
		queueName:   queueName,
		exchange:    exchangeName,
		maxPriority: maxPriority,
		maxLength:   opts.MaxLength,
		prefetch:    prefetch,
		dlqExchange: DefaultTaskDLQExchange,
		dlqQueue:    DefaultTaskDLQQueue,
		dlqRouting:  DefaultTaskDLQRoutingKey,
		consumerTag: consumerTag,
		deliveries:  deliveries,
	}, nil
}

func initRabbitMQConsumerConnection(url string, queueName string, exchangeName string, maxPriority int, maxLength int, prefetch int, consumerTag string) (*amqp.Connection, *amqp.Channel, <-chan amqp.Delivery, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, nil, nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, nil, err
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
		return nil, nil, nil, err
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
		return nil, nil, nil, err
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
		return nil, nil, nil, err
	}
	if err := ch.QueueBind(DefaultTaskDLQQueue, DefaultTaskDLQRoutingKey, DefaultTaskDLQExchange, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, nil, nil, err
	}
	if _, err := ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		resolveTaskQueueDeclareArgs(maxPriority, maxLength),
	); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, nil, nil, err
	}
	if err := ch.QueueBind(queueName, queueName, exchangeName, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, nil, nil, err
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, nil, nil, err
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
		return nil, nil, nil, err
	}
	return conn, ch, deliveries, nil
}

func MigrateRabbitMQQueue(opts RabbitMQQueueOptions) error {
	url := strings.TrimSpace(opts.URL)
	queueName := strings.TrimSpace(opts.QueueName)
	exchangeName := strings.TrimSpace(opts.ExchangeName)
	if url == "" {
		return errors.New("rabbitmq url is empty")
	}
	if queueName == "" {
		return errors.New("rabbitmq queue name is empty")
	}
	if exchangeName == "" {
		exchangeName = queueName + ".exchange"
	}
	maxPriority := opts.MaxPriority
	if maxPriority <= 0 {
		maxPriority = 10
	}
	conn, err := amqp.Dial(url)
	if err != nil {
		return err
	}
	defer conn.Close()
	declareCh, err := conn.Channel()
	if err != nil {
		return err
	}
	if err := ensureTaskQueueTopology(declareCh, exchangeName, queueName, maxPriority, opts.MaxLength); err == nil {
		_ = declareCh.Close()
		return nil
	} else if !isQueueDeclareIncompatibleError(err) {
		_ = declareCh.Close()
		return err
	}
	_ = declareCh.Close()

	deleteCh, err := conn.Channel()
	if err != nil {
		return err
	}
	_, deleteErr := deleteCh.QueueDelete(queueName, false, false, false)
	_ = deleteCh.Close()
	if deleteErr != nil && !isQueueNotFoundError(deleteErr) {
		return deleteErr
	}

	rebuildCh, err := conn.Channel()
	if err != nil {
		return err
	}
	defer rebuildCh.Close()
	if err := ensureTaskQueueTopology(rebuildCh, exchangeName, queueName, maxPriority, opts.MaxLength); err != nil {
		return err
	}
	return nil
}

func ensureTaskQueueTopology(ch *amqp.Channel, exchangeName string, queueName string, maxPriority int, maxLength int) error {
	if ch == nil {
		return errors.New("rabbitmq channel is nil")
	}
	if err := ch.ExchangeDeclare(exchangeName, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.ExchangeDeclare(DefaultTaskDLQExchange, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(DefaultTaskDLQQueue, true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.QueueBind(DefaultTaskDLQQueue, DefaultTaskDLQRoutingKey, DefaultTaskDLQExchange, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, resolveTaskQueueDeclareArgs(maxPriority, maxLength)); err != nil {
		return err
	}
	if err := ch.QueueBind(queueName, queueName, exchangeName, false, nil); err != nil {
		return err
	}
	return nil
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
	if q == nil {
		return nil, errors.New("rabbitmq queue is not initialized")
	}
	for {
		q.mu.Lock()
		deliveries := q.deliveries
		q.mu.Unlock()
		if deliveries == nil {
			if err := q.reconnectConsumer(); err != nil {
				return nil, err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				if err := q.reconnectConsumer(); err != nil {
					return nil, err
				}
				continue
			}
			var payload rabbitMQTaskMessage
			if err := json.Unmarshal(d.Body, &payload); err != nil {
				_ = q.publishDeadLetter(d, "invalid_task_queue_message")
				_ = d.Ack(false)
				return nil, errors.New("invalid task queue message")
			}
			taskID := strings.TrimSpace(payload.TaskID)
			if taskID == "" {
				_ = q.publishDeadLetter(d, "task_id_required")
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
}

func (q *RabbitMQConsumer) reconnectConsumer() error {
	if q == nil {
		return errors.New("rabbitmq queue is not initialized")
	}
	conn, ch, deliveries, err := initRabbitMQConsumerConnection(
		q.url,
		q.queueName,
		q.exchange,
		q.maxPriority,
		q.maxLength,
		q.prefetch,
		q.consumerTag,
	)
	if err != nil {
		return err
	}
	q.mu.Lock()
	oldChannel := q.channel
	oldConn := q.conn
	q.conn = conn
	q.channel = ch
	q.deliveries = deliveries
	q.mu.Unlock()
	if oldChannel != nil {
		_ = oldChannel.Close()
	}
	if oldConn != nil {
		_ = oldConn.Close()
	}
	return nil
}

func (q *RabbitMQConsumer) publishDeadLetter(d amqp.Delivery, reason string) error {
	if q == nil || q.channel == nil {
		return errors.New("rabbitmq queue is not initialized")
	}
	if strings.TrimSpace(q.dlqExchange) == "" || strings.TrimSpace(q.dlqRouting) == "" {
		return errors.New("rabbitmq dead letter route is not configured")
	}
	msg := rabbitMQDeadLetterMessage{
		SourceQueue:   strings.TrimSpace(q.queueName),
		Reason:        strings.TrimSpace(reason),
		RawBody:       string(d.Body),
		ContentType:   strings.TrimSpace(d.ContentType),
		MessageID:     strings.TrimSpace(d.MessageId),
		CorrelationID: strings.TrimSpace(d.CorrelationId),
		OccurredAt:    time.Now(),
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.channel.PublishWithContext(context.Background(), q.dlqExchange, q.dlqRouting, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
		Timestamp:    time.Now(),
	})
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

func isQueueNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not_found")
}

func isQueueDeclareIncompatibleError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "precondition_failed") && strings.Contains(message, "inequivalent arg")
}
