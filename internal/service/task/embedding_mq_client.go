package tasksvc

import (
	"context"
	"strings"
	"time"

	"ququchat/internal/service/task/embeddingmq"
)

type MQEmbeddingProviderOptions struct {
	URL             string
	RequestQueue    string
	ResponseTimeout time.Duration
}

type MQEmbeddingProvider struct {
	client *embeddingmq.Client
}

func NewMQEmbeddingProvider(opts MQEmbeddingProviderOptions) (*MQEmbeddingProvider, error) {
	client, err := embeddingmq.NewClient(embeddingmq.ClientOptions{
		URL:             opts.URL,
		RequestQueue:    opts.RequestQueue,
		ResponseTimeout: opts.ResponseTimeout,
	})
	if err != nil {
		return nil, err
	}
	return &MQEmbeddingProvider{client: client}, nil
}

func (p *MQEmbeddingProvider) EmbedRawSegments(ctx context.Context, segments []RAGSegment) ([][]float32, error) {
	inputs := make([]string, 0, len(segments))
	for _, seg := range segments {
		text := strings.TrimSpace(seg.RawText)
		if text == "" {
			continue
		}
		inputs = append(inputs, text)
	}
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}
	return p.client.Embed(ctx, inputs)
}
