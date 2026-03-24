package embeddingmq

import (
	"context"
	"errors"
	"sync"
)

type PoolOptions struct {
	URL          string
	RequestQueue string
	Provider     Provider
	Size         int
	RPM          int
	TPM          int
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
		RPM: opts.RPM,
		TPM: opts.TPM,
	})
	for i := 0; i < size; i++ {
		worker, err := NewWorker(WorkerOptions{
			URL:          opts.URL,
			RequestQueue: opts.RequestQueue,
			Prefetch:     size,
			Provider:     opts.Provider,
			RateLimiter:  limiter,
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
		return errors.New("embedding worker pool is nil")
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
