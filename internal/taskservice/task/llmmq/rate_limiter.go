package llmmq

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type RateLimiterOptions struct {
	RPM int
	TPM int
}

type RateLimiter struct {
	rpm int
	tpm int

	mu          sync.Mutex
	reqBucket   *tokenBucket
	tokenBucket *tokenBucket
}

type tokenBucket struct {
	capacity     float64
	tokens       float64
	refillPerSec float64
	lastRefill   time.Time
}

func NewRateLimiter(opts RateLimiterOptions) *RateLimiter {
	rpm := opts.RPM
	if rpm < 0 {
		rpm = 0
	}
	tpm := opts.TPM
	if tpm < 0 {
		tpm = 0
	}
	return &RateLimiter{
		rpm:         rpm,
		tpm:         tpm,
		reqBucket:   newTokenBucket(float64(rpm), float64(rpm)/60.0),
		tokenBucket: newTokenBucket(float64(tpm), float64(tpm)/60.0),
	}
}

func (l *RateLimiter) Wait(ctx context.Context, prompt string) error {
	if l == nil || (l.rpm <= 0 && l.tpm <= 0) {
		return nil
	}
	reserveTokens := l.EstimateTokens(prompt)
	for {
		wait := l.tryReserve(time.Now(), reserveTokens)
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

func (l *RateLimiter) EstimateTokens(prompt string) int {
	const baseOverheadTokens = 32
	const conservativeMaxOutputTokens = 8192
	trimmed := strings.TrimSpace(prompt)
	promptTokens := utf8.RuneCountInString(trimmed) + baseOverheadTokens
	if promptTokens < baseOverheadTokens {
		promptTokens = baseOverheadTokens
	}
	estimate := promptTokens + conservativeMaxOutputTokens
	if l != nil && l.tpm > 0 && estimate > l.tpm {
		return l.tpm
	}
	return estimate
}

func (l *RateLimiter) tryReserve(now time.Time, tokens int) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	waitReq := l.waitDurationByRPM(now)
	waitTok := l.waitDurationByTPM(now, tokens)
	wait := maxDuration(waitReq, waitTok)
	if wait > 0 {
		return wait
	}
	if l.reqBucket != nil {
		l.reqBucket.consume(now, 1)
	}
	if l.tokenBucket != nil {
		reserve := float64(tokens)
		if l.tokenBucket.capacity > 0 && reserve > l.tokenBucket.capacity {
			reserve = l.tokenBucket.capacity
		}
		if reserve > 0 {
			l.tokenBucket.consume(now, reserve)
		}
	}
	return 0
}

func (l *RateLimiter) waitDurationByRPM(now time.Time) time.Duration {
	if l.rpm <= 0 || l.reqBucket == nil {
		return 0
	}
	return l.reqBucket.waitDuration(now, 1)
}

func (l *RateLimiter) waitDurationByTPM(now time.Time, tokens int) time.Duration {
	if l.tpm <= 0 || l.tokenBucket == nil {
		return 0
	}
	reserve := float64(tokens)
	if l.tokenBucket.capacity > 0 && reserve > l.tokenBucket.capacity {
		reserve = l.tokenBucket.capacity
	}
	if reserve <= 0 {
		return 0
	}
	return l.tokenBucket.waitDuration(now, reserve)
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
