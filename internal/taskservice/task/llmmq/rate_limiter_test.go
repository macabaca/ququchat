package llmmq

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_BothBucketsMustHaveCapacity(t *testing.T) {
	limiter := NewRateLimiter(RateLimiterOptions{RPM: 1, TPM: 100})
	if err := limiter.Wait(context.Background(), "hello"); err != nil {
		t.Fatalf("first wait failed: %v", err)
	}
	if wait := limiter.tryAcquire(time.Now(), 1); wait <= 0 {
		t.Fatalf("expected wait > 0 when rpm window is full")
	}
}

func TestRateLimiter_WaitsWhenTPMBucketInsufficient(t *testing.T) {
	limiter := NewRateLimiter(RateLimiterOptions{RPM: 100, TPM: 10})
	if err := limiter.Wait(context.Background(), "a"); err != nil {
		t.Fatalf("first wait failed: %v", err)
	}
	if wait := limiter.tryAcquire(time.Now(), 10); wait <= 0 {
		t.Fatalf("expected wait > 0 when tpm window is full")
	}
}
