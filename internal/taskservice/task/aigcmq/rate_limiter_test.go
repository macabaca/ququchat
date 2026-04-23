package aigcmq

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_BothBucketsMustHaveCapacity(t *testing.T) {
	limiter := NewRateLimiter(RateLimiterOptions{IPM: 1, IPD: 100})
	if err := limiter.Wait(context.Background(), 1); err != nil {
		t.Fatalf("first wait failed: %v", err)
	}
	if wait := limiter.tryAcquire(time.Now(), 1); wait <= 0 {
		t.Fatalf("expected wait > 0 when ipm window is full")
	}
}

func TestRateLimiter_WaitsWhenIPDBucketInsufficient(t *testing.T) {
	limiter := NewRateLimiter(RateLimiterOptions{IPM: 100, IPD: 1})
	if err := limiter.Wait(context.Background(), 1); err != nil {
		t.Fatalf("first wait failed: %v", err)
	}
	if wait := limiter.tryAcquire(time.Now(), 1); wait <= 0 {
		t.Fatalf("expected wait > 0 when ipd window is full")
	}
}
