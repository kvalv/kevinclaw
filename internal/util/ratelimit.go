package util

import (
	"sync"
	"time"
)

// perHour tracks message counts per key (e.g. channel ID) within
// rolling one-hour windows. Safe for concurrent use.
type perHour struct {
	mu    sync.Mutex
	limit int
	hits  map[string][]time.Time
}

// NewPerHour creates a rate limiter that allows limit events per key per hour.
func NewPerHour(limit int) *perHour {
	return &perHour{
		limit: limit,
		hits:  make(map[string][]time.Time),
	}
}

// Allow returns true if the key is under the rate limit and records the event.
func (r *perHour) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Hour)

	// Evict old entries
	times := r.hits[key]
	i := 0
	for i < len(times) && times[i].Before(cutoff) {
		i++
	}
	times = times[i:]

	if len(times) >= r.limit {
		r.hits[key] = times
		return false
	}

	r.hits[key] = append(times, now)
	return true
}
