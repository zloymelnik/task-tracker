package ratelimit

import (
	"fmt"
	"math"
	"sync"
	"time"
)

type entry struct {
	tokens    float64
	updatedAt time.Time
}

type Limiter struct {
	mu          sync.Mutex
	entries     map[string]*entry
	interval    time.Duration
	burst       int
	nextCleanup time.Time
	maxEntries  int
}

func NewLimiter(interval time.Duration, burst int, maxEntries int) (*Limiter, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("init limiter error: interval must be more than 0")
	}
	if burst <= 0 {
		return nil, fmt.Errorf("init limiter error: burst must be more than 0")
	}
	if maxEntries <= 0 {
		return nil, fmt.Errorf("init limiter error: maxEntries must be more than 0")
	}
	return &Limiter{
		interval:   interval,
		burst:      burst,
		entries:    make(map[string]*entry),
		maxEntries: maxEntries,
	}, nil
}

func (l *Limiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.allow(key, time.Now())
}

// allow contains check logick.
// The caller must hold l.mu.
func (l *Limiter) allow(key string, now time.Time) (allowed bool, retryAfter time.Duration) {
	if !now.Before(l.nextCleanup) {
		l.cleanup(now)
		l.nextCleanup = now.Add(time.Minute)
	}
	e, ok := l.entries[key]
	if !ok {
		if len(l.entries) >= l.maxEntries {
			return false, l.nextCleanup.Sub(now)
		}
		e = &entry{
			tokens:    float64(l.burst),
			updatedAt: now,
		}
		l.entries[key] = e
	} else {
		elapsed := now.Sub(e.updatedAt)
		e.tokens = min(
			float64(l.burst),
			e.tokens+float64(elapsed)/float64(l.interval),
		)
		e.updatedAt = now
	}
	if e.tokens >= 1 {
		e.tokens--
		return true, 0
	}
	retryAfter = time.Duration(math.Ceil(
		(1 - e.tokens) * float64(l.interval),
	))
	return false, retryAfter
}

// cleanup removes fully replenished entries.
// The caller must hold l.mu.
func (l *Limiter) cleanup(now time.Time) {
	for key, e := range l.entries {
		elapsed := now.Sub(e.updatedAt)
		tokens := e.tokens + float64(elapsed)/float64(l.interval)

		if tokens >= float64(l.burst) {
			delete(l.entries, key)
		}
	}
}
