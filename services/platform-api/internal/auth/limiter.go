package auth

import (
	"sync"
	"time"
)

type limitEntry struct {
	windowStartedAt time.Time
	failures        int
	blockedUntil    time.Time
	updatedAt       time.Time
}

// Limiter keeps bounded in-memory login failure state.
type Limiter struct {
	mu            sync.Mutex
	entries       map[string]limitEntry
	window        time.Duration
	maxFailures   int
	blockDuration time.Duration
	maxEntries    int
}

// NewLimiter returns a bounded login failure limiter.
func NewLimiter(
	window time.Duration,
	maxFailures int,
	blockDuration time.Duration,
	maxEntries int,
) *Limiter {
	return &Limiter{
		entries:       make(map[string]limitEntry),
		window:        window,
		maxFailures:   maxFailures,
		blockDuration: blockDuration,
		maxEntries:    maxEntries,
	}
}

// Blocked reports whether a login key is temporarily blocked.
func (l *Limiter) Blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.prune(now)
	entry, ok := l.entries[key]
	return ok && now.Before(entry.blockedUntil)
}

// Failure records one failed login attempt.
func (l *Limiter) Failure(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.prune(now)
	entry, ok := l.entries[key]
	if !ok {
		l.makeRoom()
		entry.windowStartedAt = now
	}
	if now.Sub(entry.windowStartedAt) >= l.window {
		entry.windowStartedAt = now
		entry.failures = 0
	}
	entry.failures++
	entry.updatedAt = now
	if entry.failures >= l.maxFailures {
		entry.blockedUntil = now.Add(l.blockDuration)
	}
	l.entries[key] = entry
}

// Success clears login failure state after a successful login.
func (l *Limiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *Limiter) prune(now time.Time) {
	for key, entry := range l.entries {
		windowExpired := now.Sub(entry.windowStartedAt) >= l.window
		blockExpired := entry.blockedUntil.IsZero() || !now.Before(entry.blockedUntil)
		if windowExpired && blockExpired {
			delete(l.entries, key)
		}
	}
}

func (l *Limiter) makeRoom() {
	if len(l.entries) < l.maxEntries {
		return
	}
	var oldestKey string
	var oldestTime time.Time
	for key, entry := range l.entries {
		if oldestKey == "" || entry.updatedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.updatedAt
		}
	}
	delete(l.entries, oldestKey)
}
