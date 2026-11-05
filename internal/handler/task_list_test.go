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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type listTaskServiceFunc func(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error)

func (f listTaskServiceFunc) List(ctx context.Context, actor uuid.UUID, filter domain.TaskFilter) ([]domain.Task, int, error) {
	return f(ctx, actor, filter)
}

func (f listTaskServiceFunc) Create(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error) {
	panic("unexpected Create in list-task test")
}

func authorizedTaskList(s handler.TaskService, actor uuid.UUID, log *slog.Logger) http.Handler {
	parser := tokenParserFunc(func(string) (uuid.UUID, error) { return actor, nil })
	return handler.AuthMiddleware(parser, log)(http.HandlerFunc(handler.NewTaskHandler(s, log).List))
}

func taskListRequest(query string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?"+query, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

func TestListTasksQuery(t *testing.T) {
	actor, team, assignee := uuid.New(), uuid.New(), uuid.New()
	status := domain.InProgress
	for _, tt := range []struct {
		name, query string
		want        domain.TaskFilter
	}{
		{"defaults", "", domain.TaskFilter{TeamID: &team, Limit: 20}},
		{"all parameters", "&status=in_progress&assignee_id=" + assignee.String() + "&limit=5&offset=10", domain.TaskFilter{TeamID: &team, Status: &status, AssigneeID: &assignee, Limit: 5, Offset: 10}},
		// Range and status validation belongs to the service. Explicit zero
		// must reach it unchanged instead of being replaced by a default.
		{"explicit zero", "&limit=0&offset=0", domain.TaskFilter{TeamID: &team}},
		{"negative values", "&limit=-1&offset=-2", domain.TaskFilter{TeamID: &team, Limit: -1, Offset: -2}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			s := listTaskServiceFunc(func(gotCtx context.Context, id uuid.UUID, filter domain.TaskFilter) ([]domain.Task, int, error) {
				calls++
				if id != actor || !reflect.DeepEqual(filter, tt.want) {
					t.Errorf("actor=%s filter=%+v, want %s/%+v", id, filter, actor, tt.want)
				}
				cancel()
				if !errors.Is(gotCtx.Err(), context.Canceled) {
					t.Error("request cancellation was lost")
				}
				return []domain.Task{}, 0, nil
			})
			rec := httptest.NewRecorder()
			authorizedTaskList(s, actor, silentLogger()).ServeHTTP(rec, taskListRequest("team_id="+team.String()+tt.query+"&actor_id="+uuid.NewString()).WithContext(ctx))
			if calls != 1 || rec.Code != 200 {
				t.Fatalf("calls=%d status=%d body=%s", calls, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListTasksResponse(t *testing.T) {
	team, actor, assignee := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, 11, 5, 12, 0, 0, 123000000, time.UTC)
	tasks := []domain.Task{
		{ID: uuid.New(), TeamID: team, CreatedBy: actor, Title: "Новая", Description: "Описание", Status: domain.Todo, CreatedAt: now, UpdatedAt: now, Version: 1},
		{ID: uuid.New(), TeamID: team, CreatedBy: actor, AssigneeID: &assignee, Title: "Closed", Status: domain.Done, CreatedAt: now, UpdatedAt: now, ClosedAt: &now, Version: 3},
	}
	for _, tt := range []struct {
		name  string
		tasks []domain.Task
		total int
	}{
		{"items", tasks, 42}, {"nil slice", nil, 42}, {"empty slice", []domain.Task{}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := listTaskServiceFunc(func(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error) {
				return tt.tasks, tt.total, nil
			})
			rec := httptest.NewRecorder()
			authorizedTaskList(s, actor, silentLogger()).ServeHTTP(rec, taskListRequest("team_id="+team.String()+"&limit=5&offset=10"))
			if rec.Code != 200 {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			assertJSONContentType(t, rec)
			var body struct {
				Items  []map[string]json.RawMessage `json:"items"`
				Total  int                          `json:"total"`
				Limit  int                          `json:"limit"`
				Offset int                          `json:"offset"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Items == nil || len(body.Items) != len(tt.tasks) || body.Total != tt.total || body.Limit != 5 || body.Offset != 10 {
				t.Fatalf("unexpected page: %s", rec.Body.String())
			}
			for i, task := range tt.tasks {
				want := map[string]any{"id": task.ID, "team_id": task.TeamID, "created_by": task.CreatedBy, "assignee_id": task.AssigneeID, "title": task.Title, "description": task.Description, "status": task.Status, "created_at": task.CreatedAt, "updated_at": task.UpdatedAt, "closed_at": task.ClosedAt, "version": task.Version}
				if len(body.Items[i]) != len(want) {
					t.Errorf("unexpected item fields: %v", body.Items[i])
				}
				for key, value := range want {
					encoded, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(body.Items[i][key], encoded) {
						t.Errorf("item[%d].%s=%s, want %s", i, key, body.Items[i][key], encoded)
					}
				}
			}
		})
	}
}

func TestListTasksInvalidQuery(t *testing.T) {
	base := "team_id=" + uuid.NewString()
	for _, query := range []string{
		"", "team_id=", "team_id=invalid", base + "&assignee_id=", base + "&assignee_id=invalid",
		base + "&limit=", base + "&limit=abc", base + "&limit=1.5", base + "&limit=99999999999999999999999999999",
		base + "&offset=", base + "&offset=abc", base + "&offset=1.5", base + "&offset=99999999999999999999999999999",
	} {
		t.Run(query, func(t *testing.T) {
			s := listTaskServiceFunc(func(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error) {
				t.Fatal("invalid query reached service")
				return nil, 0, nil
			})
			rec := httptest.NewRecorder()
			authorizedTaskList(s, uuid.New(), silentLogger()).ServeHTTP(rec, taskListRequest(query))
			assertErrorResponse(t, rec, 400)
		})
	}
}

func TestListTasksErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		err    error
		status int
	}{
		{"invalid input", fmt.Errorf("list: %w", domain.ErrInvalidInput), 400},
		{"forbidden", fmt.Errorf("membership: %w", domain.ErrForbidden), 403},
		{"internal", errors.New("private list diagnostic"), 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			s := listTaskServiceFunc(func(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error) {
				calls++
				return nil, 0, tt.err
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			rec := httptest.NewRecorder()
			authorizedTaskList(s, uuid.New(), log).ServeHTTP(rec, taskListRequest("team_id="+uuid.NewString()))
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

func TestListTasksMissingIdentity(t *testing.T) {
	s := listTaskServiceFunc(func(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error) {
		t.Fatal("missing identity reached service")
		return nil, 0, nil
	})
	rec := httptest.NewRecorder()
	handler.NewTaskHandler(s, silentLogger()).List(rec, taskListRequest(""))
	assertErrorResponse(t, rec, 401)
}

func TestListTasksMarshalFailure(t *testing.T) {
	s := listTaskServiceFunc(func(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error) {
		return []domain.Task{{CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}, 1, nil
	})
	rec := httptest.NewRecorder()
	authorizedTaskList(s, uuid.New(), silentLogger()).ServeHTTP(rec, taskListRequest("team_id="+uuid.NewString()))
	assertErrorResponse(t, rec, 500)
}

func TestListTasksWriteFailure(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprint(authenticated), func(t *testing.T) {
			calls := 0
			s := listTaskServiceFunc(func(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error) {
				calls++
				return []domain.Task{}, 0, nil
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			w := &failingAuthResponseWriter{header: make(http.Header), err: errors.New("connection closed")}
			var endpoint http.Handler = http.HandlerFunc(handler.NewTaskHandler(s, log).List)
			status, wantCalls := 401, 0
			if authenticated {
				endpoint = authorizedTaskList(s, uuid.New(), log)
				status, wantCalls = 200, 1
			}
			endpoint.ServeHTTP(w, taskListRequest("team_id="+uuid.NewString()))
			if w.status != status || w.writes != 1 || calls != wantCalls {
				t.Errorf("status=%d writes=%d calls=%d, want %d/1/%d", w.status, w.writes, calls, status, wantCalls)
			}
			if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), w.err.Error()) {
				t.Errorf("expected one write error log: %s", logs.String())
			}
		})
	}
}
