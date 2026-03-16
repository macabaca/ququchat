package queue

import (
	"context"

	"ququchat/agent/internal/core/task"
)

type MemoryPriorityQueue struct {
	highCh   chan *task.Task
	normalCh chan *task.Task
	lowCh    chan *task.Task
}

func NewMemoryPriorityQueue(highCap, normalCap, lowCap int) *MemoryPriorityQueue {
	return &MemoryPriorityQueue{
		highCh:   make(chan *task.Task, highCap),
		normalCh: make(chan *task.Task, normalCap),
		lowCh:    make(chan *task.Task, lowCap),
	}
}

func (q *MemoryPriorityQueue) Push(t *task.Task) error {
	switch t.Priority {
	case task.PriorityHigh:
		select {
		case q.highCh <- t:
			return nil
		default:
			return ErrQueueFull
		}
	case task.PriorityLow:
		select {
		case q.lowCh <- t:
			return nil
		default:
			return ErrQueueFull
		}
	default:
		select {
		case q.normalCh <- t:
			return nil
		default:
			return ErrQueueFull
		}
	}
}

func (q *MemoryPriorityQueue) Pop(ctx context.Context) (*task.Task, error) {
	for {
		select {
		case t := <-q.highCh:
			return t, nil
		default:
		}
		select {
		case t := <-q.normalCh:
			return t, nil
		default:
		}
		select {
		case t := <-q.lowCh:
			return t, nil
		default:
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case t := <-q.highCh:
			return t, nil
		case t := <-q.normalCh:
			return t, nil
		case t := <-q.lowCh:
			return t, nil
		}
	}
}
