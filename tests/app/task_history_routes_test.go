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

type taskHistoryRouteFunc func(context.Context, uuid.UUID, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error)

func (f taskHistoryRouteFunc) History(ctx context.Context, actor uuid.UUID, filter domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
	return f(ctx, actor, filter)
}
func (f taskHistoryRouteFunc) Create(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error) {
	panic("unexpected Create in history route test")
}
func (f taskHistoryRouteFunc) List(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error) {
	panic("unexpected List in history route test")
}
func (f taskHistoryRouteFunc) Update(context.Context, uuid.UUID, uuid.UUID, service.UpdateTaskInput) (*domain.Task, error) {
	panic("unexpected Update in history route test")
}

func TestTaskHistoryRoute(t *testing.T) {
	const secret = "test-only-history-route-secret-32-bytes"
	actor, task := uuid.New(), uuid.New()
	for _, tt := range []struct {
		name          string
		status, calls int
	}{
		{"valid", 200, 1}, {"missing token", 401, 0}, {"malformed token", 401, 0},
		{"expired token", 401, 0}, {"wrong signature", 401, 0}, {"missing expiration", 401, 0}, {"wrong algorithm", 401, 0},
		{"invalid path", 400, 0}, {"invalid query", 400, 0}, {"wrong method", 405, 0},
		{"forbidden", 403, 1}, {"not found", 404, 1}, {"panic", 500, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&logs, nil))
			parser := service.NewAuthService(config.Auth{JWTSecret: secret}, nil, log)
			calls := 0
			s := taskHistoryRouteFunc(func(ctx context.Context, id uuid.UUID, filter domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
				calls++
				if id != actor || filter.TaskID == nil || *filter.TaskID != task || filter.Limit != 5 || filter.Offset != 10 {
					t.Errorf("wrong identity/filter: %s/%+v", id, filter)
				}
				if got, ok := handler.UserIDFromContext(ctx); !ok || got != actor {
					t.Error("JWT identity missing from context")
				}
				switch tt.name {
				case "forbidden":
					return nil, 0, fmt.Errorf("membership: %w", domain.ErrForbidden)
				case "not found":
					return nil, 0, fmt.Errorf("get: %w", domain.ErrTaskNotFound)
				case "panic":
					panic("private history panic")
				}
				return nil, 42, nil
			})
			h := handler.New(&service.Service{Auth: parser}, log)
			h.Task = handler.NewTaskHandler(s, log)
			router := app.NewRouter(h, false, parser, log)
			method, id, query := http.MethodGet, task.String(), "limit=5&offset=10&id="+uuid.NewString()
			if tt.name == "invalid path" {
				id = "invalid"
			}
			if tt.name == "invalid query" {
				query = "limit=invalid"
			}
			if tt.name == "wrong method" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, "/api/v1/tasks/"+id+"/history?"+query, nil)
			if tt.name != "missing token" {
				token := "invalid-token"
				if tt.name != "malformed token" {
					claims := jwt.MapClaims{"sub": actor.String(), "exp": time.Now().Add(time.Hour).Unix()}
					key, signing := secret, jwt.SigningMethodHS256
					switch tt.name {
					case "expired token":
						claims["exp"] = time.Now().Add(-time.Hour).Unix()
					case "wrong signature":
						key = "different-secret"
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
				t.Errorf("missing JSON type or request ID: %v", rec.Header())
			}
			if tt.status == 200 {
				var page struct {
					Items  []json.RawMessage `json:"items"`
					Total  int               `json:"total"`
					Limit  int               `json:"limit"`
					Offset int               `json:"offset"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				if page.Items == nil || len(page.Items) != 0 || page.Total != 42 || page.Limit != 5 || page.Offset != 10 {
					t.Errorf("unexpected page: %s", rec.Body.String())
				}
			} else {
				var body map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
					t.Errorf("expected JSON error: %s", rec.Body.String())
				}
				if tt.status == 401 && rec.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Error("missing Bearer challenge")
				}
				if tt.name == "panic" && (strings.Contains(rec.Body.String(), "private history panic") || !strings.Contains(logs.String(), "private history panic")) {
					t.Error("panic must be logged and hidden from client")
				}
			}
		})
	}
}
