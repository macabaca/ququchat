package embeddingmq

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

type ClientOptions struct {
	URL             string
	RequestQueue    string
	MaxLength       int
	MessageTTL      time.Duration
	ResponseTimeout time.Duration
}

type Client struct {
	conn            *amqp.Connection
	requestQueue    string
	responseTimeout time.Duration
}

func NewClient(opts ClientOptions) (*Client, error) {
	url := strings.TrimSpace(opts.URL)
	requestQueue := strings.TrimSpace(opts.RequestQueue)
	if url == "" {
		return nil, errors.New("rabbitmq url is empty")
	}
	if requestQueue == "" {
		return nil, errors.New("rabbitmq request queue is empty")
	}
	responseTimeout := opts.ResponseTimeout
	if responseTimeout <= 0 {
		responseTimeout = 60 * time.Second
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
	defer ch.Close()
	_, err = ch.QueueDeclare(
		requestQueue,
		true,
		false,
		false,
		false,
		resolveRequestQueueDeclareArgs(opts.MaxLength, opts.MessageTTL),
	)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &Client{
		conn:            conn,
		requestQueue:    requestQueue,
		responseTimeout: responseTimeout,
	}, nil
}

func (c *Client) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if c == nil || c.conn == nil {
		return nil, errors.New("rabbitmq embedding client is not initialized")
	}
	cleanedInputs := make([]string, 0, len(inputs))
	for _, input := range inputs {
		trimmed := strings.TrimSpace(input)
		if trimmed == "" {
			continue
		}
		cleanedInputs = append(cleanedInputs, trimmed)
	}
	if len(cleanedInputs) == 0 {
		return nil, errors.New("embedding inputs are empty")
	}
	ch, err := c.conn.Channel()
	if err != nil {
		return nil, err
	}
	defer ch.Close()
	replyQueue, err := ch.QueueDeclare(
		"",
		false,
		true,
		true,
		false,
		nil,
	)
	if err != nil {
		return nil, err
	}
	msgs, err := ch.Consume(
		replyQueue.Name,
		"",
		true,
		true,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, err
	}
	requestID := uuid.NewString()
	body, err := json.Marshal(RequestMessage{
		RequestID: requestID,
		Inputs:    cleanedInputs,
	})
	if err != nil {
		return nil, err
	}
	if err := ch.PublishWithContext(ctx, "", c.requestQueue, false, false, amqp.Publishing{
		ContentType:   "application/json",
		Body:          body,
		ReplyTo:       replyQueue.Name,
		CorrelationId: requestID,
		Timestamp:     time.Now(),
	}); err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, c.responseTimeout)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return nil, errors.New("embedding worker response timeout")
			}
			return nil, waitCtx.Err()
		case msg, ok := <-msgs:
			if !ok {
				return nil, errors.New("embedding worker reply channel closed")
			}
			if strings.TrimSpace(msg.CorrelationId) != requestID {
				continue
			}
			var response ResponseMessage
			if err := json.Unmarshal(msg.Body, &response); err != nil {
				return nil, err
			}
			if strings.TrimSpace(response.Error) != "" {
				return nil, errors.New(strings.TrimSpace(response.Error))
			}
			if len(response.Vectors) == 0 {
				return nil, errors.New("embedding worker response vectors are empty")
			}
			if len(response.Vectors) != len(cleanedInputs) {
				return nil, errors.New("embedding worker response vector count mismatch")
			}
			return response.Vectors, nil
		}
	}
}
