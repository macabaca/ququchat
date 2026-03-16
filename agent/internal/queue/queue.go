package queue

import (
	"context"
	"errors"

	"ququchat/agent/internal/core/task"
)

var ErrQueueFull = errors.New("queue full")

type Queue interface {
	Push(t *task.Task) error
	Pop(ctx context.Context) (*task.Task, error)
}
