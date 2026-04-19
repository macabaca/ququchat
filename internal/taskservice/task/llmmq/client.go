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

type ChatObservation struct {
	Text  string
	Usage TokenUsage
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
	observation, err := c.ObserveChat(ctx, prompt)
	if err != nil {
		return "", err
	}
	return observation.Text, nil
}

func (c *Client) ChatWithUsage(ctx context.Context, prompt string) (string, int, int, int, error) {
	observation, err := c.ObserveChat(ctx, prompt)
	if err != nil {
		return "", 0, 0, 0, err
	}
	promptTokens := observation.Usage.PromptTokens
	completionTokens := observation.Usage.CompletionTokens
	totalTokens := observation.Usage.TotalTokens
	if totalTokens <= 0 {
		totalTokens = promptTokens + completionTokens
	}
	return observation.Text, promptTokens, completionTokens, totalTokens, nil
}

func (c *Client) ObserveChat(ctx context.Context, prompt string) (ChatObservation, error) {
	if c == nil || c.conn == nil {
		return ChatObservation{}, errors.New("rabbitmq llm client is not initialized")
	}
	trimmedPrompt := strings.TrimSpace(prompt)
	if trimmedPrompt == "" {
		return ChatObservation{}, errors.New("llm prompt is empty")
	}
	ch, err := c.conn.Channel()
	if err != nil {
		return ChatObservation{}, err
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
		return ChatObservation{}, err
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
		return ChatObservation{}, err
	}
	requestID := uuid.NewString()
	startAt := time.Now()
	body, err := json.Marshal(RequestMessage{
		RequestID: requestID,
		Prompt:    trimmedPrompt,
	})
	if err != nil {
		return ChatObservation{}, err
	}
	if err := ch.PublishWithContext(ctx, "", c.requestQueue, false, false, amqp.Publishing{
		ContentType:   "application/json",
		Body:          body,
		ReplyTo:       replyQueue.Name,
		CorrelationId: requestID,
		Timestamp:     time.Now(),
	}); err != nil {
		log.Printf("[llm-mq-client] publish request failed request_id=%s queue=%s err=%v", requestID, c.requestQueue, err)
		return ChatObservation{}, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, c.responseTimeout)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				log.Printf("[llm-mq-client] response timeout request_id=%s queue=%s waited=%s", requestID, c.requestQueue, time.Since(startAt))
				return ChatObservation{}, errors.New("llm worker response timeout")
			}
			return ChatObservation{}, waitCtx.Err()
		case msg, ok := <-msgs:
			if !ok {
				log.Printf("[llm-mq-client] response channel closed request_id=%s queue=%s", requestID, c.requestQueue)
				return ChatObservation{}, errors.New("llm worker reply channel closed")
			}
			if strings.TrimSpace(msg.CorrelationId) != requestID {
				continue
			}
			var response ResponseMessage
			if err := json.Unmarshal(msg.Body, &response); err != nil {
				log.Printf("[llm-mq-client] response decode failed request_id=%s err=%v", requestID, err)
				return ChatObservation{}, err
			}
			if strings.TrimSpace(response.Error) != "" {
				log.Printf("[llm-mq-client] response contains error request_id=%s err=%s", requestID, strings.TrimSpace(response.Error))
				return ChatObservation{}, errors.New(strings.TrimSpace(response.Error))
			}
			text := strings.TrimSpace(response.Text)
			if text == "" {
				return ChatObservation{}, errors.New("llm worker response content is empty")
			}
			return ChatObservation{
				Text:  text,
				Usage: response.Usage,
			}, nil
		}
	}
}
