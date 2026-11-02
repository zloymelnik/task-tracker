package app_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zloymelnik/task-tracker/internal/app"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
)

func TestHealthRout(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.New(&service.Service{}, log)
	r := app.NewRouter(h, nil, log)
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

type authRoutesStub struct {
	registerCalls int
	loginCalls    int
}

func (s *authRoutesStub) Register(w http.ResponseWriter, r *http.Request) {
	s.registerCalls++
	w.WriteHeader(http.StatusCreated)
}

func (s *authRoutesStub) Login(w http.ResponseWriter, r *http.Request) {
	s.loginCalls++
	w.WriteHeader(http.StatusOK)
}

func TestAuthRoutes(t *testing.T) {
	for _, tt := range []struct {
		method, path                      string
		status, registerCalls, loginCalls int
	}{
		{http.MethodPost, "/auth/register", 201, 1, 0},
		{http.MethodPost, "/auth/login", 200, 0, 1},
		{http.MethodGet, "/auth/register", 405, 0, 0},
		{http.MethodGet, "/auth/login", 405, 0, 0},
	} {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			stub := &authRoutesStub{}
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			router := app.NewRouter(&handler.Handler{Auth: stub}, nil, log)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.status || stub.registerCalls != tt.registerCalls || stub.loginCalls != tt.loginCalls {
				t.Errorf("status=%d register calls=%d login calls=%d; want %d/%d/%d",
					rec.Code, stub.registerCalls, stub.loginCalls, tt.status, tt.registerCalls, tt.loginCalls)
			}
		})
	}
}
