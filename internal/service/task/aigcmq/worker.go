package aigcmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"ququchat/internal/models"
)

type Provider interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error)
}

type AttachmentSaver interface {
	SaveAIGCImageFromURL(ctx context.Context, imageURL string) (*models.Attachment, error)
}

type WorkerOptions struct {
	URL             string
	RequestQueue    string
	Provider        Provider
	RateLimiter     *RateLimiter
	AttachmentSaver AttachmentSaver
}

type Worker struct {
	conn         *amqp.Connection
	channel      *amqp.Channel
	requestQueue string
	provider     Provider
	rateLimiter  *RateLimiter
	attachment   AttachmentSaver
	consumerTag  string
	stopOnce     sync.Once
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
		return nil, errors.New("aigc provider is nil")
	}
	if opts.AttachmentSaver == nil {
		return nil, errors.New("aigc attachment saver is nil")
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
	if _, err := ch.QueueDeclare(
		requestQueue,
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
	if err := ch.Qos(1, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	return &Worker{
		conn:         conn,
		channel:      ch,
		requestQueue: requestQueue,
		provider:     opts.Provider,
		rateLimiter:  opts.RateLimiter,
		attachment:   opts.AttachmentSaver,
	}, nil
}

func (w *Worker) Start(ctx context.Context) error {
	if w == nil || w.channel == nil || w.provider == nil {
		return errors.New("aigc worker is not initialized")
	}
	w.consumerTag = "aigc-worker-" + uuid.NewString()
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
	defer w.close()
	go func() {
		<-ctx.Done()
		_ = w.stopConsume()
	}()
	for {
		d, ok := <-deliveries
		if !ok {
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
		response.Error = "invalid aigc request message"
		_ = w.publishResponse(opCtx, d, response)
		_ = d.Ack(false)
		return
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(d.CorrelationId)
	}
	if requestID == "" {
		response.Error = "request_id is required"
		_ = w.publishResponse(opCtx, d, response)
		_ = d.Ack(false)
		return
	}
	response.RequestID = requestID

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		response.Error = "prompt is required"
		_ = w.publishResponse(opCtx, d, response)
		_ = d.Ack(false)
		return
	}
	imageSize := strings.TrimSpace(req.ImageSize)
	if imageSize == "" {
		imageSize = "1024x1024"
	}
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	numInferenceSteps := req.NumInferenceSteps
	if numInferenceSteps <= 0 {
		numInferenceSteps = 20
	}
	guidanceScale := req.GuidanceScale
	if guidanceScale <= 0 {
		guidanceScale = 7.5
	}
	generateReq := GenerateRequest{
		Prompt:            prompt,
		NegativePrompt:    strings.TrimSpace(req.NegativePrompt),
		ImageSize:         imageSize,
		BatchSize:         batchSize,
		NumInferenceSteps: numInferenceSteps,
		GuidanceScale:     guidanceScale,
	}

	if err := w.rateLimiter.Wait(opCtx, batchSize); err != nil {
		response.Error = err.Error()
		_ = w.publishResponse(opCtx, d, response)
		_ = d.Ack(false)
		return
	}
	result, err := w.provider.Generate(opCtx, generateReq)
	if err != nil {
		response.Error = err.Error()
	} else {
		response.Timings = result.Timings
		response.Seed = result.Seed
		imageList := make([]ImageData, 0, len(result.Images))
		for idx, image := range result.Images {
			rawURL := strings.TrimSpace(image.URL)
			if rawURL == "" {
				response.Error = fmt.Sprintf("image url is empty at index %d", idx)
				imageList = nil
				break
			}
			attachment, saveErr := w.attachment.SaveAIGCImageFromURL(opCtx, rawURL)
			if saveErr != nil {
				response.Error = saveErr.Error()
				imageList = nil
				break
			}
			imageList = append(imageList, ImageData{
				AttachmentID: attachment.ID,
			})
		}
		if response.Error == "" {
			response.Images = imageList
		}
	}
	_ = w.publishResponse(opCtx, d, response)
	_ = d.Ack(false)
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
		return nil
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
