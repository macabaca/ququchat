package embeddingmq

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_BothBucketsMustHaveCapacity(t *testing.T) {
	limiter := NewRateLimiter(RateLimiterOptions{
		RPM: 1,
		TPM: 100,
	})
	ctx := context.Background()
	if err := limiter.Wait(ctx, []string{"hello"}); err != nil {
		t.Fatalf("first wait failed: %v", err)
	}
	wait := limiter.tryReserve(time.Now(), 1)
	if wait <= 0 {
		t.Fatalf("expected wait > 0 when rpm bucket is empty")
	}
}

func TestRateLimiter_WaitsWhenTPMBucketInsufficient(t *testing.T) {
	limiter := NewRateLimiter(RateLimiterOptions{
		RPM: 100,
		TPM: 10,
	})
	ctx := context.Background()
	if err := limiter.Wait(ctx, []string{"a"}); err != nil {
		t.Fatalf("first wait failed: %v", err)
	}
	wait := limiter.tryReserve(time.Now(), 10)
	if wait <= 0 {
		t.Fatalf("expected wait > 0 when tpm bucket is empty")
	}
}
