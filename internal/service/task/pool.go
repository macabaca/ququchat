package tasksvc

import (
	"context"
	"errors"
	"log"
	"sync"
)

type Pool struct {
	queue      Queue
	store      Store
	exec       Executor
	workerSize int
	onFinish   func(ctx context.Context, doneTask *Task)
}

func NewPool(queue Queue, store Store, exec Executor, workerSize int, onFinish func(ctx context.Context, doneTask *Task)) *Pool {
	if workerSize <= 0 {
		workerSize = 1
	}
	return &Pool{
		queue:      queue,
		store:      store,
		exec:       exec,
		workerSize: workerSize,
		onFinish:   onFinish,
	}
}

func (p *Pool) Start(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(p.workerSize)
	for i := range p.workerSize {
		workerID := i + 1
		go func() {
			defer wg.Done()
			p.runWorker(ctx, workerID)
		}()
	}
	<-ctx.Done()
	wg.Wait()
}

func (p *Pool) runWorker(ctx context.Context, workerID int) {
	for {
		t, err := p.queue.Pop(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("[task-worker-%d] pop failed: %v", workerID, err)
			continue
		}
		if _, err := p.store.MarkRunning(t.ID); err != nil {
			log.Printf("[task-worker-%d] mark running failed task=%s err=%v", workerID, t.ID, err)
			continue
		}
		result, execErr := p.exec.Execute(ctx, t)
		if execErr != nil {
			doneTask, err := p.store.MarkFailed(t.ID, execErr.Error())
			if err != nil {
				log.Printf("[task-worker-%d] mark failed failed task=%s err=%v", workerID, t.ID, err)
			} else {
				p.publishDone(ctx, doneTask.Clone())
			}
			continue
		}
		doneTask, err := p.store.MarkSucceeded(t.ID, result)
		if err != nil {
			log.Printf("[task-worker-%d] mark succeeded failed task=%s err=%v", workerID, t.ID, err)
		} else {
			p.publishDone(ctx, doneTask.Clone())
		}
	}
}

func (p *Pool) publishDone(ctx context.Context, doneTask *Task) {
	if p.onFinish == nil || doneTask == nil {
		return
	}
	p.onFinish(ctx, doneTask)
}
