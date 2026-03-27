package llmmq

import (
	"context"
	"encoding/json"
	"errors"
	"log"
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

func (c *Client) Chat(ctx context.Context, prompt string) (string, error) {
	if c == nil || c.conn == nil {
		return "", errors.New("rabbitmq llm client is not initialized")
	}
	trimmedPrompt := strings.TrimSpace(prompt)
	if trimmedPrompt == "" {
		return "", errors.New("llm prompt is empty")
	}
	ch, err := c.conn.Channel()
	if err != nil {
		return "", err
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
		return "", err
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
		return "", err
	}
	requestID := uuid.NewString()
	startAt := time.Now()
	body, err := json.Marshal(RequestMessage{
		RequestID: requestID,
		Prompt:    trimmedPrompt,
	})
	if err != nil {
		return "", err
	}
	if err := ch.PublishWithContext(ctx, "", c.requestQueue, false, false, amqp.Publishing{
		ContentType:   "application/json",
		Body:          body,
		ReplyTo:       replyQueue.Name,
		CorrelationId: requestID,
		Timestamp:     time.Now(),
	}); err != nil {
		log.Printf("[llm-mq-client] publish request failed request_id=%s queue=%s err=%v", requestID, c.requestQueue, err)
		return "", err
	}
	waitCtx, cancel := context.WithTimeout(ctx, c.responseTimeout)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				log.Printf("[llm-mq-client] response timeout request_id=%s queue=%s waited=%s", requestID, c.requestQueue, time.Since(startAt))
				return "", errors.New("llm worker response timeout")
			}
			return "", waitCtx.Err()
		case msg, ok := <-msgs:
			if !ok {
				log.Printf("[llm-mq-client] response channel closed request_id=%s queue=%s", requestID, c.requestQueue)
				return "", errors.New("llm worker reply channel closed")
			}
			if strings.TrimSpace(msg.CorrelationId) != requestID {
				continue
			}
			var response ResponseMessage
			if err := json.Unmarshal(msg.Body, &response); err != nil {
				log.Printf("[llm-mq-client] response decode failed request_id=%s err=%v", requestID, err)
				return "", err
			}
			if strings.TrimSpace(response.Error) != "" {
				log.Printf("[llm-mq-client] response contains error request_id=%s err=%s", requestID, strings.TrimSpace(response.Error))
				return "", errors.New(strings.TrimSpace(response.Error))
			}
			text := strings.TrimSpace(response.Text)
			if text == "" {
				return "", errors.New("llm worker response content is empty")
			}
			return text, nil
		}
	}
}
