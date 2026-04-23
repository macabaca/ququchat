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

type RateLimiter struct {
	mu      sync.Mutex
	rpm     int
	tpm     int
	rpmRing *slidingWindow
	tpmRing *slidingWindow
}

func NewRateLimiter(opts RateLimiterOptions) *RateLimiter {
	rpm := max0(opts.RPM)
	tpm := max0(opts.TPM)
	rl := &RateLimiter{rpm: rpm, tpm: tpm}
	if rpm > 0 {
		rl.rpmRing = newSlidingWindow(rpm)
	}
	if tpm > 0 {
		tpmCap := rpm
		if tpmCap <= 0 {
			tpmCap = 10000
		}
		rl.tpmRing = newSlidingWindow(tpmCap)
	}
	return rl
}

func (l *RateLimiter) Wait(ctx context.Context, prompt string) error {
	if l == nil || (l.rpm <= 0 && l.tpm <= 0) {
		return nil
	}
	tokens := l.EstimateTokens(prompt)
	for {
		wait := l.tryAcquire(time.Now(), tokens)
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

func (l *RateLimiter) tryAcquire(now time.Time, tokens int) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	var waitRPM, waitTPM time.Duration
	if l.rpmRing != nil {
		waitRPM = l.rpmRing.waitDuration(now, 1, l.rpm)
	}
	if l.tpmRing != nil {
		need := tokens
		if need > l.tpm {
			need = l.tpm
		}
		waitTPM = l.tpmRing.waitDuration(now, need, l.tpm)
	}
	wait := maxDuration(waitRPM, waitTPM)
	if wait > 0 {
		return wait
	}
	if l.rpmRing != nil {
		l.rpmRing.push(now, 1)
	}
	if l.tpmRing != nil {
		need := tokens
		if need > l.tpm {
			need = l.tpm
		}
		l.tpmRing.push(now, need)
	}
	return 0
}

func (l *RateLimiter) EstimateTokens(prompt string) int {
	const baseOverhead = 32
	const maxOutput = 8192
	n := utf8.RuneCountInString(strings.TrimSpace(prompt)) + baseOverhead + maxOutput
	if l != nil && l.tpm > 0 && n > l.tpm {
		return l.tpm
	}
	return n
}

// slidingWindow is a fixed-capacity ring buffer tracking (timestamp, weight) entries
// within a 60-second window. capacity = RPM or TPM limit.
type slidingWindow struct {
	buf         []swEntry
	head, tail  int
	count       int // number of valid entries
	windowSum   int // sum of weights currently in window
}

type swEntry struct {
	t      time.Time
	weight int
}

func newSlidingWindow(capacity int) *slidingWindow {
	return &slidingWindow{buf: make([]swEntry, capacity)}
}

const windowDuration = 60 * time.Second

func (w *slidingWindow) evict(now time.Time) {
	cutoff := now.Add(-windowDuration)
	for w.count > 0 && w.buf[w.head].t.Before(cutoff) {
		w.windowSum -= w.buf[w.head].weight
		w.head = (w.head + 1) % len(w.buf)
		w.count--
	}
}

// waitDuration returns how long to wait before `needed` more weight fits in the window.
func (w *slidingWindow) waitDuration(now time.Time, needed, limit int) time.Duration {
	w.evict(now)
	if w.windowSum+needed <= limit {
		return 0
	}
	// Walk from head until enough weight is evicted to fit `needed`.
	sum := w.windowSum
	i := w.head
	for n := 0; n < w.count; n++ {
		sum -= w.buf[i].weight
		if sum+needed <= limit {
			// This entry expiring is enough — wait until it leaves the window.
			expiry := w.buf[i].t.Add(windowDuration)
			if d := expiry.Sub(now); d > 0 {
				return d
			}
			return time.Millisecond
		}
		i = (i + 1) % len(w.buf)
	}
	return windowDuration
}

func (w *slidingWindow) push(now time.Time, weight int) {
	if w.count == len(w.buf) {
		// Ring is full: overwrite oldest (should not happen if waitDuration is respected).
		w.windowSum -= w.buf[w.head].weight
		w.head = (w.head + 1) % len(w.buf)
		w.count--
	}
	w.buf[w.tail] = swEntry{t: now, weight: weight}
	w.tail = (w.tail + 1) % len(w.buf)
	w.count++
	w.windowSum += weight
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
