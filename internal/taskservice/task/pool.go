package tasksvc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type Pool struct {
	queue                 ConsumerQueue
	store                 Store
	exec                  Executor
	workerSize            int
	onFinish              func(ctx context.Context, doneTask *Task)
	inputRetryMaxAttempts int
	inputRetryDelay       time.Duration
}

func NewPool(queue ConsumerQueue, store Store, exec Executor, workerSize int, onFinish func(ctx context.Context, doneTask *Task), inputRetryMaxAttempts int, inputRetryDelay time.Duration) *Pool {
	if workerSize <= 0 {
		workerSize = 1
	}
	if inputRetryMaxAttempts <= 0 {
		inputRetryMaxAttempts = 3
	}
	if inputRetryDelay <= 0 {
		inputRetryDelay = 500 * time.Millisecond
	}
	return &Pool{
		queue:                 queue,
		store:                 store,
		exec:                  exec,
		workerSize:            workerSize,
		onFinish:              onFinish,
		inputRetryMaxAttempts: inputRetryMaxAttempts,
		inputRetryDelay:       inputRetryDelay,
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
		msg, err := p.queue.Pop(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("[task-worker-%d] pop failed: %v", workerID, err)
			if !sleepWithContext(ctx, p.inputRetryDelay) {
				return
			}
			continue
		}
		t := msg.Task()
		if t == nil || t.ID == "" {
			_ = msg.Ack()
			log.Printf("[task-worker-%d] invalid queue message", workerID)
			continue
		}
		runningTask, err := p.store.MarkRunning(t.ID)
		if err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				_ = msg.Ack()
			} else {
				recoveredRunningTask := (*Task)(nil)
				processErr := p.retryInputOperation(ctx, t.ID, "mark_running", func() error {
					nextRunningTask, opErr := p.store.MarkRunning(t.ID)
					if opErr == nil {
						recoveredRunningTask = nextRunningTask
					}
					return opErr
				})
				if processErr != nil {
					_ = p.markFailedWithLog(t.ID, processErr)
					_ = msg.Ack()
					log.Printf("[task-worker-%d] input failed and reached retry limit task=%s err=%v", workerID, t.ID, processErr)
					continue
				}
				runningTask = recoveredRunningTask
			}
			log.Printf("[task-worker-%d] mark running failed task=%s err=%v", workerID, t.ID, err)
			if runningTask == nil {
				continue
			}
		}
		result, execErr := p.exec.Execute(ctx, runningTask)
		if execErr != nil {
			doneTask, err := p.store.MarkFailed(t.ID, execErr.Error())
			if err != nil {
				log.Printf("[task-worker-%d] mark failed failed task=%s err=%v", workerID, t.ID, err)
			} else {
				p.publishDone(ctx, doneTask.Clone())
			}
			_ = msg.Ack()
			continue
		}
		doneTask, err := p.store.MarkSucceeded(t.ID, result)
		if err != nil {
			processErr := p.retryInputOperation(ctx, t.ID, "mark_succeeded", func() error {
				_, opErr := p.store.MarkSucceeded(t.ID, result)
				return opErr
			})
			if processErr != nil {
				_ = p.markFailedWithLog(t.ID, processErr)
				log.Printf("[task-worker-%d] mark succeeded failed and reached retry limit task=%s err=%v", workerID, t.ID, processErr)
			} else {
				doneTask, _ = p.store.Get(t.ID)
			}
		}
		p.publishDone(ctx, doneTask.Clone())
		_ = msg.Ack()
	}
}

func (p *Pool) publishDone(ctx context.Context, doneTask *Task) {
	if p.onFinish == nil || doneTask == nil {
		return
	}
	p.onFinish(ctx, doneTask)
}

func (p *Pool) retryInputOperation(ctx context.Context, taskID string, opName string, fn func() error) error {
	var lastErr error
	for attempt := 1; attempt <= p.inputRetryMaxAttempts; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		log.Printf("[task-retry] task=%s op=%s attempt=%d/%d err=%v", taskID, opName, attempt, p.inputRetryMaxAttempts, lastErr)
		if attempt == p.inputRetryMaxAttempts {
			break
		}
		if !sleepWithContext(ctx, p.inputRetryDelay) {
			return ctx.Err()
		}
	}
	return fmt.Errorf("task=%s op=%s exhausted retries: %w", taskID, opName, lastErr)
}

func (p *Pool) markFailedWithLog(taskID string, err error) error {
	if strings.TrimSpace(taskID) == "" || err == nil {
		return nil
	}
	_, markErr := p.store.MarkFailed(taskID, err.Error())
	if markErr != nil {
		log.Printf("[task-retry] mark failed to db failed task=%s err=%v", taskID, markErr)
		return markErr
	}
	return nil
}

func sleepWithContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
