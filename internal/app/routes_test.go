package app_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zloymelnik/task-tracker/internal/app"
	"github.com/zloymelnik/task-tracker/internal/handler"
)

func TestHealthRout(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := app.NewRouter(handler.New(), log)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Body.String(); got != "OK" {
		t.Errorf("body = %q, want %q", got, "OK")
	}
}
