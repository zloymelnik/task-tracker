package ratelimit_test

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zloymelnik/task-tracker/internal/ratelimit"
)

func TestNewLimiter(t *testing.T) {

	for _, tt := range []struct {
		name       string
		interval   time.Duration
		burst      int
		maxEntries int
	}{
		{"zero interval", 0, 1, 1},
		{"negative interval", -time.Second, 1, 1},
		{"zero burst", time.Second, 0, 1},
		{"negative burst", time.Second, -1, 1},
		{"zero capacity", time.Second, 1, 0},
		{"negative capacity", time.Second, 1, -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l, err := ratelimit.NewLimiter(tt.interval, tt.burst, tt.maxEntries)
			if err == nil || l != nil {
				t.Fatalf("NewLimiter = (%v, %v), want nil limiter and error", l, err)
			}
		})
	}

	t.Run("valid minimums", func(t *testing.T) {
		l := newTestLimiter(t, time.Nanosecond, 1, 1)
		assertAllow(t, l, "client", time.Now(), true, 0)
	})

}

func TestAllowBurstAndIndependentKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := newTestLimiter(t, 6*time.Second, 3, 10)
		now := time.Now()
		for range 3 {
			assertAllow(t, l, "first", now, true, 0)
		}
		assertAllow(t, l, "first", now, false, 6*time.Second)
		assertAllow(t, l, "second", now, true, 0)
		assertAllow(t, l, "first", now, false, 6*time.Second)
	})
}

func TestAllowFractionalRefillAndRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := newTestLimiter(t, 6*time.Second, 1, 10)
		start := time.Now()
		assertAllow(t, l, "client", start, true, 0)
		assertAllow(t, l, "client", start.Add(3*time.Second), false, 3*time.Second)
		assertAllow(t, l, "client", start.Add(4500*time.Millisecond), false, 1500*time.Millisecond)
		assertAllow(t, l, "client", start.Add(6*time.Second), true, 0)
		assertAllow(t, l, "client", start.Add(6*time.Second), false, 6*time.Second)
	})
}

func TestAllowRefillNeverExceedsBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := newTestLimiter(t, time.Second, 2, 10)
		start := time.Now()
		assertAllow(t, l, "client", start, true, 0)
		assertAllow(t, l, "client", start, true, 0)
		// Before the next cleanup: exercise refill capping on the existing entry.
		later := start.Add(30 * time.Second)
		assertAllow(t, l, "client", later, true, 0)
		assertAllow(t, l, "client", later, true, 0)
		assertAllow(t, l, "client", later, false, time.Second)
	})
}

func TestAllowNanosecondBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := newTestLimiter(t, 4*time.Nanosecond, 1, 1)
		start := time.Now()
		assertAllow(t, l, "client", start, true, 0)
		assertAllow(t, l, "client", start.Add(time.Nanosecond), false, 3*time.Nanosecond)
		assertAllow(t, l, "client", start.Add(3*time.Nanosecond), false, time.Nanosecond)
		assertAllow(t, l, "client", start.Add(4*time.Nanosecond), true, 0)
	})
}

func TestAllowCapacityKeepsExistingClients(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := newTestLimiter(t, time.Hour, 2, 1)
		start := time.Now()
		assertAllow(t, l, "existing", start, true, 0)
		assertAllow(t, l, "new", start.Add(time.Second), false, 59*time.Second)
		assertAllow(t, l, "existing", start.Add(time.Second), true, 0)
		assertAllow(t, l, "new", start.Add(time.Minute), false, time.Minute)
		allowed, retry := l.Allow("existing")
		if allowed || retry <= 0 {
			t.Fatalf("existing client lost its state: allowed=%v retry=%v", allowed, retry)
		}
	})
}

func TestAllowScheduledCleanupFreesCapacity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := newTestLimiter(t, time.Second, 1, 1)
		start := time.Now()
		assertAllow(t, l, "old", start, true, 0)
		// A full entry is retained until the scheduled cleanup; no extra scan.
		assertAllow(t, l, "new", start.Add(2*time.Second), false, 58*time.Second)
		assertAllow(t, l, "new", start.Add(time.Minute), true, 0)
		assertAllow(t, l, "old", start.Add(time.Minute), false, time.Minute)
	})
}

func TestCleanupOnlyRemovesFullyReplenishedEntries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := newTestLimiter(t, time.Minute, 2, 3)
		start := time.Now()
		assertAllow(t, l, "full", start, true, 0)
		assertAllow(t, l, "partial", start, true, 0)
		assertAllow(t, l, "partial", start, true, 0)
		assertAllow(t, l, "recent", start.Add(30*time.Second), true, 0)

		// Scheduled cleanup must free exactly the fully replenished entry's slot.
		assertAllow(t, l, "new", start.Add(time.Minute), true, 0)
		assertAllow(t, l, "another", start.Add(time.Minute), false, time.Minute)
		// Partial credit survives cleanup and pays for exactly one request.
		assertAllow(t, l, "partial", start.Add(time.Minute), true, 0)
		assertAllow(t, l, "partial", start.Add(time.Minute), false, time.Minute)
		assertAllow(t, l, "recent", start.Add(time.Minute), true, 0)
		assertAllow(t, l, "recent", start.Add(time.Minute), false, 30*time.Second)
	})
}

func TestAllowConcurrentBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const burst, requests = 7, 100
		l := newTestLimiter(t, time.Hour, burst, 10)
		start := make(chan struct{})
		results := make(chan bool, requests)
		var wg sync.WaitGroup
		for range requests {
			wg.Go(func() {
				<-start
				allowed, _ := l.Allow("shared")
				results <- allowed
			})
		}
		close(start)
		wg.Wait()
		close(results)
		accepted := 0
		for allowed := range results {
			if allowed {
				accepted++
			}
		}
		if accepted != burst {
			t.Fatalf("accepted %d concurrent requests, want %d", accepted, burst)
		}
	})
}

func newTestLimiter(t *testing.T, interval time.Duration, burst, maxEntries int) *ratelimit.Limiter {
	t.Helper()
	l, err := ratelimit.NewLimiter(interval, burst, maxEntries)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func assertAllow(t *testing.T, l *ratelimit.Limiter, key string, now time.Time, wantAllowed bool, wantRetry time.Duration) {
	t.Helper()
	// Inside synctest, Sleep advances virtual time without a real delay.
	if elapsed := time.Until(now); elapsed > 0 {
		time.Sleep(elapsed)
	}
	allowed, retry := l.Allow(key)
	if allowed != wantAllowed || retry != wantRetry {
		t.Fatalf("allow(%q, %v) = (%v, %v), want (%v, %v)", key, now, allowed, retry, wantAllowed, wantRetry)
	}
}
