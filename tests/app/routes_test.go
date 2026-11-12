package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/app"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
)

func TestHealthRout(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.New(&service.Service{}, log)
	r := app.NewRouter(h, false, nil, log)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
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

func (s *authRoutesStub) Register(context.Context, service.RegisterInput) (*domain.User, error) {
	s.registerCalls++
	return &domain.User{}, nil
}

func (s *authRoutesStub) Login(context.Context, service.LoginInput) (string, error) {
	s.loginCalls++
	return "test-token", nil
}

func TestAuthRoutes(t *testing.T) {
	for _, tt := range []struct {
		method, path                      string
		status, registerCalls, loginCalls int
	}{
		{http.MethodPost, "/api/v1/register", 201, 1, 0},
		{http.MethodPost, "/api/v1/login", 200, 0, 1},
		{http.MethodGet, "/api/v1/register", 405, 0, 0},
		{http.MethodGet, "/api/v1/login", 405, 0, 0},
	} {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			stub := &authRoutesStub{}
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			teamStub := createTeamServiceFunc(func(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error) {
				t.Fatal("public auth request must not reach team service")
				return nil, nil
			})
			h := &handler.Handler{
				Auth: handler.NewAuthHandler(stub, log),
				Team: handler.NewTeamHandler(teamStub, log),
			}
			router := app.NewRouter(h, false, nil, log)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{}`)))
			if rec.Code != tt.status || stub.registerCalls != tt.registerCalls || stub.loginCalls != tt.loginCalls {
				t.Errorf("status=%d register calls=%d login calls=%d; want %d/%d/%d",
					rec.Code, stub.registerCalls, stub.loginCalls, tt.status, tt.registerCalls, tt.loginCalls)
			}
		})
	}
}
