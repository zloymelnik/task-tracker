package app_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/app"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/ratelimit"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type routeLimiterFunc func(string) (bool, time.Duration)

func (f routeLimiterFunc) Allow(key string) (bool, time.Duration) { return f(key) }

type rateLimitTokenParserFunc func(string) (uuid.UUID, error)

func (f rateLimitTokenParserFunc) ParseToken(token string) (uuid.UUID, error) { return f(token) }

func TestRateLimitRouteOrder(t *testing.T) {
	for _, tt := range []struct {
		name, method, path, deny          string
		status, registerCalls, loginCalls int
		stages                            []string
	}{
		{"health allowed", "GET", "/api/v1/health", "", 200, 0, 0, []string{"api"}},
		{"register allowed", "POST", "/api/v1/register", "", 201, 1, 0, []string{"api", "register"}},
		{"login allowed", "POST", "/api/v1/login", "", 200, 0, 1, []string{"api", "login"}},
		{"private reaches JWT", "GET", "/api/v1/teams", "", 401, 0, 0, []string{"api", "jwt"}},
		{"health denied", "GET", "/api/v1/health", "api", 429, 0, 0, []string{"api"}},
		{"register globally denied", "POST", "/api/v1/register", "api", 429, 0, 0, []string{"api"}},
		{"login globally denied", "POST", "/api/v1/login", "api", 429, 0, 0, []string{"api"}},
		{"private globally denied", "GET", "/api/v1/teams", "api", 429, 0, 0, []string{"api"}},
		{"register locally denied", "POST", "/api/v1/register", "register", 429, 0, 0, []string{"api", "register"}},
		{"login locally denied", "POST", "/api/v1/login", "login", 429, 0, 0, []string{"api", "login"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stages []string
			makeLimiter := func(name string) handler.RequestLimiter {
				return routeLimiterFunc(func(key string) (bool, time.Duration) {
					stages = append(stages, name)
					if key != "192.0.2.10" {
						t.Errorf("%s key=%q", name, key)
					}
					if tt.deny == name {
						return false, 1500 * time.Millisecond
					}
					return true, 0
				})
			}
			parser := rateLimitTokenParserFunc(func(string) (uuid.UUID, error) {
				stages = append(stages, "jwt")
				return uuid.Nil, errors.New("test invalid token")
			})
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			auth := &authRoutesStub{}
			h := handler.New(&service.Service{}, log)
			h.Auth = handler.NewAuthHandler(auth, log)
			router := app.NewRouter(h, false, parser, makeLimiter("api"), makeLimiter("login"), makeLimiter("register"), log)
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{}`))
			req.RemoteAddr = "192.0.2.10:1234"
			req.Header.Set("Authorization", "Bearer test-token")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.status || !slices.Equal(stages, tt.stages) {
				t.Fatalf("status=%d stages=%v want=%d/%v body=%s", rec.Code, stages, tt.status, tt.stages, rec.Body.String())
			}
			if auth.registerCalls != tt.registerCalls || auth.loginCalls != tt.loginCalls {
				t.Errorf("register/login calls=%d/%d want=%d/%d", auth.registerCalls, auth.loginCalls, tt.registerCalls, tt.loginCalls)
			}
			if tt.status == http.StatusTooManyRequests {
				if rec.Header().Get("Retry-After") != "2" || rec.Body.String() != `{"error":"too many requests"}` {
					t.Errorf("unexpected rate limit response: headers=%v body=%s", rec.Header(), rec.Body.String())
				}
			} else if rec.Header().Get("Retry-After") != "" {
				t.Error("allowed response contains Retry-After")
			}
			if rec.Header().Get("X-Request-ID") == "" {
				t.Error("rate limiting must retain request ID middleware")
			}
		})
	}
}

func TestDocumentationBypassesRateLimits(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.New(&service.Service{}, log)
	limiter := routeLimiterFunc(func(string) (bool, time.Duration) {
		t.Fatal("documentation reached API limiter")
		return false, time.Minute
	})
	router := app.NewRouter(h, true, nil, limiter, limiter, limiter, log)
	for _, path := range []string{"/openapi.json", "/swagger/index.html", "/swagger/swagger-ui.css"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Errorf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRoutesWithRealRateLimiters(t *testing.T) {
	for _, tt := range []struct {
		name     string
		apiBurst int
		steps    []struct {
			method, path string
			status       int
		}
	}{
		{"auth buckets independent", 20, []struct {
			method, path string
			status       int
		}{
			{"POST", "/api/v1/login", 200},
			{"POST", "/api/v1/login", 429},
			{"POST", "/api/v1/register", 201},
			{"POST", "/api/v1/register", 429},
			{"GET", "/api/v1/health", 200},
		}},
		{"shared API bucket", 1, []struct {
			method, path string
			status       int
		}{
			{"POST", "/api/v1/login", 200},
			{"POST", "/api/v1/register", 429},
			{"GET", "/api/v1/health", 429},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				newLimiter := func(burst int) *ratelimit.Limiter {
					l, err := ratelimit.NewLimiter(time.Hour, burst, 10)
					if err != nil {
						t.Fatal(err)
					}
					return l
				}
				log := slog.New(slog.NewTextHandler(io.Discard, nil))
				h := handler.New(&service.Service{}, log)
				h.Auth = handler.NewAuthHandler(&authRoutesStub{}, log)
				router := app.NewRouter(h, false, nil, newLimiter(tt.apiBurst), newLimiter(1), newLimiter(1), log)
				for i, step := range tt.steps {
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, httptest.NewRequest(step.method, step.path, strings.NewReader(`{}`)))
					if rec.Code != step.status {
						t.Fatalf("step %d %s: status=%d want=%d body=%s", i, step.path, rec.Code, step.status, rec.Body.String())
					}
				}
			})
		})
	}
}
