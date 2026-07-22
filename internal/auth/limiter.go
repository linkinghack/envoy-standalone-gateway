package auth

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type accountFailures struct {
	count       int
	lockedUntil time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	byIP     map[string][]time.Time
	accounts map[string]accountFailures
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{byIP: map[string][]time.Time{}, accounts: map[string]accountFailures{}}
}

func (l *loginLimiter) allow(now time.Time, ip, username string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := strings.ToLower(username)
	failure := l.accounts[key]
	if now.Before(failure.lockedUntil) {
		return RateLimitError{RetryAfter: failure.lockedUntil.Sub(now)}
	}
	if !failure.lockedUntil.IsZero() {
		delete(l.accounts, key)
	}
	cutoff := now.Add(-time.Minute)
	attempts := l.byIP[ip][:0]
	for _, attempt := range l.byIP[ip] {
		if attempt.After(cutoff) {
			attempts = append(attempts, attempt)
		}
	}
	if len(attempts) >= 10 {
		return RateLimitError{RetryAfter: attempts[0].Add(time.Minute).Sub(now)}
	}
	l.byIP[ip] = append(attempts, now)
	return nil
}

func (l *loginLimiter) failure(now time.Time, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := strings.ToLower(username)
	failure := l.accounts[key]
	failure.count++
	if failure.count >= 5 {
		failure.count = 0
		failure.lockedUntil = now.Add(5 * time.Minute)
	}
	l.accounts[key] = failure
}

func (l *loginLimiter) success(username string) {
	l.mu.Lock()
	delete(l.accounts, strings.ToLower(username))
	l.mu.Unlock()
}

// RateLimitError carries the minimum retry delay for an HTTP Retry-After header.
type RateLimitError struct{ RetryAfter time.Duration }

func (e RateLimitError) Error() string {
	return fmt.Sprintf("authentication rate limited for %s", e.RetryAfter.Round(time.Second))
}
