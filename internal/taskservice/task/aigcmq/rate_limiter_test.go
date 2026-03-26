package aigcmq

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_BothBucketsMustHaveCapacity(t *testing.T) {
	limiter := NewRateLimiter(RateLimiterOptions{
		IPM: 1,
		IPD: 100,
	})
	ctx := context.Background()
	if err := limiter.Wait(ctx, 1); err != nil {
		t.Fatalf("first wait failed: %v", err)
	}
	wait := limiter.tryReserve(time.Now(), 1)
	if wait <= 0 {
		t.Fatalf("expected wait > 0 when ipm bucket is empty")
	}
}

func TestRateLimiter_WaitsWhenIPDBucketInsufficient(t *testing.T) {
	limiter := NewRateLimiter(RateLimiterOptions{
		IPM: 100,
		IPD: 1,
	})
	ctx := context.Background()
	if err := limiter.Wait(ctx, 1); err != nil {
		t.Fatalf("first wait failed: %v", err)
	}
	wait := limiter.tryReserve(time.Now(), 1)
	if wait <= 0 {
		t.Fatalf("expected wait > 0 when ipd bucket is empty")
	}
}
