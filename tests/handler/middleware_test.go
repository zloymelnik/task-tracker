package handler_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/handler"
)

type tokenParserFunc func(string) (uuid.UUID, error)

func (f tokenParserFunc) ParseToken(token string) (uuid.UUID, error) {
	return f(token)
}

func TestAuthMiddlewareInvalidHeader(t *testing.T) {
	for _, header := range []string{"", "Bearer", "Bearer ", "Basic token", "token", "Bearer token extra"} {
		t.Run(header, func(t *testing.T) {
			parser := tokenParserFunc(func(string) (uuid.UUID, error) {
				t.Fatal("invalid header must not reach token parser")
				return uuid.Nil, nil
			})
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("unauthorized request must not reach next handler")
			})
			req := httptest.NewRequest(http.MethodGet, "/private", nil)
			req.Header.Set("Authorization", header)
			rec := httptest.NewRecorder()
			handler.AuthMiddleware(parser, silentLogger())(next).ServeHTTP(rec, req)
			assertErrorResponse(t, rec, http.StatusUnauthorized)
			if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Errorf("WWW-Authenticate=%q, want Bearer", got)
			}
		})
	}
}

func TestAuthMiddlewareInvalidToken(t *testing.T) {
	calls := 0
	parser := tokenParserFunc(func(token string) (uuid.UUID, error) {
		calls++
		if token != "invalid-token" {
			t.Errorf("parser received %q, want token without scheme", token)
		}
		return uuid.Nil, errors.New("private token diagnostic")
	})
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid token must not reach next handler")
	})
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()
	handler.AuthMiddleware(parser, silentLogger())(next).ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusUnauthorized)
	if calls != 1 || rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("parser calls=%d challenge=%q, want 1 and Bearer", calls, rec.Header().Get("WWW-Authenticate"))
	}
	if strings.Contains(rec.Body.String(), "private token diagnostic") {
		t.Error("token diagnostic leaked into response")
	}
}

func TestAuthMiddlewareSuccess(t *testing.T) {
	for _, scheme := range []string{"Bearer", "bearer", "bEaReR"} {
		t.Run(scheme, func(t *testing.T) {
			id := uuid.New()
			parserCalls, nextCalls := 0, 0
			parser := tokenParserFunc(func(token string) (uuid.UUID, error) {
				parserCalls++
				if token != "valid-token" {
					t.Errorf("parser received %q, want valid-token", token)
				}
				return id, nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalls++
				got, ok := handler.UserIDFromContext(r.Context())
				if !ok || got != id {
					t.Errorf("user ID=(%s, %v), want (%s, true)", got, ok, id)
				}
				// The derived context must retain cancellation from the request.
				cancel()
				if !errors.Is(r.Context().Err(), context.Canceled) {
					t.Error("request context cancellation was lost")
				}
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/private", nil).WithContext(ctx)
			req.Header.Set("Authorization", scheme+" valid-token")
			rec := httptest.NewRecorder()
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			handler.AuthMiddleware(parser, log)(next).ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent || parserCalls != 1 || nextCalls != 1 {
				t.Errorf("status=%d parser calls=%d next calls=%d, want 204/1/1", rec.Code, parserCalls, nextCalls)
			}
			if rec.Header().Get("WWW-Authenticate") != "" {
				t.Error("successful response contains authentication challenge")
			}
			if strings.Contains(logs.String(), "valid-token") {
				t.Error("token leaked into logs")
			}
			if _, ok := handler.UserIDFromContext(req.Context()); ok {
				t.Error("middleware mutated original request context")
			}
		})
	}
}

type failingAuthResponseWriter struct {
	header http.Header
	status int
	writes int
	err    error
}

func (w *failingAuthResponseWriter) Header() http.Header { return w.header }

func (w *failingAuthResponseWriter) WriteHeader(status int) { w.status = status }

func (w *failingAuthResponseWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, w.err
}

func TestAuthMiddlewareResponseWriteError(t *testing.T) {
	for _, invalidToken := range []bool{false, true} {
		name := "invalid header"
		if invalidToken {
			name = "invalid token"
		}
		t.Run(name, func(t *testing.T) {
			parserCalls := 0
			parser := tokenParserFunc(func(string) (uuid.UUID, error) {
				parserCalls++
				return uuid.Nil, errors.New("invalid token")
			})
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("unauthorized request must not reach next handler")
			})
			req := httptest.NewRequest(http.MethodGet, "/private", nil)
			if invalidToken {
				req.Header.Set("Authorization", "Bearer rejected-token")
			}
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			w := &failingAuthResponseWriter{header: make(http.Header), err: errors.New("connection closed")}
			handler.AuthMiddleware(parser, log)(next).ServeHTTP(w, req)
			wantCalls := 0
			if invalidToken {
				wantCalls = 1
			}
			if parserCalls != wantCalls || w.status != http.StatusUnauthorized || w.writes != 1 {
				t.Errorf("parser calls=%d status=%d writes=%d, want %d/401/1", parserCalls, w.status, w.writes, wantCalls)
			}
			if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), w.err.Error()) {
				t.Errorf("want one error log with write diagnostic, got %s", logs.String())
			}
			if strings.Contains(logs.String(), "rejected-token") {
				t.Error("token leaked into logs")
			}
		})
	}
}

func TestUserIDFromContextMissing(t *testing.T) {
	id, ok := handler.UserIDFromContext(context.Background())
	if ok || id != uuid.Nil {
		t.Errorf("UserIDFromContext()=(%s, %v), want (nil UUID, false)", id, ok)
	}
}
