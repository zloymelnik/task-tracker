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

type commentRoutesStub struct {
	createTaskRouteFunc
	add  func(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error)
	list func(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error)
}

func (s commentRoutesStub) AddComment(ctx context.Context, actor, task uuid.UUID, input service.CreateTaskCommentInput) (*domain.TaskComment, error) {
	return s.add(ctx, actor, task, input)
}
func (s commentRoutesStub) Comments(ctx context.Context, actor uuid.UUID, f domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	return s.list(ctx, actor, f)
}

func TestCommentRoutes(t *testing.T) {
	const secret = "test-only-comment-routes-secret-32-bytes"
	actor, task := uuid.New(), uuid.New()
	want := domain.TaskComment{ID: uuid.New(), TaskID: task, CreatedBy: actor, Body: "Комментарий", CreatedAt: time.Date(2026, 11, 5, 12, 0, 0, 123000000, time.UTC)}
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		for _, tt := range []struct {
			name          string
			status, calls int
		}{
			{"valid", 200, 1}, {"missing token", 401, 0}, {"malformed token", 401, 0}, {"expired token", 401, 0}, {"wrong signature", 401, 0}, {"missing expiration", 401, 0}, {"wrong algorithm", 401, 0},
			{"invalid ID", 400, 0}, {"invalid request", 400, 0}, {"wrong method", 405, 0}, {"invalid input", 400, 1}, {"forbidden", 403, 1}, {"not found", 404, 1}, {"panic", 500, 1},
		} {
			t.Run(method+"/"+tt.name, func(t *testing.T) {
				var logs bytes.Buffer
				log := slog.New(slog.NewJSONHandler(&logs, nil))
				parser := service.NewAuthService(config.Auth{JWTSecret: secret}, nil, log)
				addCalls, listCalls := 0, 0
				checkActor := func(ctx context.Context, id uuid.UUID) {
					if id != actor {
						t.Errorf("actor=%s, want %s", id, actor)
					}
					if user, ok := handler.UserIDFromContext(ctx); !ok || user != actor {
						t.Error("JWT identity missing from context")
					}
				}
				serviceError := func() error {
					switch tt.name {
					case "invalid input":
						return fmt.Errorf("comment: %w", domain.ErrInvalidInput)
					case "forbidden":
						return fmt.Errorf("membership: %w", domain.ErrForbidden)
					case "not found":
						return fmt.Errorf("task: %w", domain.ErrTaskNotFound)
					case "panic":
						panic("private comment panic")
					}
					return nil
				}
				s := commentRoutesStub{
					add: func(ctx context.Context, id, tid uuid.UUID, input service.CreateTaskCommentInput) (*domain.TaskComment, error) {
						addCalls++
						checkActor(ctx, id)
						if tid != task || input.Body != want.Body {
							t.Errorf("wrong task/input: %s/%+v", tid, input)
						}
						if err := serviceError(); err != nil {
							return nil, err
						}
						return &want, nil
					},
					list: func(ctx context.Context, id uuid.UUID, f domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
						listCalls++
						checkActor(ctx, id)
						if f.TaskID == nil || *f.TaskID != task || f.Limit != 5 || f.Offset != 10 {
							t.Errorf("wrong filter: %+v", f)
						}
						if err := serviceError(); err != nil {
							return nil, 0, err
						}
						return []domain.TaskComment{want}, 42, nil
					},
				}
				h := handler.New(&service.Service{Auth: parser}, log)
				h.Task = handler.NewTaskHandler(s, log)
				router := app.NewRouter(h, parser, log)
				id, query, body, requestMethod := task.String(), "limit=5&offset=10&id="+uuid.NewString(), `{"body":"Комментарий","created_by":"`+uuid.NewString()+`"}`, method
				if tt.name == "invalid ID" {
					id = "invalid"
				}
				if tt.name == "invalid request" {
					if method == http.MethodPost {
						body = `{"body":123}`
					} else {
						query = "limit=invalid"
					}
				}
				if tt.name == "wrong method" {
					requestMethod = http.MethodDelete
				}
				req := httptest.NewRequest(requestMethod, "/api/v1/tasks/"+id+"/comments?"+query, strings.NewReader(body))
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
				status := tt.status
				if tt.name == "valid" && method == http.MethodPost {
					status = 201
				}
				wantAdd, wantList := 0, 0
				if method == http.MethodPost {
					wantAdd = tt.calls
				} else {
					wantList = tt.calls
				}
				if rec.Code != status || addCalls != wantAdd || listCalls != wantList {
					t.Fatalf("status=%d add/list=%d/%d body=%s, want %d %d/%d", rec.Code, addCalls, listCalls, rec.Body.String(), status, wantAdd, wantList)
				}
				if status == 405 {
					return
				}
				if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("X-Request-ID") == "" {
					t.Errorf("missing JSON type/request ID: %v", rec.Header())
				}
				if tt.name == "valid" {
					if method == http.MethodPost {
						var item handler.TaskCommentResponse
						if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
							t.Fatal(err)
						}
						if item.ID != want.ID || item.TaskID != task || item.CreatedBy != actor || item.Body != want.Body || !item.CreatedAt.Equal(want.CreatedAt) {
							t.Errorf("unexpected comment: %+v", item)
						}
					} else {
						var page handler.ListTaskCommentsResponse
						if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
							t.Fatal(err)
						}
						if len(page.Items) != 1 || page.Total != 42 || page.Limit != 5 || page.Offset != 10 {
							t.Fatalf("unexpected page: %+v", page)
						}
						if page.Items[0].ID != want.ID || page.Items[0].TaskID != task {
							t.Errorf("unexpected list item: %+v", page.Items[0])
						}
					}
				} else {
					var body map[string]string
					if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
						t.Errorf("expected JSON error: %s", rec.Body.String())
					}
					if status == 401 && rec.Header().Get("WWW-Authenticate") != "Bearer" {
						t.Error("missing Bearer challenge")
					}
					if tt.name == "panic" && (strings.Contains(rec.Body.String(), "private comment panic") || !strings.Contains(logs.String(), "private comment panic")) {
						t.Error("panic must be logged and hidden from client")
					}
				}
			})
		}
	}
}
