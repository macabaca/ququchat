package scheduler

import (
	"ququchat/agent/internal/core/task"
	"ququchat/agent/internal/queue"
)

type Dispatcher struct {
	q queue.Queue
}

func NewDispatcher(q queue.Queue) *Dispatcher {
	return &Dispatcher{q: q}
}

func (d *Dispatcher) Dispatch(t *task.Task) error {
	return d.q.Push(t)
}
