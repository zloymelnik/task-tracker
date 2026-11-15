package handler_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zloymelnik/task-tracker/internal/handler"
)

type requestLimiterFunc func(string) (bool, time.Duration)

func (f requestLimiterFunc) Allow(key string) (bool, time.Duration) { return f(key) }

func TestRateLimitMiddlewareAllowed(t *testing.T) {
	for _, tt := range []struct{ addr, ip string }{
		{"192.0.2.10:1234", "192.0.2.10"},
		{"192.0.2.10:5678", "192.0.2.10"},
		{"[2001:db8::10]:1234", "2001:db8::10"},
	} {
		t.Run(tt.addr, func(t *testing.T) {
			limiterCalls, nextCalls := 0, 0
			limiter := requestLimiterFunc(func(key string) (bool, time.Duration) {
				limiterCalls++
				if key != tt.ip {
					t.Errorf("key=%q, want %q", key, tt.ip)
				}
				return true, 0
			})
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalls++
				w.Header().Set("X-Handler", "called")
				w.WriteHeader(http.StatusAccepted)
				if _, err := w.Write([]byte("accepted")); err != nil {
					t.Fatal(err)
				}
			})
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tt.addr
			req.Header.Set("X-Forwarded-For", "203.0.113.99")
			req.Header.Set("X-Real-IP", "203.0.113.99")
			rec := httptest.NewRecorder()
			handler.RateLimitMiddleware(limiter, silentLogger())(next).ServeHTTP(rec, req)
			if limiterCalls != 1 || nextCalls != 1 || rec.Code != http.StatusAccepted || rec.Body.String() != "accepted" {
				t.Fatalf("limiter=%d next=%d status=%d body=%q", limiterCalls, nextCalls, rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Retry-After") != "" || rec.Header().Get("X-Handler") != "called" {
				t.Errorf("unexpected headers: %v", rec.Header())
			}
		})
	}
}

func TestRateLimitMiddlewareRejected(t *testing.T) {
	for _, tt := range []struct {
		name  string
		retry time.Duration
		want  string
	}{
		{"zero", 0, "1"},
		{"negative", -time.Second, "1"},
		{"nanosecond", time.Nanosecond, "1"},
		{"whole second", time.Second, "1"},
		{"round up", time.Second + time.Nanosecond, "2"},
		{"fractional seconds", 1500 * time.Millisecond, "2"},
		{"whole minute", time.Minute, "60"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			limiter := requestLimiterFunc(func(key string) (bool, time.Duration) {
				calls++
				if key != "192.0.2.10" {
					t.Errorf("key=%q", key)
				}
				return false, tt.retry
			})
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("rejected request reached handler") })
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("invalid JSON"))
			req.RemoteAddr = "192.0.2.10:1234"
			rec := httptest.NewRecorder()
			handler.RateLimitMiddleware(limiter, silentLogger())(next).ServeHTTP(rec, req)
			assertErrorResponse(t, rec, http.StatusTooManyRequests)
			if calls != 1 || rec.Header().Get("Retry-After") != tt.want {
				t.Errorf("calls=%d retry=%q want=%q", calls, rec.Header().Get("Retry-After"), tt.want)
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error != "too many requests" {
				t.Errorf("error=%q", body.Error)
			}
		})
	}
}

func TestRateLimitMiddlewareInvalidRemoteAddr(t *testing.T) {
	for _, addr := range []string{"", "192.0.2.10", "2001:db8::10", "[2001:db8::10]", "invalid"} {
		t.Run(addr, func(t *testing.T) {
			limiter := requestLimiterFunc(func(string) (bool, time.Duration) { t.Fatal("invalid address reached limiter"); return true, 0 })
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid address reached handler") })
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = addr
			rec := httptest.NewRecorder()
			handler.RateLimitMiddleware(limiter, silentLogger())(next).ServeHTTP(rec, req)
			assertErrorResponse(t, rec, http.StatusBadRequest)
			if rec.Header().Get("Retry-After") != "" {
				t.Error("invalid address response contains Retry-After")
			}
		})
	}
}

func TestRateLimitMiddlewareWriteError(t *testing.T) {
	for _, addr := range []string{"192.0.2.10:1234", "invalid"} {
		t.Run(addr, func(t *testing.T) {
			limiter := requestLimiterFunc(func(string) (bool, time.Duration) { return false, time.Second })
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("rejected request reached handler") })
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = addr
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			w := &failingAuthResponseWriter{header: make(http.Header), err: errors.New("connection closed")}
			handler.RateLimitMiddleware(limiter, log)(next).ServeHTTP(w, req)
			wantStatus := http.StatusTooManyRequests
			if addr == "invalid" {
				wantStatus = http.StatusBadRequest
			}
			if w.status != wantStatus || w.writes != 1 {
				t.Errorf("status=%d writes=%d", w.status, w.writes)
			}
			if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), w.err.Error()) {
				t.Errorf("expected one write error log, got %s", logs.String())
			}
		})
	}
}
