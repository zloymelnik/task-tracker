package handler

import (
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"
)

type RequestLimiter interface {
	Allow(key string) (bool, time.Duration)
}

func RateLimitMiddleware(limiter RequestLimiter, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				if err := writeErrorResponse(w, http.StatusBadRequest, "wrong ip"); err != nil {
					log.Error("Failed to write RateLimitMiddleware response", "error", err)
				}
				return
			}
			allow, retryAfter := limiter.Allow(ip)
			if allow {
				next.ServeHTTP(w, r)
				return
			}
			seconds := max(1, int(math.Ceil(retryAfter.Seconds())))
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			if err := writeErrorResponse(w, http.StatusTooManyRequests, "too many requests"); err != nil {
				log.Error("Failed to write RateLimitMiddleware response", "error", err)
			}
			return
		})
	}
}
