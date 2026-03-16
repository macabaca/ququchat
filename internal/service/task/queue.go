package tasksvc

import (
	"context"
	"errors"
)

var ErrQueueFull = errors.New("queue full")

type Queue interface {
	Push(t *Task) error
	Pop(ctx context.Context) (*Task, error)
}

type MemoryPriorityQueue struct {
	highCh   chan *Task
	normalCh chan *Task
	lowCh    chan *Task
}

func NewMemoryPriorityQueue(highCap, normalCap, lowCap int) *MemoryPriorityQueue {
	return &MemoryPriorityQueue{
		highCh:   make(chan *Task, highCap),
		normalCh: make(chan *Task, normalCap),
		lowCh:    make(chan *Task, lowCap),
	}
}

func (q *MemoryPriorityQueue) Push(t *Task) error {
	switch t.Priority {
	case PriorityHigh:
		select {
		case q.highCh <- t:
			return nil
		default:
			return ErrQueueFull
		}
	case PriorityLow:
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

func (q *MemoryPriorityQueue) Pop(ctx context.Context) (*Task, error) {
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
