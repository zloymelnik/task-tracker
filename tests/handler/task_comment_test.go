package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type commentServiceStub struct {
	taskServiceFunc
	add  func(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error)
	list func(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error)
}

func (s commentServiceStub) AddComment(ctx context.Context, actor, task uuid.UUID, input service.CreateTaskCommentInput) (*domain.TaskComment, error) {
	return s.add(ctx, actor, task, input)
}
func (s commentServiceStub) Comments(ctx context.Context, actor uuid.UUID, f domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	return s.list(ctx, actor, f)
}

func commentEndpoint(s handler.TaskService, log *slog.Logger) http.Handler {
	h := handler.NewTaskHandler(s, log)
	r := chi.NewRouter()
	r.Post("/tasks/{id}/comments", h.AddComment)
	r.Get("/tasks/{id}/comments", h.Comments)
	return r
}
func authorizedComments(s handler.TaskService, actor uuid.UUID, log *slog.Logger) http.Handler {
	p := tokenParserFunc(func(string) (uuid.UUID, error) { return actor, nil })
	return handler.AuthMiddleware(p, log)(commentEndpoint(s, log))
}
func commentRequest(method, id, query, body string) *http.Request {
	r := httptest.NewRequest(method, "/tasks/"+id+"/comments?"+query, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer test-token")
	return r
}

func TestAddCommentSuccess(t *testing.T) {
	actor, task := uuid.New(), uuid.New()
	now := time.Date(2026, 11, 5, 12, 0, 0, 123000000, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	want := domain.TaskComment{ID: uuid.New(), TaskID: task, CreatedBy: actor, Body: " \nКомментарий\n ", CreatedAt: now}
	calls := 0
	s := commentServiceStub{add: func(got context.Context, id, tid uuid.UUID, input service.CreateTaskCommentInput) (*domain.TaskComment, error) {
		calls++
		if id != actor || tid != task || input.Body != want.Body {
			t.Errorf("wrong identity/input: %s/%s/%+v", id, tid, input)
		}
		if user, ok := handler.UserIDFromContext(got); !ok || user != actor {
			t.Error("authenticated context lost")
		}
		cancel()
		if !errors.Is(got.Err(), context.Canceled) {
			t.Error("cancellation lost")
		}
		return &want, nil
	}}
	body := fmt.Sprintf(`{"body":" \nКомментарий\n ","created_by":%q,"task_id":%q}`, uuid.New(), uuid.New())
	rec := httptest.NewRecorder()
	authorizedComments(s, actor, silentLogger()).ServeHTTP(rec, commentRequest("POST", task.String(), "", body).WithContext(ctx))
	if rec.Code != 201 || calls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
	}
	assertJSONContentType(t, rec)
	assertCommentJSON(t, rec.Body.Bytes(), want)
}

func assertCommentJSON(t *testing.T, data []byte, want domain.TaskComment) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	values := map[string]any{"id": want.ID, "task_id": want.TaskID, "created_by": want.CreatedBy, "body": want.Body, "created_at": want.CreatedAt}
	if len(fields) != len(values) {
		t.Errorf("unexpected comment fields: %s", data)
	}
	for key, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fields[key], encoded) {
			t.Errorf("%s=%s, want %s", key, fields[key], encoded)
		}
	}
}

