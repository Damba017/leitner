package auth

import (
	"sync"
	"time"
)

// maxLimiterKeys caps memory if someone floods logins with distinct names or IPs.
// Past the cap, new keys aren't tracked until old ones expire; keys already
// tracked (such as a real username under attack) keep working.
const maxLimiterKeys = 100_000

// Limiter counts failed attempts per key (an IP or a user) in a sliding window.
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	fails     map[string][]time.Time // oldest first
	lastSweep time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, now: time.Now, fails: map[string][]time.Time{}}
}

// Blocked reports whether any of keys has reached the limit, and if so how
// long until another attempt is allowed.
func (l *Limiter) Blocked(keys ...string) (retryAfter time.Duration, blocked bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for _, k := range keys {
		ts := l.recent(k, now)
		if len(ts) < l.max {
			continue
		}
		// Allowed again once enough of the oldest failures have left the window.
		wait := ts[len(ts)-l.max].Add(l.window).Sub(now)
		if wait > retryAfter {
			retryAfter = wait
		}
		blocked = true
	}
	return retryAfter, blocked
}

// Fail records a failed attempt for each of keys.
func (l *Limiter) Fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) >= l.window || len(l.fails) >= maxLimiterKeys {
		for k := range l.fails {
			l.recent(k, now)
		}
		l.lastSweep = now
	}
	for _, k := range keys {
		ts := l.recent(k, now)
		if ts == nil && len(l.fails) >= maxLimiterKeys {
			continue
		}
		l.fails[k] = append(ts, now)
	}
}

// recent drops the failures of k that are outside the window and returns the rest.
func (l *Limiter) recent(k string, now time.Time) []time.Time {
	ts := l.fails[k]
	i := 0
	for i < len(ts) && now.Sub(ts[i]) >= l.window {
		i++
	}
	if i == len(ts) {
		delete(l.fails, k)
		return nil
	}
	if i > 0 {
		ts = append(ts[:0], ts[i:]...)
		l.fails[k] = ts
	}
	return ts
}
