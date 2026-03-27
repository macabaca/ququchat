package tasksvc

import (
	"context"
	"errors"
	"strings"
	"time"

	"ququchat/internal/taskservice/task/embeddingmq"
)

type MQEmbeddingProviderOptions struct {
	URL             string
	RequestQueue    string
	MaxLength       int
	MessageTTL      time.Duration
	ResponseTimeout time.Duration
}

type MQEmbeddingProvider struct {
	client *embeddingmq.Client
}

type DirectEmbeddingProvider struct {
	embed func(ctx context.Context, inputs []string) ([][]float32, error)
}

func NewMQEmbeddingProvider(opts MQEmbeddingProviderOptions) (*MQEmbeddingProvider, error) {
	client, err := embeddingmq.NewClient(embeddingmq.ClientOptions{
		URL:             opts.URL,
		RequestQueue:    opts.RequestQueue,
		MaxLength:       opts.MaxLength,
		MessageTTL:      opts.MessageTTL,
		ResponseTimeout: opts.ResponseTimeout,
	})
	if err != nil {
		return nil, err
	}
	return &MQEmbeddingProvider{client: client}, nil
}

func (p *MQEmbeddingProvider) EmbedRawSegments(ctx context.Context, segments []RAGSegment) ([][]float32, error) {
	inputs := collectRawSegmentInputs(segments)
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}
	return p.client.Embed(ctx, inputs)
}

func (p *MQEmbeddingProvider) EmbedTexts(ctx context.Context, inputs []string) ([][]float32, error) {
	if p == nil || p.client == nil {
		return nil, errors.New("mq embedding provider is not initialized")
	}
	cleaned := collectTextInputs(inputs)
	if len(cleaned) == 0 {
		return [][]float32{}, nil
	}
	return p.client.Embed(ctx, cleaned)
}

func NewDirectEmbeddingProvider(embed func(ctx context.Context, inputs []string) ([][]float32, error)) *DirectEmbeddingProvider {
	return &DirectEmbeddingProvider{embed: embed}
}

func (p *DirectEmbeddingProvider) EmbedRawSegments(ctx context.Context, segments []RAGSegment) ([][]float32, error) {
	if p == nil || p.embed == nil {
		return nil, errors.New("direct embedding provider is not initialized")
	}
	inputs := collectRawSegmentInputs(segments)
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}
	return p.embed(ctx, inputs)
}

func (p *DirectEmbeddingProvider) EmbedTexts(ctx context.Context, inputs []string) ([][]float32, error) {
	if p == nil || p.embed == nil {
		return nil, errors.New("direct embedding provider is not initialized")
	}
	cleaned := collectTextInputs(inputs)
	if len(cleaned) == 0 {
		return [][]float32{}, nil
	}
	return p.embed(ctx, cleaned)
}

func collectRawSegmentInputs(segments []RAGSegment) []string {
	inputs := make([]string, 0, len(segments))
	for _, seg := range segments {
		text := strings.TrimSpace(seg.RawText)
		if text == "" {
			continue
		}
		inputs = append(inputs, text)
	}
	if len(inputs) == 0 {
		return nil
	}
	return inputs
}

func collectTextInputs(inputs []string) []string {
	cleaned := make([]string, 0, len(inputs))
	for _, input := range inputs {
		text := strings.TrimSpace(input)
		if text == "" {
			continue
		}
		cleaned = append(cleaned, text)
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}
