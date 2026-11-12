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

type updateTaskRouteFunc func(context.Context, uuid.UUID, uuid.UUID, service.UpdateTaskInput) (*domain.Task, error)

func (f updateTaskRouteFunc) History(context.Context, uuid.UUID, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
	panic("unexpected History in update-task route test")
}

func (f updateTaskRouteFunc) Update(ctx context.Context, actor, task uuid.UUID, input service.UpdateTaskInput) (*domain.Task, error) {
	return f(ctx, actor, task, input)
}
func (f updateTaskRouteFunc) Create(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error) {
	panic("unexpected Create in update-task route test")
}
func (f updateTaskRouteFunc) List(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error) {
	panic("unexpected List in update-task route test")
}

func TestUpdateTaskRoute(t *testing.T) {
	const secret = "test-only-update-route-secret-32-bytes"
	actor, taskID := uuid.New(), uuid.New()
	for _, tt := range []struct {
		name          string
		status, calls int
	}{
		{"valid", 200, 1}, {"missing token", 401, 0}, {"malformed token", 401, 0},
		{"expired token", 401, 0}, {"wrong signature", 401, 0}, {"missing expiration", 401, 0}, {"wrong algorithm", 401, 0},
		{"invalid ID", 400, 0}, {"invalid assignee", 400, 0}, {"wrong method", 405, 0},
		{"forbidden", 403, 1}, {"not found", 404, 1}, {"conflict", 409, 1}, {"panic", 500, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&logs, nil))
			parser := service.NewAuthService(config.Auth{JWTSecret: secret}, nil, log)
			calls := 0
			s := updateTaskRouteFunc(func(ctx context.Context, id, tid uuid.UUID, input service.UpdateTaskInput) (*domain.Task, error) {
				calls++
				if id != actor || tid != taskID || input.Version != 4 || !input.AssigneeSet || input.AssigneeID != nil {
					t.Errorf("wrong identity/input: %s/%s/%+v", id, tid, input)
				}
				if got, ok := handler.UserIDFromContext(ctx); !ok || got != actor {
					t.Error("JWT identity missing from context")
				}
				switch tt.name {
				case "forbidden":
					return nil, fmt.Errorf("membership: %w", domain.ErrForbidden)
				case "not found":
					return nil, fmt.Errorf("lookup: %w", domain.ErrTaskNotFound)
				case "conflict":
					return nil, fmt.Errorf("update: %w", domain.ErrTaskVersionConflict)
				case "panic":
					panic("private update panic")
				}
				return &domain.Task{ID: taskID, CreatedBy: actor, Title: "Task", Status: domain.Todo, Version: 5}, nil
			})
			h := handler.New(&service.Service{Auth: parser}, log)
			h.Task = handler.NewTaskHandler(s, log)
			router := app.NewRouter(h, false, parser, log)
			method, path, body := http.MethodPut, "/api/v1/tasks/"+taskID.String(), `{"version":4,"assignee_id":null}`
			if tt.name == "invalid ID" {
				path = "/api/v1/tasks/invalid"
			}
			if tt.name == "invalid assignee" {
				body = `{"version":4,"assignee_id":"invalid"}`
			}
			if tt.name == "wrong method" {
				method = http.MethodPatch
			}
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			if tt.name != "missing token" {
				token := "invalid-token"
				if tt.name != "malformed token" {
					claims := jwt.MapClaims{"sub": actor.String(), "exp": time.Now().Add(time.Hour).Unix()}
					key, method := secret, jwt.SigningMethodHS256
					switch tt.name {
					case "expired token":
						claims["exp"] = time.Now().Add(-time.Hour).Unix()
					case "wrong signature":
						key = "other-secret"
					case "missing expiration":
						delete(claims, "exp")
					case "wrong algorithm":
						method = jwt.SigningMethodHS384
					}
					var err error
					token, err = jwt.NewWithClaims(method, claims).SignedString([]byte(key))
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
				t.Errorf("missing JSON type or request ID: %v", rec.Header())
			}
			if tt.status == 200 {
				var body struct {
					ID         uuid.UUID  `json:"id"`
					Version    uint32     `json:"version"`
					AssigneeID *uuid.UUID `json:"assignee_id"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.ID != taskID || body.Version != 5 || body.AssigneeID != nil {
					t.Errorf("unexpected task: %+v", body)
				}
			} else {
				var body map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
					t.Errorf("expected JSON error: %s", rec.Body.String())
				}
				if tt.status == 401 && rec.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Error("missing Bearer challenge")
				}
				if tt.name == "panic" && (strings.Contains(rec.Body.String(), "private update panic") || !strings.Contains(logs.String(), "private update panic")) {
					t.Error("panic must be logged and hidden from client")
				}
			}
		})
	}
}
