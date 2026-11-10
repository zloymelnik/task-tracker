package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type authServiceFunc func(context.Context, service.RegisterInput) (*domain.User, error)

func (f authServiceFunc) Register(ctx context.Context, input service.RegisterInput) (*domain.User, error) {
	return f(ctx, input)
}

func (f authServiceFunc) Login(context.Context, service.LoginInput) (string, error) {
	panic("unexpected Login call in registration service stub")
}

const registerJSON = `{"email":"user@example.com","name":"User","password":"secret-password"}`

func TestRegisterSuccess(t *testing.T) {
	want := domain.User{
		ID: uuid.New(), Email: "user@example.com", Name: "User", PasswordHash: "private-hash",
		CreatedAt: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	s := authServiceFunc(func(gotCtx context.Context, input service.RegisterInput) (*domain.User, error) {
		calls++
		if gotCtx != ctx {
			t.Error("service received a different context")
		}
		if input != (service.RegisterInput{Email: want.Email, Name: want.Name, Password: "secret-password"}) {
			t.Errorf("unexpected service input: %+v", input)
		}
		return &want, nil
	})
	h := handler.NewAuthHandler(s, silentLogger())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(registerJSON+" \n\t")).WithContext(ctx)
	h.Register(rec, req)
	if rec.Code != http.StatusCreated || calls != 1 {
		t.Fatalf("status=%d calls=%d, want 201 and one service call", rec.Code, calls)
	}
	assertJSONContentType(t, rec)
	var got handler.UserResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Email != want.Email || got.Name != want.Name || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("response=%+v, want public fields of %+v", got, want)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 || fields["password"] != nil || fields["password_hash"] != nil ||
		strings.Contains(rec.Body.String(), want.PasswordHash) || strings.Contains(rec.Body.String(), "secret-password") {
		t.Errorf("response contains unexpected or secret fields: %s", rec.Body.String())
	}
}

func TestRegisterInvalidJSON(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
	}{
		{"empty", "", 400},
		{"malformed", `{"email":`, 400},
		{"null", "null", 400},
		{"array", "[]", 400},
		{"wrong field type", `{"email":123}`, 400},
		{"trailing garbage", registerJSON + " garbage", 400},
		{"second object", registerJSON + " {}", 400},
		{"second null", registerJSON + " null", 400},
		{"oversized object", `{"name":"` + strings.Repeat("a", 64<<10) + `"}`, 413},
		{"oversized trailing whitespace", registerJSON + strings.Repeat(" ", 64<<10), 413},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := authServiceFunc(func(context.Context, service.RegisterInput) (*domain.User, error) {
				t.Fatal("invalid JSON must not reach the service")
				return nil, nil
			})
			rec := httptest.NewRecorder()
			handler.NewAuthHandler(s, silentLogger()).Register(rec,
				httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(tt.body)))
			assertErrorResponse(t, rec, tt.status)
		})
	}
}

func TestRegisterServiceErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{"invalid input", fmt.Errorf("register: %w", domain.ErrInvalidInput), 400},
		{"duplicate email", fmt.Errorf("register: %w", domain.ErrEmailAlreadyExists), 409},
		{"storage failure", errors.New("private database diagnostic"), 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			calls := 0
			s := authServiceFunc(func(context.Context, service.RegisterInput) (*domain.User, error) {
				calls++
				return nil, tt.err
			})
			rec := httptest.NewRecorder()
			handler.NewAuthHandler(s, log).Register(rec,
				httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(registerJSON)))
			assertErrorResponse(t, rec, tt.status)
			if calls != 1 {
				t.Errorf("service calls=%d, want 1", calls)
			}
			if tt.status == 500 {
				if strings.Contains(rec.Body.String(), tt.err.Error()) {
					t.Error("internal error leaked into response")
				}
				if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), tt.err.Error()) {
					t.Errorf("want one error log with original diagnostic, got %s", logs.String())
				}
			}
		})
	}
}

