package aigcmq

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
		responseTimeout = 120 * time.Second
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

func (c *Client) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	if c == nil || c.conn == nil {
		return GenerateResponse{}, errors.New("rabbitmq aigc client is not initialized")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return GenerateResponse{}, errors.New("aigc prompt is empty")
	}
	request := GenerateRequest{
		Prompt:            prompt,
		NegativePrompt:    strings.TrimSpace(req.NegativePrompt),
		ImageSize:         strings.TrimSpace(req.ImageSize),
		BatchSize:         req.BatchSize,
		NumInferenceSteps: req.NumInferenceSteps,
		GuidanceScale:     req.GuidanceScale,
	}
	if request.ImageSize == "" {
		request.ImageSize = "1024x1024"
	}
	if request.BatchSize <= 0 {
		request.BatchSize = 1
	}
	if request.NumInferenceSteps <= 0 {
		request.NumInferenceSteps = 20
	}
	if request.GuidanceScale <= 0 {
		request.GuidanceScale = 7.5
	}

	ch, err := c.conn.Channel()
	if err != nil {
		return GenerateResponse{}, err
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
		return GenerateResponse{}, err
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
		return GenerateResponse{}, err
	}
	requestID := uuid.NewString()
	body, err := json.Marshal(RequestMessage{
		RequestID:       requestID,
		GenerateRequest: request,
	})
	if err != nil {
		return GenerateResponse{}, err
	}
	if err := ch.PublishWithContext(ctx, "", c.requestQueue, false, false, amqp.Publishing{
		ContentType:   "application/json",
		Body:          body,
		ReplyTo:       replyQueue.Name,
		CorrelationId: requestID,
		Timestamp:     time.Now(),
	}); err != nil {
		return GenerateResponse{}, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, c.responseTimeout)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return GenerateResponse{}, errors.New("aigc worker response timeout")
			}
			return GenerateResponse{}, waitCtx.Err()
		case msg, ok := <-msgs:
			if !ok {
				return GenerateResponse{}, errors.New("aigc worker reply channel closed")
			}
			if strings.TrimSpace(msg.CorrelationId) != requestID {
				continue
			}
			var response ResponseMessage
			if err := json.Unmarshal(msg.Body, &response); err != nil {
				return GenerateResponse{}, err
			}
			if strings.TrimSpace(response.Error) != "" {
				return GenerateResponse{}, errors.New(strings.TrimSpace(response.Error))
			}
			if len(response.Images) == 0 {
				return GenerateResponse{}, errors.New("aigc worker response has no images")
			}
			return GenerateResponse{
				Images:  response.Images,
				Timings: response.Timings,
				Seed:    response.Seed,
			}, nil
		}
	}
}
