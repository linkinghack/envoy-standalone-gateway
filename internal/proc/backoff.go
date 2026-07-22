package proc

import (
	"sync"
	"time"
)

// Backoff applies exponential delay and a rolling ten-minute give-up limit.
type Backoff struct {
	Initial    time.Duration
	Max        time.Duration
	ResetAfter time.Duration
	GiveUp     int

	mu       sync.Mutex
	failures []time.Time
	streak   int
}

// Failure records an unexpected exit. runFor resets the exponential streak
// after a stable window. degraded is true once the rolling limit is reached.
func (b *Backoff) Failure(now time.Time, runFor time.Duration) (delay time.Duration, degraded bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.defaults()
	cutoff := now.Add(-10 * time.Minute)
	kept := b.failures[:0]
	for _, failure := range b.failures {
		if failure.After(cutoff) {
			kept = append(kept, failure)
		}
	}
	b.failures = append(kept, now)
	if runFor >= b.ResetAfter {
		b.streak = 0
	}
	delay = b.Initial
	for i := 0; i < b.streak && delay < b.Max; i++ {
		if delay > b.Max/2 {
			delay = b.Max
			break
		}
		delay *= 2
	}
	if delay > b.Max {
		delay = b.Max
	}
	b.streak++
	return delay, len(b.failures) >= b.GiveUp
}

// Reset clears both the rolling failure window and exponential streak.
func (b *Backoff) Reset() {
	b.mu.Lock()
	b.failures = nil
	b.streak = 0
	b.mu.Unlock()
}

func (b *Backoff) defaults() {
	if b.Initial <= 0 {
		b.Initial = time.Second
	}
	if b.Max <= 0 {
		b.Max = 30 * time.Second
	}
	if b.ResetAfter <= 0 {
		b.ResetAfter = time.Minute
	}
	if b.GiveUp <= 0 {
		b.GiveUp = 5
	}
}
