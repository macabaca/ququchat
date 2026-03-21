package aigcmq

import (
	"context"
	"sync"
	"time"
)

type RateLimiterOptions struct {
	IPM int
	IPD int
}

type countEvent struct {
	at    time.Time
	count int
}

type RateLimiter struct {
	ipm int
	ipd int

	mu         sync.Mutex
	minuteSum  int
	daySum     int
	minuteHits []countEvent
	dayHits    []countEvent
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
		ipm: ipm,
		ipd: ipd,
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

	l.prune(now)
	waitMinute := l.waitByIPM(now, imageCount)
	waitDay := l.waitByIPD(now, imageCount)
	wait := maxDuration(waitMinute, waitDay)
	if wait > 0 {
		return wait
	}

	l.minuteHits = append(l.minuteHits, countEvent{at: now, count: imageCount})
	l.dayHits = append(l.dayHits, countEvent{at: now, count: imageCount})
	l.minuteSum += imageCount
	l.daySum += imageCount
	return 0
}

func (l *RateLimiter) prune(now time.Time) {
	minuteCutoff := now.Add(-time.Minute)
	dayCutoff := now.Add(-24 * time.Hour)

	keptMinute := l.minuteHits[:0]
	minuteSum := 0
	for _, event := range l.minuteHits {
		if event.at.After(minuteCutoff) {
			keptMinute = append(keptMinute, event)
			minuteSum += event.count
		}
	}
	l.minuteHits = keptMinute
	l.minuteSum = minuteSum

	keptDay := l.dayHits[:0]
	daySum := 0
	for _, event := range l.dayHits {
		if event.at.After(dayCutoff) {
			keptDay = append(keptDay, event)
			daySum += event.count
		}
	}
	l.dayHits = keptDay
	l.daySum = daySum
}

func (l *RateLimiter) waitByIPM(now time.Time, imageCount int) time.Duration {
	if l.ipm <= 0 {
		return 0
	}
	if imageCount > l.ipm {
		imageCount = l.ipm
	}
	if l.minuteSum+imageCount <= l.ipm {
		return 0
	}
	sum := l.minuteSum
	for _, event := range l.minuteHits {
		sum -= event.count
		waitUntil := event.at.Add(time.Minute)
		if sum+imageCount <= l.ipm {
			if waitUntil.After(now) {
				return waitUntil.Sub(now)
			}
			return 0
		}
	}
	if len(l.minuteHits) > 0 {
		waitUntil := l.minuteHits[0].at.Add(time.Minute)
		if waitUntil.After(now) {
			return waitUntil.Sub(now)
		}
	}
	return 50 * time.Millisecond
}

func (l *RateLimiter) waitByIPD(now time.Time, imageCount int) time.Duration {
	if l.ipd <= 0 {
		return 0
	}
	if imageCount > l.ipd {
		imageCount = l.ipd
	}
	if l.daySum+imageCount <= l.ipd {
		return 0
	}
	sum := l.daySum
	for _, event := range l.dayHits {
		sum -= event.count
		waitUntil := event.at.Add(24 * time.Hour)
		if sum+imageCount <= l.ipd {
			if waitUntil.After(now) {
				return waitUntil.Sub(now)
			}
			return 0
		}
	}
	if len(l.dayHits) > 0 {
		waitUntil := l.dayHits[0].at.Add(24 * time.Hour)
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