func TestRegisterMarshalFailure(t *testing.T) {
	s := authServiceFunc(func(context.Context, service.RegisterInput) (*domain.User, error) {
		return &domain.User{ID: uuid.New(), CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
	})
	rec := httptest.NewRecorder()
	handler.NewAuthHandler(s, silentLogger()).Register(rec,
		httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(registerJSON)))
	assertErrorResponse(t, rec, http.StatusInternalServerError)
}

type loginServiceFunc func(context.Context, service.LoginInput) (string, error)

func (f loginServiceFunc) Register(context.Context, service.RegisterInput) (*domain.User, error) {
	panic("unexpected Register call in login service stub")
}

func (f loginServiceFunc) Login(ctx context.Context, input service.LoginInput) (string, error) {
	return f(ctx, input)
}

const loginJSON = `{"email":"user@example.com","password":"secret-password"}`

func TestLoginSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const token = "test-access-token"
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	calls := 0
	s := loginServiceFunc(func(gotCtx context.Context, input service.LoginInput) (string, error) {
		calls++
		if gotCtx != ctx {
			t.Error("service received a different context")
		}
		if input != (service.LoginInput{Email: "user@example.com", Password: "secret-password"}) {
			t.Error("service received unexpected login input")
		}
		return token, nil
	})
	rec := httptest.NewRecorder()
	handler.NewAuthHandler(s, log).Login(rec,
		httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(loginJSON+" \n\t")).WithContext(ctx))
	if rec.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status=%d calls=%d, want 200 and one service call", rec.Code, calls)
	}
	assertJSONContentType(t, rec)
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["token"] != token {
		t.Errorf("unexpected login response: %v", body)
	}
	if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), "secret-password") {
		t.Error("token or password leaked into logs")
	}
}

func TestLoginInvalidJSON(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
	}{
		{"empty", "", 400},
		{"malformed", `{"email":`, 400},
		{"null", "null", 400},
		{"array", "[]", 400},
		{"wrong field type", `{"password":123}`, 400},
		{"trailing garbage", loginJSON + " garbage", 400},
		{"second object", loginJSON + " {}", 400},
		{"second null", loginJSON + " null", 400},
		{"oversized object", `{"password":"` + strings.Repeat("a", 64<<10) + `"}`, 413},
		{"oversized trailing whitespace", loginJSON + strings.Repeat(" ", 64<<10), 413},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := loginServiceFunc(func(context.Context, service.LoginInput) (string, error) {
				t.Fatal("invalid JSON must not reach the service")
				return "", nil
			})
			rec := httptest.NewRecorder()
			handler.NewAuthHandler(s, silentLogger()).Login(rec,
				httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(tt.body)))
			assertErrorResponse(t, rec, tt.status)
		})
	}
}

func TestLoginServiceErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"invalid input", fmt.Errorf("login: %w", domain.ErrInvalidInput), 400, "invalid input"},
		{"invalid credentials", fmt.Errorf("login: %w", domain.ErrInvalidCredentials), 401, "invalid credentials"},
		{"storage failure", errors.New("private database diagnostic"), 500, "internal server error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			calls := 0
			s := loginServiceFunc(func(context.Context, service.LoginInput) (string, error) {
				calls++
				return "", tt.err
			})
			rec := httptest.NewRecorder()
			handler.NewAuthHandler(s, log).Login(rec,
				httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(loginJSON)))
			assertErrorResponse(t, rec, tt.status)
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["error"] != tt.message || calls != 1 {
				t.Errorf("message=%q calls=%d, want %q and 1", body["error"], calls, tt.message)
			}
			if tt.status == 500 {
				if strings.Contains(rec.Body.String(), tt.err.Error()) {
					t.Error("internal error leaked into response")
				}
				if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), tt.err.Error()) {
					t.Errorf("want one error log with original diagnostic, got %s", logs.String())
				}
			}
		})
	}
}

func assertJSONContentType(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type=%q, want application/json", got)
	}
}

func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status=%d, want %d; body=%s", rec.Code, status, rec.Body.String())
	}
	assertJSONContentType(t, rec)
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid error JSON: %v", err)
	}
	if len(body) != 1 || body["error"] == "" {
		t.Errorf("unexpected error response: %v", body)
	}
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
