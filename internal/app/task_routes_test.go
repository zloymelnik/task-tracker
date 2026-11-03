package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/app"
	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type createTaskRouteFunc func(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error)

func (f createTaskRouteFunc) Create(ctx context.Context, actor uuid.UUID, input service.CreateTaskInput) (*domain.Task, error) {
	return f(ctx, actor, input)
}

func TestCreateTaskRoute(t *testing.T) {
	const secret = "test-only-task-route-secret-32-bytes"
	actor, team, taskID := uuid.New(), uuid.New(), uuid.New()
	for _, tt := range []struct {
		name          string
		status, calls int
	}{
		{"valid", 201, 1}, {"missing", 401, 0}, {"malformed", 401, 0},
		{"wrong signature", 401, 0}, {"expired", 401, 0},
		{"missing expiration", 401, 0}, {"wrong algorithm", 401, 0},
		{"wrong method", 405, 0}, {"panic", 500, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&logs, nil))
			parser := service.NewAuthService(config.Auth{JWTSecret: secret, JWTTtl: time.Minute}, nil, log)
			calls := 0
			s := createTaskRouteFunc(func(ctx context.Context, id uuid.UUID, input service.CreateTaskInput) (*domain.Task, error) {
				calls++
				if id != actor || input.TeamID != team || input.Title != "Task" {
					t.Errorf("actor=%s input=%+v: incorrect route input", id, input)
				}
				if got, ok := handler.UserIDFromContext(ctx); !ok || got != actor {
					t.Error("JWT identity missing from service context")
				}
				if tt.name == "panic" {
					panic("private task panic")
				}
				return &domain.Task{ID: taskID, TeamID: team, CreatedBy: id, Title: input.Title, Status: domain.Todo, Version: 1}, nil
			})
			h := handler.New(&service.Service{Auth: parser}, log)
			h.Task = handler.NewTaskHandler(s, log)
			router := app.NewRouter(h, parser, log)
			method := http.MethodPost
			if tt.name == "wrong method" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, "/api/v1/tasks", strings.NewReader(fmt.Sprintf(`{"team_id":%q,"title":"Task"}`, team)))
			if tt.name != "missing" {
				token := "invalid-token"
				if tt.name != "malformed" {
					claims := jwt.MapClaims{"sub": actor.String(), "exp": time.Now().Add(time.Hour).Unix()}
					key, signing := secret, jwt.SigningMethodHS256
					switch tt.name {
					case "wrong signature":
						key = "different-test-secret"
					case "expired":
						claims["exp"] = time.Now().Add(-time.Hour).Unix()
					case "missing expiration":
						delete(claims, "exp")
					case "wrong algorithm":
						signing = jwt.SigningMethodHS384
					}
					var err error
					token, err = jwt.NewWithClaims(signing, claims).SignedString([]byte(key))
					if err != nil {
						t.Fatal(err)
					}
				}
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.status || calls != tt.calls {
				t.Fatalf("status=%d calls=%d body=%s, want %d/%d", rec.Code, calls, rec.Body.String(), tt.status, tt.calls)
			}
			if tt.status == 405 {
				return
			}
			if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("X-Request-ID") == "" {
				t.Errorf("missing JSON content type or request ID: %v", rec.Header())
			}
			if tt.status == 201 {
				var body struct {
					ID     uuid.UUID `json:"id"`
					Author uuid.UUID `json:"created_by"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.ID != taskID || body.Author != actor {
					t.Errorf("unexpected task response: %+v", body)
				}
			} else {
				var body map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
					t.Errorf("expected JSON error, got %s", rec.Body.String())
				}
				if tt.status == 401 && rec.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Error("missing Bearer challenge")
				}
				if tt.name == "panic" && (strings.Contains(rec.Body.String(), "private task panic") || !strings.Contains(logs.String(), "private task panic")) {
					t.Error("panic diagnostic must be logged and hidden from client")
				}
			}
		})
	}
}
