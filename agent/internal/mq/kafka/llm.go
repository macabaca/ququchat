package kafka

import (
	"context"
	"encoding/json"
	"errors"

	kafkago "github.com/segmentio/kafka-go"

	"ququchat/agent/pkg/llmmsg"
)

type IngressConsumer struct {
	reader *kafkago.Reader
}

func NewIngressConsumer(brokers []string, topic string, groupID string) *IngressConsumer {
	return &IngressConsumer{
		reader: kafkago.NewReader(kafkago.ReaderConfig{
			Brokers: brokers,
			Topic:   topic,
			GroupID: groupID,
		}),
	}
}

func (c *IngressConsumer) Start(ctx context.Context, handle func(ctx context.Context, msg *llmmsg.Ingress) error) error {
	for {
		m, err := c.reader.FetchMessage(ctx)
		if err != nil {
			return err
		}
		var payload llmmsg.Ingress
		if err := json.Unmarshal(m.Value, &payload); err != nil {
			_ = c.reader.CommitMessages(ctx, m)
			continue
		}
		if err := handle(ctx, &payload); err != nil {
			return err
		}
		if err := c.reader.CommitMessages(ctx, m); err != nil {
			return err
		}
	}
}

func (c *IngressConsumer) Close() error {
	if c == nil || c.reader == nil {
		return nil
	}
	return c.reader.Close()
}

type ResultProducer struct {
	writer *kafkago.Writer
}

func NewResultProducer(brokers []string, topic string) *ResultProducer {
	return &ResultProducer{
		writer: &kafkago.Writer{
			Addr:     kafkago.TCP(brokers...),
			Topic:    topic,
			Balancer: &kafkago.LeastBytes{},
		},
	}
}

func (p *ResultProducer) Publish(ctx context.Context, msg *llmmsg.Result) error {
	if p == nil || p.writer == nil {
		return errors.New("result writer not initialized")
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return p.writer.WriteMessages(ctx, kafkago.Message{
		Key:   []byte(msg.TaskID),
		Value: b,
	})
}

func (p *ResultProducer) Close() error {
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
}
