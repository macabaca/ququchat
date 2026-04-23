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

type RateLimiter struct {
	mu      sync.Mutex
	ipm     int
	ipd     int
	ipmRing *slidingWindow
	ipdWin  *fixedDayWindow
}

func NewRateLimiter(opts RateLimiterOptions) *RateLimiter {
	ipm := max0(opts.IPM)
	ipd := max0(opts.IPD)
	rl := &RateLimiter{ipm: ipm, ipd: ipd}
	if ipm > 0 {
		rl.ipmRing = newSlidingWindow(ipm)
	}
	if ipd > 0 {
		rl.ipdWin = &fixedDayWindow{}
	}
	return rl
}

func (l *RateLimiter) Wait(ctx context.Context, imageCount int) error {
	if l == nil || (l.ipm <= 0 && l.ipd <= 0) {
		return nil
	}
	if imageCount <= 0 {
		imageCount = 1
	}
	for {
		wait := l.tryAcquire(time.Now(), imageCount)
		if wait <= 0 {
			return nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *RateLimiter) tryAcquire(now time.Time, imageCount int) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	var waitIPM, waitIPD time.Duration
	if l.ipmRing != nil {
		need := imageCount
		if need > l.ipm {
			need = l.ipm
		}
		waitIPM = l.ipmRing.waitDuration(now, need, l.ipm)
	}
	if l.ipdWin != nil {
		waitIPD = l.ipdWin.waitDuration(now, imageCount, l.ipd)
	}
	wait := maxDuration(waitIPM, waitIPD)
	if wait > 0 {
		return wait
	}
	if l.ipmRing != nil {
		need := imageCount
		if need > l.ipm {
			need = l.ipm
		}
		l.ipmRing.push(now, need)
	}
	if l.ipdWin != nil {
		l.ipdWin.consume(now, imageCount)
	}
	return 0
}

// slidingWindow is a fixed-capacity ring buffer for a 60-second window.
type slidingWindow struct {
	buf        []swEntry
	head, tail int
	count      int
	windowSum  int
}

type swEntry struct {
	t      time.Time
	weight int
}

func newSlidingWindow(capacity int) *slidingWindow {
	return &slidingWindow{buf: make([]swEntry, capacity)}
}

const minuteWindow = 60 * time.Second

func (w *slidingWindow) evict(now time.Time) {
	cutoff := now.Add(-minuteWindow)
	for w.count > 0 && w.buf[w.head].t.Before(cutoff) {
		w.windowSum -= w.buf[w.head].weight
		w.head = (w.head + 1) % len(w.buf)
		w.count--
	}
}

func (w *slidingWindow) waitDuration(now time.Time, needed, limit int) time.Duration {
	w.evict(now)
	if w.windowSum+needed <= limit {
		return 0
	}
	sum := w.windowSum
	i := w.head
	for n := 0; n < w.count; n++ {
		sum -= w.buf[i].weight
		if sum+needed <= limit {
			if d := w.buf[i].t.Add(minuteWindow).Sub(now); d > 0 {
				return d
			}
			return time.Millisecond
		}
		i = (i + 1) % len(w.buf)
	}
	return minuteWindow
}

func (w *slidingWindow) push(now time.Time, weight int) {
	if w.count == len(w.buf) {
		w.windowSum -= w.buf[w.head].weight
		w.head = (w.head + 1) % len(w.buf)
		w.count--
	}
	w.buf[w.tail] = swEntry{t: now, weight: weight}
	w.tail = (w.tail + 1) % len(w.buf)
	w.count++
	w.windowSum += weight
}

// fixedDayWindow resets at midnight (local time).
type fixedDayWindow struct {
	date  time.Time
	count int
}

func (w *fixedDayWindow) today(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}

func (w *fixedDayWindow) waitDuration(now time.Time, needed, limit int) time.Duration {
	today := w.today(now)
	if !today.Equal(w.date) {
		return 0
	}
	if w.count+needed <= limit {
		return 0
	}
	return today.Add(24 * time.Hour).Sub(now)
}

func (w *fixedDayWindow) consume(now time.Time, n int) {
	today := w.today(now)
	if !today.Equal(w.date) {
		w.date = today
		w.count = 0
	}
	w.count += n
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
