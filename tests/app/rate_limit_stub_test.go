package app_test

import "time"

// allowAllRateLimiter keeps unrelated route tests independent of rate limits.
type allowAllRateLimiter struct{}

func (allowAllRateLimiter) Allow(string) (bool, time.Duration) { return true, 0 }
