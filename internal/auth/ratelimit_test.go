package auth

import (
	"strconv"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := NewLimiter(5, 15*time.Minute)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	for i := range 5 {
		if _, blocked := l.Blocked("ip:1", "user:1"); blocked {
			t.Fatalf("attempt %d blocked", i+1)
		}
		l.Fail("ip:1", "user:1")
		now = now.Add(time.Minute)
	}
	// 5 failures at 12:00..12:04, now 12:05: the 6th attempt is blocked
	// until the first failure leaves the window at 12:15.
	retry, blocked := l.Blocked("ip:1", "user:1")
	if !blocked || retry != 10*time.Minute {
		t.Fatalf("6th attempt: blocked = %v, retry = %v; want true, 10m", blocked, retry)
	}

	// Keys are independent: another IP for the same user is blocked by the user key,
	// the same IP for another user by the IP key, and an unrelated pair is free.
	if _, blocked := l.Blocked("ip:2", "user:1"); !blocked {
		t.Error("other IP, same user: not blocked")
	}
	if _, blocked := l.Blocked("ip:1", "user:2"); !blocked {
		t.Error("same IP, other user: not blocked")
	}
	if _, blocked := l.Blocked("ip:2", "user:2"); blocked {
		t.Error("unrelated keys blocked")
	}

	now = now.Add(10 * time.Minute) // 12:15
	if _, blocked := l.Blocked("ip:1"); blocked {
		t.Error("still blocked after the oldest failure left the window")
	}
	l.Fail("ip:1")
	if _, blocked := l.Blocked("ip:1"); !blocked {
		t.Error("not blocked again after a new failure")
	}

	now = now.Add(time.Hour)
	l.Fail("ip:3") // triggers a sweep
	if len(l.fails) != 1 {
		t.Errorf("keys after sweep = %d, want 1", len(l.fails))
	}
}

func TestLimiterKeyCap(t *testing.T) {
	l := NewLimiter(1, 15*time.Minute)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	l.Fail("user:1")
	for i := range maxLimiterKeys {
		l.Fail("ip:" + strconv.Itoa(i))
	}
	if len(l.fails) > maxLimiterKeys {
		t.Errorf("keys = %d, want at most %d", len(l.fails), maxLimiterKeys)
	}
	// A key tracked before the flood keeps working; a new one isn't tracked.
	if _, blocked := l.Blocked("user:1"); !blocked {
		t.Error("tracked key not blocked after the flood")
	}
	l.Fail("user:2")
	if _, blocked := l.Blocked("user:2"); blocked {
		t.Error("new key tracked past the cap")
	}
}
