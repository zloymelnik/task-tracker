package handler_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/zloymelnik/task-tracker/internal/handler"
)

func TestRecoveryPanic(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("private panic diagnostic")
	})
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("X-Request-ID", "recovery-test")
	rec := httptest.NewRecorder()
	middleware.RequestID(handler.Recovery(log)(next)).ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusInternalServerError)
	if strings.Contains(rec.Body.String(), "private panic diagnostic") {
		t.Error("panic diagnostic leaked into response")
	}
	for _, want := range []string{"level=ERROR", "private panic diagnostic", "recovery-test", "stack="} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("panic log missing %q", want)
		}
	}
}

func TestRecoveryNormalResponse(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	handler.Recovery(log)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNoContent || logs.Len() != 0 {
		t.Errorf("status=%d logs=%q, want 204 and no logs", rec.Code, logs.String())
	}
}

func TestRecoveryAbort(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "explicit abort"
		if partial {
			name = "panic after response started"
		}
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if partial {
					w.WriteHeader(http.StatusAccepted)
					panic("partial response panic")
				}
				panic(http.ErrAbortHandler)
			})
			rec := httptest.NewRecorder()
			defer func() {
				if got := recover(); got != http.ErrAbortHandler {
					t.Errorf("panic=%v, want http.ErrAbortHandler", got)
				}
				if rec.Body.Len() != 0 {
					t.Error("recovery appended a response body after abort")
				}
				if partial {
					if rec.Code != http.StatusAccepted || strings.Count(logs.String(), "level=ERROR") != 1 {
						t.Errorf("status=%d logs=%q, want 202 and one panic log", rec.Code, logs.String())
					}
				} else if logs.Len() != 0 {
					t.Error("http.ErrAbortHandler must not be logged")
				}
			}()
			handler.Recovery(log)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		})
	}
}

func TestRecoveryWriteError(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("handler panic") })
	w := &failingAuthResponseWriter{header: make(http.Header), err: http.ErrHandlerTimeout}
	handler.Recovery(log)(next).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.status != http.StatusInternalServerError || w.writes != 1 {
		t.Errorf("status=%d writes=%d, want 500 and one write", w.status, w.writes)
	}
	if strings.Count(logs.String(), "level=ERROR") != 2 || !strings.Contains(logs.String(), w.err.Error()) {
		t.Errorf("want panic and write error logs, got %s", logs.String())
	}
}