func TestCommentsPagination(t *testing.T) {
	actor, task := uuid.New(), uuid.New()
	for _, tt := range []struct {
		query         string
		limit, offset int
	}{{"", 20, 0}, {"limit=5&offset=10", 5, 10}, {"offset=20", 20, 20}, {"limit=100", 100, 0}, {"limit=0", 0, 0}, {"limit=-1&offset=-1", -1, -1}} {
		t.Run(tt.query, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			s := commentServiceStub{list: func(got context.Context, id uuid.UUID, f domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
				calls++
				if id != actor || f.TaskID == nil || *f.TaskID != task || f.Limit != tt.limit || f.Offset != tt.offset {
					t.Errorf("wrong actor/filter: %s/%+v", id, f)
				}
				cancel()
				if !errors.Is(got.Err(), context.Canceled) {
					t.Error("cancellation lost")
				}
				return nil, 42, nil
			}}
			rec := httptest.NewRecorder()
			query := tt.query + "&id=" + uuid.NewString() + "&user_id=" + uuid.NewString()
			authorizedComments(s, actor, silentLogger()).ServeHTTP(rec, commentRequest("GET", task.String(), query, "").WithContext(ctx))
			if rec.Code != 200 || calls != 1 {
				t.Fatalf("status=%d calls=%d", rec.Code, calls)
			}
			var page struct {
				Items  []json.RawMessage `json:"items"`
				Total  int               `json:"total"`
				Limit  int               `json:"limit"`
				Offset int               `json:"offset"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if page.Items == nil || len(page.Items) != 0 || page.Total != 42 || page.Limit != tt.limit || page.Offset != tt.offset {
				t.Errorf("unexpected page: %s", rec.Body.String())
			}
		})
	}
}

func TestCommentsResponse(t *testing.T) {
	task := uuid.New()
	now := time.Date(2026, 11, 5, 12, 0, 0, 123000000, time.UTC)
	items := []domain.TaskComment{{ID: uuid.New(), TaskID: task, CreatedBy: uuid.New(), Body: " \nКомментарий\n ", CreatedAt: now}, {ID: uuid.New(), TaskID: task, CreatedBy: uuid.New(), Body: "Second", CreatedAt: now.Add(-time.Hour)}}
	for _, tt := range []struct {
		name  string
		items []domain.TaskComment
		total int
	}{{"items", items, 42}, {"empty", []domain.TaskComment{}, 0}, {"nil", nil, 42}} {
		t.Run(tt.name, func(t *testing.T) {
			s := commentServiceStub{list: func(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
				return tt.items, tt.total, nil
			}}
			rec := httptest.NewRecorder()
			authorizedComments(s, uuid.New(), silentLogger()).ServeHTTP(rec, commentRequest("GET", task.String(), "limit=5&offset=10", ""))
			if rec.Code != 200 {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			assertJSONContentType(t, rec)
			var page struct {
				Items  []json.RawMessage `json:"items"`
				Total  int               `json:"total"`
				Limit  int               `json:"limit"`
				Offset int               `json:"offset"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if page.Items == nil || len(page.Items) != len(tt.items) || page.Total != tt.total || page.Limit != 5 || page.Offset != 10 {
				t.Fatalf("unexpected page: %s", rec.Body.String())
			}
			for i, item := range tt.items {
				assertCommentJSON(t, page.Items[i], item)
			}
		})
	}
}

func TestCommentsInvalidRequest(t *testing.T) {
	for _, tt := range []struct {
		name, method, id, query, body string
		status                        int
	}{
		{"bad add ID", "POST", "invalid", "", `{"body":"Text"}`, 400}, {"bad list ID", "GET", "invalid", "id=" + uuid.NewString(), "", 400},
		{"empty", "POST", "", "", "", 400}, {"malformed", "POST", "", "", `{"body":`, 400}, {"null", "POST", "", "", "null", 400}, {"array", "POST", "", "", "[]", 400},
		{"body type", "POST", "", "", `{"body":123}`, 400}, {"extra object", "POST", "", "", `{"body":"Text"} {}`, 400}, {"trailing garbage", "POST", "", "", `{} bad`, 400},
		{"oversized", "POST", "", "", `{"body":"` + strings.Repeat("a", 64<<10) + `"}`, 413},
		{"empty limit", "GET", "", "limit=", "", 400}, {"invalid limit", "GET", "", "limit=abc", "", 400}, {"fraction limit", "GET", "", "limit=1.5", "", 400}, {"overflow limit", "GET", "", "limit=999999999999999999999999", "", 400},
		{"empty offset", "GET", "", "offset=", "", 400}, {"invalid offset", "GET", "", "offset=abc", "", 400}, {"fraction offset", "GET", "", "offset=1.5", "", 400}, {"overflow offset", "GET", "", "offset=999999999999999999999999", "", 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id := tt.id
			if id == "" {
				id = uuid.NewString()
			}
			s := commentServiceStub{add: func(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
				t.Fatal("invalid request reached AddComment")
				return nil, nil
			}, list: func(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
				t.Fatal("invalid request reached Comments")
				return nil, 0, nil
			}}
			rec := httptest.NewRecorder()
			authorizedComments(s, uuid.New(), silentLogger()).ServeHTTP(rec, commentRequest(tt.method, id, tt.query, tt.body))
			assertErrorResponse(t, rec, tt.status)
		})
	}
}

