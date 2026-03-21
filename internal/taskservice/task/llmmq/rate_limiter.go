package llmmq

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type RateLimiterOptions struct {
	RPM int
	TPM int
}

type tokenEvent struct {
	at     time.Time
	tokens int
}

type RateLimiter struct {
	rpm int
	tpm int

	mu          sync.Mutex
	reqEvents   []time.Time
	tokenEvents []tokenEvent
	tokenSum    int
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
		rpm: rpm,
		tpm: tpm,
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
	cutoff := now.Add(-time.Minute)
	l.prune(cutoff)
	waitReq := l.waitDurationByRPM(now)
	waitTok := l.waitDurationByTPM(now, tokens)
	wait := maxDuration(waitReq, waitTok)
	if wait > 0 {
		return wait
	}
	l.reqEvents = append(l.reqEvents, now)
	if l.tpm > 0 {
		l.tokenEvents = append(l.tokenEvents, tokenEvent{
			at:     now,
			tokens: tokens,
		})
		l.tokenSum += tokens
	}
	return 0
}

func (l *RateLimiter) prune(cutoff time.Time) {
	keptReq := l.reqEvents[:0]
	for _, t := range l.reqEvents {
		if t.After(cutoff) {
			keptReq = append(keptReq, t)
		}
	}
	l.reqEvents = keptReq

	if l.tpm <= 0 {
		l.tokenEvents = l.tokenEvents[:0]
		l.tokenSum = 0
		return
	}
	keptTok := l.tokenEvents[:0]
	sum := 0
	for _, e := range l.tokenEvents {
		if e.at.After(cutoff) {
			keptTok = append(keptTok, e)
			sum += e.tokens
		}
	}
	l.tokenEvents = keptTok
	l.tokenSum = sum
}

func (l *RateLimiter) waitDurationByRPM(now time.Time) time.Duration {
	if l.rpm <= 0 || len(l.reqEvents) < l.rpm {
		return 0
	}
	oldest := l.reqEvents[0]
	waitUntil := oldest.Add(time.Minute)
	if waitUntil.After(now) {
		return waitUntil.Sub(now)
	}
	return 0
}

func (l *RateLimiter) waitDurationByTPM(now time.Time, tokens int) time.Duration {
	if l.tpm <= 0 {
		return 0
	}
	if tokens > l.tpm {
		tokens = l.tpm
	}
	if l.tokenSum+tokens <= l.tpm {
		return 0
	}
	sum := l.tokenSum
	for _, e := range l.tokenEvents {
		sum -= e.tokens
		waitUntil := e.at.Add(time.Minute)
		if sum+tokens <= l.tpm {
			if waitUntil.After(now) {
				return waitUntil.Sub(now)
			}
			return 0
		}
	}
	if len(l.tokenEvents) > 0 {
		waitUntil := l.tokenEvents[0].at.Add(time.Minute)
		if waitUntil.After(now) {
			return waitUntil.Sub(now)
		}
	}
	return 50 * time.Millisecond
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
