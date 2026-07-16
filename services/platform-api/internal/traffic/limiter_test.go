package traffic

import (
	"testing"
	"time"
)

func TestLimiterRefillsAndEvicts(t *testing.T) {
	limiter := NewLimiter(1, 2, 1)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	if !limiter.Allow(1) || !limiter.Allow(1) || limiter.Allow(1) {
		t.Fatal("initial burst did not enforce two tokens")
	}
	now = now.Add(time.Second)
	if !limiter.Allow(1) {
		t.Fatal("token did not refill")
	}
	if !limiter.Allow(2) {
		t.Fatal("second user should receive a fresh bucket after eviction")
	}
	if len(limiter.entries) != 1 {
		t.Fatalf("entries = %d; want 1", len(limiter.entries))
	}
}
