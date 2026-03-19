package aigcmq

import (
	"context"
	"errors"
	"sync"
)

type PoolOptions struct {
	URL             string
	RequestQueue    string
	Provider        Provider
	AttachmentSaver AttachmentSaver
	Size            int
	IPM             int
	IPD             int
}

type Pool struct {
	workers []*Worker
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewPool(opts PoolOptions) (*Pool, error) {
	size := opts.Size
	if size <= 0 {
		size = 1
	}
	workers := make([]*Worker, 0, size)
	limiter := NewRateLimiter(RateLimiterOptions{
		IPM: opts.IPM,
		IPD: opts.IPD,
	})
	for i := 0; i < size; i++ {
		worker, err := NewWorker(WorkerOptions{
			URL:             opts.URL,
			RequestQueue:    opts.RequestQueue,
			Provider:        opts.Provider,
			RateLimiter:     limiter,
			AttachmentSaver: opts.AttachmentSaver,
		})
		if err != nil {
			return nil, err
		}
		workers = append(workers, worker)
	}
	return &Pool{workers: workers}, nil
}

func (p *Pool) Start(ctx context.Context) error {
	if p == nil {
		return errors.New("aigc worker pool is nil")
	}
	runCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	for _, worker := range p.workers {
		w := worker
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			_ = w.Start(runCtx)
		}()
	}
	return nil
}

func (p *Pool) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if p.cancel != nil {
		p.cancel()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.wg.Wait()
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
