package tasksvc

import (
	"context"
	"fmt"
)

type Queue interface {
	ProducerQueue
	ConsumerQueue
}

type ProducerQueue interface {
	Push(t *Task) error
}

type ConsumerQueue interface {
	Pop(ctx context.Context) (QueueMessage, error)
}

type QueueMessage interface {
	Task() *Task
	Ack() error
	Nack(requeue bool) error
}

type unavailableQueue struct {
	reason error
}

func (q unavailableQueue) Push(t *Task) error {
	if q.reason != nil {
		return q.reason
	}
	return fmt.Errorf("queue is unavailable")
}

func (q unavailableQueue) Pop(ctx context.Context) (QueueMessage, error) {
	if q.reason != nil {
		return nil, q.reason
	}
	return nil, fmt.Errorf("queue is unavailable")
}
