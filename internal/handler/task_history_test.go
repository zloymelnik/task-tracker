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

type taskHistoryServiceFunc func(context.Context, uuid.UUID, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error)

func (f taskHistoryServiceFunc) History(ctx context.Context, actor uuid.UUID, filter domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
	return f(ctx, actor, filter)
}
func (f taskHistoryServiceFunc) Create(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error) {
	panic("unexpected Create in history test")
}
func (f taskHistoryServiceFunc) List(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error) {
	panic("unexpected List in history test")
}
func (f taskHistoryServiceFunc) Update(context.Context, uuid.UUID, uuid.UUID, service.UpdateTaskInput) (*domain.Task, error) {
	panic("unexpected Update in history test")
}

func taskHistoryEndpoint(s handler.TaskService, log *slog.Logger) http.Handler {
	router := chi.NewRouter()
	router.Get("/tasks/{id}/history", handler.NewTaskHandler(s, log).History)
	return router
}

func authorizedTaskHistory(s handler.TaskService, actor uuid.UUID, log *slog.Logger) http.Handler {
	parser := tokenParserFunc(func(string) (uuid.UUID, error) { return actor, nil })
	return handler.AuthMiddleware(parser, log)(taskHistoryEndpoint(s, log))
}

func taskHistoryRequest(id, query string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/tasks/"+id+"/history?"+query, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

func TestTaskHistoryQuery(t *testing.T) {
	actor, task := uuid.New(), uuid.New()
	for _, tt := range []struct {
		query         string
		limit, offset int
	}{
		{"", 20, 0}, {"limit=5&offset=10", 5, 10}, {"offset=100", 20, 100},
		{"limit=100", 100, 0}, {"limit=0&offset=0", 0, 0}, {"limit=-1&offset=-1", -1, -1},
	} {
		t.Run(tt.query, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			s := taskHistoryServiceFunc(func(gotCtx context.Context, id uuid.UUID, filter domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
				calls++
				if id != actor || filter.TaskID == nil || *filter.TaskID != task || filter.Limit != tt.limit || filter.Offset != tt.offset {
					t.Errorf("wrong actor or filter: %s/%+v", id, filter)
				}
				if got, ok := handler.UserIDFromContext(gotCtx); !ok || got != actor {
					t.Error("authenticated context missing")
				}
				cancel()
				if !errors.Is(gotCtx.Err(), context.Canceled) {
					t.Error("cancellation lost")
				}
				return nil, 42, nil
			})
			rec := httptest.NewRecorder()
			// The path ID and authenticated user must take precedence over query IDs.
			req := taskHistoryRequest(task.String(), tt.query+"&id="+uuid.NewString()+"&user_id="+uuid.NewString()).WithContext(ctx)
			authorizedTaskHistory(s, actor, silentLogger()).ServeHTTP(rec, req)
			if rec.Code != 200 || calls != 1 {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
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

func TestTaskHistoryResponse(t *testing.T) {
	task, actor, assignee := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, 11, 5, 12, 0, 0, 123000000, time.UTC)
	entries := []domain.TaskHistory{
		{ID: uuid.New(), TaskID: task, ChangedBy: actor, CreatedAt: now, Changes: domain.TaskChanges{"assignee_id": {Old: nil, New: &assignee}, "title": {Old: "Old", New: "Новая задача"}}},
		{ID: uuid.New(), TaskID: task, ChangedBy: assignee, CreatedAt: now.Add(-time.Hour), Changes: domain.TaskChanges{"assignee_id": {Old: &assignee, New: nil}, "status": {Old: domain.Todo, New: domain.Done}}},
	}
	for _, tt := range []struct {
		name    string
		entries []domain.TaskHistory
		total   int
	}{
		{"entries", entries, 42}, {"empty", []domain.TaskHistory{}, 0}, {"nil page", nil, 42},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := taskHistoryServiceFunc(func(context.Context, uuid.UUID, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
				return tt.entries, tt.total, nil
			})
			rec := httptest.NewRecorder()
			authorizedTaskHistory(s, actor, silentLogger()).ServeHTTP(rec, taskHistoryRequest(task.String(), "limit=5&offset=10"))
			if rec.Code != 200 {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			assertJSONContentType(t, rec)
			var page struct {
				Items  []map[string]json.RawMessage `json:"items"`
				Total  int                          `json:"total"`
				Limit  int                          `json:"limit"`
				Offset int                          `json:"offset"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if page.Items == nil || len(page.Items) != len(tt.entries) || page.Total != tt.total || page.Limit != 5 || page.Offset != 10 {
				t.Fatalf("unexpected page: %s", rec.Body.String())
			}
			for i, entry := range tt.entries {
				values := map[string]any{"id": entry.ID, "task_id": entry.TaskID, "changed_by": entry.ChangedBy, "changes": entry.Changes, "created_at": entry.CreatedAt}
				if len(page.Items[i]) != len(values) {
					t.Errorf("unexpected fields: %v", page.Items[i])
				}
				for key, value := range values {
					want, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(page.Items[i][key], want) {
						t.Errorf("entry[%d].%s=%s, want %s", i, key, page.Items[i][key], want)
					}
				}
			}
		})
	}
}

func TestTaskHistoryInvalidRequest(t *testing.T) {
	for _, tt := range []struct{ id, query string }{
		{"invalid", ""}, {"invalid", "id=" + uuid.NewString()},
		{"", "limit="}, {"", "limit=abc"}, {"", "limit=1.5"}, {"", "limit=99999999999999999999999999"},
		{"", "offset="}, {"", "offset=abc"}, {"", "offset=1.5"}, {"", "offset=99999999999999999999999999"},
	} {
		t.Run(tt.id+"/"+tt.query, func(t *testing.T) {
			id := tt.id
			if id == "" {
				id = uuid.NewString()
			}
			s := taskHistoryServiceFunc(func(context.Context, uuid.UUID, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
				t.Fatal("invalid request reached service")
				return nil, 0, nil
			})
			rec := httptest.NewRecorder()
			authorizedTaskHistory(s, uuid.New(), silentLogger()).ServeHTTP(rec, taskHistoryRequest(id, tt.query))
			assertErrorResponse(t, rec, 400)
		})
	}
}

func TestTaskHistoryServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		err    error
		status int
	}{
		{"invalid", domain.ErrInvalidInput, 400}, {"forbidden", domain.ErrForbidden, 403},
		{"not found", domain.ErrTaskNotFound, 404}, {"internal", errors.New("private history diagnostic"), 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			s := taskHistoryServiceFunc(func(context.Context, uuid.UUID, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
				calls++
				return nil, 0, fmt.Errorf("history: %w", tt.err)
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			rec := httptest.NewRecorder()
			authorizedTaskHistory(s, uuid.New(), log).ServeHTTP(rec, taskHistoryRequest(uuid.NewString(), ""))
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

func TestTaskHistoryMissingIdentity(t *testing.T) {
	s := taskHistoryServiceFunc(func(context.Context, uuid.UUID, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
		t.Fatal("missing identity reached service")
		return nil, 0, nil
	})
	rec := httptest.NewRecorder()
	taskHistoryEndpoint(s, silentLogger()).ServeHTTP(rec, taskHistoryRequest(uuid.NewString(), ""))
	assertErrorResponse(t, rec, 401)
}

func TestTaskHistoryEncodeFailure(t *testing.T) {
	for _, value := range []string{"date", "changes"} {
		t.Run(value, func(t *testing.T) {
			entry := domain.TaskHistory{Changes: domain.TaskChanges{}}
			if value == "date" {
				entry.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			} else {
				entry.Changes["bad"] = domain.FieldChange{New: make(chan int)}
			}
			s := taskHistoryServiceFunc(func(context.Context, uuid.UUID, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
				return []domain.TaskHistory{entry}, 1, nil
			})
			rec := httptest.NewRecorder()
			authorizedTaskHistory(s, uuid.New(), silentLogger()).ServeHTTP(rec, taskHistoryRequest(uuid.NewString(), ""))
			assertErrorResponse(t, rec, 500)
		})
	}
}

func TestTaskHistoryWriteFailure(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		t.Run(fmt.Sprint(authorized), func(t *testing.T) {
			calls := 0
			s := taskHistoryServiceFunc(func(context.Context, uuid.UUID, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
				calls++
				return nil, 0, nil
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			w := &failingAuthResponseWriter{header: make(http.Header), err: errors.New("connection closed")}
			endpoint := taskHistoryEndpoint(s, log)
			status, wantCalls := 401, 0
			if authorized {
				endpoint = authorizedTaskHistory(s, uuid.New(), log)
				status, wantCalls = 200, 1
			}
			endpoint.ServeHTTP(w, taskHistoryRequest(uuid.NewString(), ""))
			if w.status != status || w.writes != 1 || calls != wantCalls {
				t.Errorf("status=%d writes=%d calls=%d, want %d/1/%d", w.status, w.writes, calls, status, wantCalls)
			}
			if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), w.err.Error()) {
				t.Errorf("expected one write error log: %s", logs.String())
			}
		})
	}
}