func TestCommentServiceErrors(t *testing.T) {
	for _, method := range []string{"POST", "GET"} {
		for _, tt := range []struct {
			name   string
			err    error
			status int
		}{{"invalid", domain.ErrInvalidInput, 400}, {"forbidden", domain.ErrForbidden, 403}, {"not found", domain.ErrTaskNotFound, 404}, {"internal", errors.New("private comment diagnostic"), 500}} {
			t.Run(method+"/"+tt.name, func(t *testing.T) {
				calls := 0
				err := fmt.Errorf("comment: %w", tt.err)
				s := commentServiceStub{add: func(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
					calls++
					return nil, err
				}, list: func(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
					calls++
					return nil, 0, err
				}}
				var logs bytes.Buffer
				log := slog.New(slog.NewTextHandler(&logs, nil))
				rec := httptest.NewRecorder()
				authorizedComments(s, uuid.New(), log).ServeHTTP(rec, commentRequest(method, uuid.NewString(), "", `{"body":"Text"}`))
				assertErrorResponse(t, rec, tt.status)
				if calls != 1 {
					t.Errorf("calls=%d, want 1", calls)
				}
				if tt.status == 500 && (strings.Contains(rec.Body.String(), tt.err.Error()) || !strings.Contains(logs.String(), tt.err.Error())) {
					t.Error("internal error must be logged and hidden from client")
				}
			})
		}
	}
}

func TestCommentMissingIdentity(t *testing.T) {
	for _, method := range []string{"POST", "GET"} {
		t.Run(method, func(t *testing.T) {
			s := commentServiceStub{add: func(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
				t.Fatal("unauthorized add")
				return nil, nil
			}, list: func(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
				t.Fatal("unauthorized list")
				return nil, 0, nil
			}}
			rec := httptest.NewRecorder()
			commentEndpoint(s, silentLogger()).ServeHTTP(rec, commentRequest(method, uuid.NewString(), "", `{"body":"Text"}`))
			assertErrorResponse(t, rec, 401)
		})
	}
}

func TestCommentEncodeFailure(t *testing.T) {
	for _, method := range []string{"POST", "GET"} {
		t.Run(method, func(t *testing.T) {
			bad := domain.TaskComment{CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}
			s := commentServiceStub{add: func(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
				return &bad, nil
			}, list: func(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
				return []domain.TaskComment{bad}, 1, nil
			}}
			rec := httptest.NewRecorder()
			authorizedComments(s, uuid.New(), silentLogger()).ServeHTTP(rec, commentRequest(method, uuid.NewString(), "", `{"body":"Text"}`))
			assertErrorResponse(t, rec, 500)
		})
	}
}

func TestCommentWriteFailure(t *testing.T) {
	for _, method := range []string{"POST", "GET"} {
		for _, authorized := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/authorized=%v", method, authorized), func(t *testing.T) {
				calls := 0
				s := commentServiceStub{add: func(context.Context, uuid.UUID, uuid.UUID, service.CreateTaskCommentInput) (*domain.TaskComment, error) {
					calls++
					return &domain.TaskComment{}, nil
				}, list: func(context.Context, uuid.UUID, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
					calls++
					return nil, 0, nil
				}}
				var logs bytes.Buffer
				log := slog.New(slog.NewTextHandler(&logs, nil))
				w := &failingAuthResponseWriter{header: make(http.Header), err: errors.New("connection closed")}
				endpoint := commentEndpoint(s, log)
				status, wantCalls := 401, 0
				if authorized {
					endpoint = authorizedComments(s, uuid.New(), log)
					status, wantCalls = 200, 1
					if method == "POST" {
						status = 201
					}
				}
				endpoint.ServeHTTP(w, commentRequest(method, uuid.NewString(), "", `{"body":"Text"}`))
				if w.status != status || w.writes != 1 || calls != wantCalls {
					t.Errorf("status=%d writes=%d calls=%d, want %d/1/%d", w.status, w.writes, calls, status, wantCalls)
				}
				if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), w.err.Error()) {
					t.Errorf("expected one write error log: %s", logs.String())
				}
			})
		}
	}
}
