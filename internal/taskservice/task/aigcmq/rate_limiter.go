package aigcmq

import (
	"context"
	"math"
	"sync"
	"time"
)

type RateLimiterOptions struct {
	IPM int
	IPD int
}

type RateLimiter struct {
	ipm int
	ipd int

	mu           sync.Mutex
	minuteBucket *tokenBucket
	dayBucket    *tokenBucket
}

type tokenBucket struct {
	capacity     float64
	tokens       float64
	refillPerSec float64
	lastRefill   time.Time
}

func NewRateLimiter(opts RateLimiterOptions) *RateLimiter {
	ipm := opts.IPM
	if ipm < 0 {
		ipm = 0
	}
	ipd := opts.IPD
	if ipd < 0 {
		ipd = 0
	}
	return &RateLimiter{
		ipm:          ipm,
		ipd:          ipd,
		minuteBucket: newTokenBucket(float64(ipm), float64(ipm)/60.0),
		dayBucket:    newTokenBucket(float64(ipd), float64(ipd)/(24.0*60.0*60.0)),
	}
}

func (l *RateLimiter) Wait(ctx context.Context, imageCount int) error {
	if l == nil || (l.ipm <= 0 && l.ipd <= 0) {
		return nil
	}
	if imageCount <= 0 {
		imageCount = 1
	}
	for {
		wait := l.tryReserve(time.Now(), imageCount)
		if wait <= 0 {
			return nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *RateLimiter) tryReserve(now time.Time, imageCount int) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	waitMinute := l.waitByIPM(now, imageCount)
	waitDay := l.waitByIPD(now, imageCount)
	wait := maxDuration(waitMinute, waitDay)
	if wait > 0 {
		return wait
	}
	if l.minuteBucket != nil {
		reserveMinute := float64(imageCount)
		if l.minuteBucket.capacity > 0 && reserveMinute > l.minuteBucket.capacity {
			reserveMinute = l.minuteBucket.capacity
		}
		if reserveMinute > 0 {
			l.minuteBucket.consume(now, reserveMinute)
		}
	}
	if l.dayBucket != nil {
		reserveDay := float64(imageCount)
		if l.dayBucket.capacity > 0 && reserveDay > l.dayBucket.capacity {
			reserveDay = l.dayBucket.capacity
		}
		if reserveDay > 0 {
			l.dayBucket.consume(now, reserveDay)
		}
	}
	return 0
}

func (l *RateLimiter) waitByIPM(now time.Time, imageCount int) time.Duration {
	if l.ipm <= 0 || l.minuteBucket == nil {
		return 0
	}
	reserve := float64(imageCount)
	if l.minuteBucket.capacity > 0 && reserve > l.minuteBucket.capacity {
		reserve = l.minuteBucket.capacity
	}
	if reserve <= 0 {
		return 0
	}
	return l.minuteBucket.waitDuration(now, reserve)
}

func (l *RateLimiter) waitByIPD(now time.Time, imageCount int) time.Duration {
	if l.ipd <= 0 || l.dayBucket == nil {
		return 0
	}
	reserve := float64(imageCount)
	if l.dayBucket.capacity > 0 && reserve > l.dayBucket.capacity {
		reserve = l.dayBucket.capacity
	}
	if reserve <= 0 {
		return 0
	}
	return l.dayBucket.waitDuration(now, reserve)
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func newTokenBucket(capacity float64, refillPerSec float64) *tokenBucket {
	if capacity <= 0 || refillPerSec <= 0 {
		return nil
	}
	now := time.Now()
	return &tokenBucket{
		capacity:     capacity,
		tokens:       capacity,
		refillPerSec: refillPerSec,
		lastRefill:   now,
	}
}

func (b *tokenBucket) refill(now time.Time) {
	if b == nil {
		return
	}
	if b.lastRefill.IsZero() {
		b.lastRefill = now
		return
	}
	if now.Before(b.lastRefill) {
		return
	}
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	b.tokens = math.Min(b.capacity, b.tokens+elapsed*b.refillPerSec)
	b.lastRefill = now
}

func (b *tokenBucket) waitDuration(now time.Time, needed float64) time.Duration {
	if b == nil || needed <= 0 {
		return 0
	}
	b.refill(now)
	if b.tokens >= needed {
		return 0
	}
	deficit := needed - b.tokens
	if b.refillPerSec <= 0 {
		return time.Second
	}
	seconds := deficit / b.refillPerSec
	wait := time.Duration(math.Ceil(seconds * float64(time.Second)))
	if wait <= 0 {
		return time.Millisecond
	}
	return wait
}

func (b *tokenBucket) consume(now time.Time, amount float64) {
	if b == nil || amount <= 0 {
		return
	}
	b.refill(now)
	if amount >= b.tokens {
		b.tokens = 0
		return
	}
	b.tokens -= amount
}
