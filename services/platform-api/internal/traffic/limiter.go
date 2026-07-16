package traffic

import (
	"sync"
	"time"
)

type limiterEntry struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

// Limiter applies a bounded per-user token bucket.
type Limiter struct {
	rate       float64
	burst      float64
	maxEntries int
	now        func() time.Time
	mu         sync.Mutex
	entries    map[uint64]limiterEntry
}

// NewLimiter returns a bounded per-user limiter.
func NewLimiter(ratePerSecond, burst, maxEntries int) *Limiter {
	return &Limiter{
		rate:       float64(ratePerSecond),
		burst:      float64(burst),
		maxEntries: maxEntries,
		now:        time.Now,
		entries:    make(map[uint64]limiterEntry),
	}
}

// Allow consumes one user token when available.
func (l *Limiter) Allow(userID uint64) bool {
	if userID == 0 || l.rate <= 0 || l.burst <= 0 || l.maxEntries <= 0 {
		return false
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[userID]
	if !ok {
		if len(l.entries) >= l.maxEntries {
			l.evictOldest()
		}
		entry = limiterEntry{tokens: l.burst, updated: now}
	}
	entry.tokens = min(l.burst, entry.tokens+now.Sub(entry.updated).Seconds()*l.rate)
	entry.updated = now
	entry.lastSeen = now
	allowed := entry.tokens >= 1
	if allowed {
		entry.tokens--
	}
	l.entries[userID] = entry
	return allowed
}

func (l *Limiter) evictOldest() {
	var oldestID uint64
	var oldest time.Time
	for userID, entry := range l.entries {
		if oldestID == 0 || entry.lastSeen.Before(oldest) {
			oldestID = userID
			oldest = entry.lastSeen
		}
	}
	delete(l.entries, oldestID)
}
