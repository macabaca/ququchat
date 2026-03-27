package llmmq

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Provider interface {
	Chat(ctx context.Context, prompt string) (string, error)
}

type WorkerOptions struct {
	URL          string
	RequestQueue string
	MaxLength    int
	MessageTTL   time.Duration
	Prefetch     int
	Provider     Provider
	RateLimiter  *RateLimiter
}

type Worker struct {
	conn         *amqp.Connection
	channel      *amqp.Channel
	requestQueue string
	dlqExchange  string
	dlqQueue     string
	dlqRouting   string
	provider     Provider
	rateLimiter  *RateLimiter
	consumerTag  string
	stopOnce     sync.Once
}

const (
	defaultTaskDLQExchange   = "ququchat.task.dlq.exchange"
	defaultTaskDLQQueue      = "ququchat.task.dlq"
	defaultTaskDLQRoutingKey = "task.dead"
)

type taskDeadLetterMessage struct {
	SourceQueue   string    `json:"source_queue"`
	Reason        string    `json:"reason"`
	RawBody       string    `json:"raw_body"`
	ContentType   string    `json:"content_type,omitempty"`
	MessageID     string    `json:"message_id,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
}

func NewWorker(opts WorkerOptions) (*Worker, error) {
	url := strings.TrimSpace(opts.URL)
	requestQueue := strings.TrimSpace(opts.RequestQueue)
	if url == "" {
		return nil, errors.New("rabbitmq url is empty")
	}
	if requestQueue == "" {
		return nil, errors.New("rabbitmq request queue is empty")
	}
	if opts.Provider == nil {
		return nil, errors.New("llm provider is nil")
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
	prefetch := opts.Prefetch
	if prefetch <= 0 {
		prefetch = 1
	}
	if _, err := ch.QueueDeclare(
		requestQueue,
		true,
		false,
		false,
		false,
		resolveRequestQueueDeclareArgs(opts.MaxLength, opts.MessageTTL),
	); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.ExchangeDeclare(defaultTaskDLQExchange, "direct", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if _, err := ch.QueueDeclare(defaultTaskDLQQueue, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.QueueBind(defaultTaskDLQQueue, defaultTaskDLQRoutingKey, defaultTaskDLQExchange, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	return &Worker{
		conn:         conn,
		channel:      ch,
		requestQueue: requestQueue,
		dlqExchange:  defaultTaskDLQExchange,
		dlqQueue:     defaultTaskDLQQueue,
		dlqRouting:   defaultTaskDLQRoutingKey,
		provider:     opts.Provider,
		rateLimiter:  opts.RateLimiter,
	}, nil
}

func resolveRequestQueueDeclareArgs(maxLength int, messageTTL time.Duration) amqp.Table {
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

func (w *Worker) Start(ctx context.Context) error {
	if w == nil || w.channel == nil || w.provider == nil {
		return errors.New("llm worker is not initialized")
	}
	w.consumerTag = "llm-worker-" + uuid.NewString()
	deliveries, err := w.channel.Consume(
		w.requestQueue,
		w.consumerTag,
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}
	log.Printf("[llm-worker] consumer started queue=%s tag=%s", strings.TrimSpace(w.requestQueue), strings.TrimSpace(w.consumerTag))
	defer w.close()
	go func() {
		<-ctx.Done()
		_ = w.stopConsume()
	}()
	for {
		d, ok := <-deliveries
		if !ok {
			log.Printf("[llm-worker] delivery channel closed queue=%s tag=%s", strings.TrimSpace(w.requestQueue), strings.TrimSpace(w.consumerTag))
			return nil
		}
		w.handleDelivery(d)
	}
}

func (w *Worker) handleDelivery(d amqp.Delivery) {
	opCtx := context.Background()
	response := ResponseMessage{}
	var req RequestMessage
	if err := json.Unmarshal(d.Body, &req); err != nil {
		log.Printf("[llm-worker] invalid request body queue=%s message_id=%s correlation_id=%s err=%v",
			strings.TrimSpace(w.requestQueue),
			strings.TrimSpace(d.MessageId),
			strings.TrimSpace(d.CorrelationId),
			err,
		)
		response.Error = "invalid llm request message"
		w.respondOrDeadLetter(opCtx, d, response, "invalid_llm_request_message")
		return
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(d.CorrelationId)
	}
	if requestID == "" {
		response.Error = "request_id is required"
		w.respondOrDeadLetter(opCtx, d, response, "llm_request_id_required")
		return
	}
	response.RequestID = requestID
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		response.Error = "prompt is required"
		w.respondOrDeadLetter(opCtx, d, response, "llm_prompt_required")
		return
	}
	if err := w.rateLimiter.Wait(opCtx, prompt); err != nil {
		log.Printf("[llm-worker] rate limiter failed request_id=%s queue=%s err=%v", requestID, strings.TrimSpace(w.requestQueue), err)
		response.Error = err.Error()
		w.respondOrDeadLetter(opCtx, d, response, "llm_rate_limit_wait_failed")
		return
	}
	text, err := w.provider.Chat(opCtx, prompt)
	if err != nil {
		log.Printf("[llm-worker] provider chat failed request_id=%s err=%v", requestID, err)
		response.Error = err.Error()
	} else {
		response.Text = strings.TrimSpace(text)
	}
	w.respondOrDeadLetter(opCtx, d, response, "llm_response_delivery_failed")
}

func (w *Worker) stopConsume() error {
	if w == nil || w.channel == nil {
		return nil
	}
	var stopErr error
	w.stopOnce.Do(func() {
		tag := strings.TrimSpace(w.consumerTag)
		if tag == "" {
			return
		}
		stopErr = w.channel.Cancel(tag, false)
	})
	return stopErr
}

func (w *Worker) close() {
	if w == nil {
		return
	}
	if w.channel != nil {
		_ = w.channel.Close()
	}
	if w.conn != nil {
		_ = w.conn.Close()
	}
}

func (w *Worker) publishResponse(ctx context.Context, d amqp.Delivery, response ResponseMessage) error {
	replyTo := strings.TrimSpace(d.ReplyTo)
	if replyTo == "" {
		return errors.New("llm reply_to is empty")
	}
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return w.channel.PublishWithContext(ctx, "", replyTo, false, false, amqp.Publishing{
		ContentType:   "application/json",
		Body:          body,
		CorrelationId: d.CorrelationId,
	})
}

func (w *Worker) respondOrDeadLetter(ctx context.Context, d amqp.Delivery, response ResponseMessage, reason string) {
	if err := w.publishResponse(ctx, d, response); err != nil {
		dlqReason := strings.TrimSpace(reason)
		if dlqReason == "" {
			dlqReason = "llm_response_publish_failed"
		}
		if dlqErr := w.publishDeadLetter(ctx, d, dlqReason); dlqErr != nil {
			log.Printf("[llm-worker] publish dead letter failed, drop message, reason=%s err=%v dlq_err=%v queue=%s message_id=%s correlation_id=%s",
				dlqReason,
				err,
				dlqErr,
				strings.TrimSpace(w.requestQueue),
				strings.TrimSpace(d.MessageId),
				strings.TrimSpace(d.CorrelationId),
			)
		}
		log.Printf("[llm-worker] response publish failed queue=%s message_id=%s correlation_id=%s reason=%s err=%v",
			strings.TrimSpace(w.requestQueue),
			strings.TrimSpace(d.MessageId),
			strings.TrimSpace(d.CorrelationId),
			dlqReason,
			err,
		)
	}
	_ = d.Ack(false)
}

func (w *Worker) publishDeadLetter(ctx context.Context, d amqp.Delivery, reason string) error {
	if w == nil || w.channel == nil {
		return errors.New("llm worker is not initialized")
	}
	if strings.TrimSpace(w.dlqExchange) == "" || strings.TrimSpace(w.dlqRouting) == "" {
		return errors.New("llm dead letter route is not configured")
	}
	body, err := json.Marshal(taskDeadLetterMessage{
		SourceQueue:   strings.TrimSpace(w.requestQueue),
		Reason:        strings.TrimSpace(reason),
		RawBody:       string(d.Body),
		ContentType:   strings.TrimSpace(d.ContentType),
		MessageID:     strings.TrimSpace(d.MessageId),
		CorrelationID: strings.TrimSpace(d.CorrelationId),
		OccurredAt:    time.Now(),
	})
	if err != nil {
		return err
	}
	return w.channel.PublishWithContext(ctx, w.dlqExchange, w.dlqRouting, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
		Timestamp:    time.Now(),
	})
}
