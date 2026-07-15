package auth

import (
	"testing"
	"time"
)

func TestLimiterBlocksAndExpires(t *testing.T) {
	limiter := NewLimiter(5*time.Minute, 2, 10*time.Minute, 10)
	now := time.Date(2026, time.July, 15, 8, 0, 0, 0, time.UTC)

	limiter.Failure("learner", now)
	if limiter.Blocked("learner", now) {
		t.Fatal("Blocked() = true after one failure")
	}
	limiter.Failure("learner", now.Add(time.Second))
	if !limiter.Blocked("learner", now.Add(2*time.Second)) {
		t.Fatal("Blocked() = false after maximum failures")
	}
	if limiter.Blocked("learner", now.Add(11*time.Minute)) {
		t.Fatal("Blocked() = true after block expiration")
	}
}

func TestLimiterSuccessClearsFailures(t *testing.T) {
	limiter := NewLimiter(time.Minute, 2, time.Minute, 10)
	now := time.Now().UTC()
	limiter.Failure("learner", now)
	limiter.Success("learner")
	limiter.Failure("learner", now.Add(time.Second))
	if limiter.Blocked("learner", now.Add(2*time.Second)) {
		t.Fatal("Blocked() = true after successful login cleared state")
	}
}
