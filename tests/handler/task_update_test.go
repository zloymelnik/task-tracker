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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type updateTaskServiceFunc func(context.Context, uuid.UUID, uuid.UUID, service.UpdateTaskInput) (*domain.Task, error)

func (f updateTaskServiceFunc) History(context.Context, uuid.UUID, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
	panic("unexpected History in update-task test")
}

func (f updateTaskServiceFunc) Update(ctx context.Context, actor, task uuid.UUID, input service.UpdateTaskInput) (*domain.Task, error) {
	return f(ctx, actor, task, input)
}
func (f updateTaskServiceFunc) Create(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error) {
	panic("unexpected Create in update-task test")
}
func (f updateTaskServiceFunc) List(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error) {
	panic("unexpected List in update-task test")
}

func taskUpdateEndpoint(s handler.TaskService, log *slog.Logger) http.Handler {
	router := chi.NewRouter()
	router.Put("/tasks/{id}", handler.NewTaskHandler(s, log).Update)
	return router
}

func authorizedTaskUpdate(s handler.TaskService, actor uuid.UUID, log *slog.Logger) http.Handler {
	parser := tokenParserFunc(func(string) (uuid.UUID, error) { return actor, nil })
	return handler.AuthMiddleware(parser, log)(taskUpdateEndpoint(s, log))
}

func taskUpdateRequest(id, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/tasks/"+id, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

func TestUpdateTaskSuccess(t *testing.T) {
	actor, taskID, team, assignee := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, assignment := range []string{"omitted", "null", "UUID"} {
		t.Run(assignment, func(t *testing.T) {
			body := fmt.Sprintf(`{"version":4,"title":"  Заголовок  ","description":"","status":"done","created_by":%q,"team_id":%q,"id":%q`, uuid.New(), uuid.New(), uuid.New())
			var assigned *uuid.UUID
			if assignment == "null" {
				body += `,"assignee_id":null`
			}
			if assignment == "UUID" {
				assigned = &assignee
				body += fmt.Sprintf(`,"assignee_id":%q`, assignee)
			}
			body += "} \n\t"
			now := time.Date(2026, 11, 5, 12, 0, 0, 123000000, time.UTC)
			want := domain.Task{ID: taskID, TeamID: team, CreatedBy: actor, AssigneeID: assigned, Title: "Заголовок", Status: domain.Done, CreatedAt: now.Add(-time.Hour), UpdatedAt: now, ClosedAt: &now, Version: 5}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			s := updateTaskServiceFunc(func(gotCtx context.Context, id, tid uuid.UUID, input service.UpdateTaskInput) (*domain.Task, error) {
				calls++
				if id != actor || tid != taskID || input.Version != 4 || input.Title == nil || *input.Title != "  Заголовок  " || input.Description == nil || *input.Description != "" || input.Status == nil || *input.Status != domain.Done || input.AssigneeSet != (assignment != "omitted") || !reflect.DeepEqual(input.AssigneeID, assigned) {
					t.Errorf("wrong identity or input: %s/%s/%+v", id, tid, input)
				}
				if got, ok := handler.UserIDFromContext(gotCtx); !ok || got != actor {
					t.Error("authenticated identity missing from service context")
				}
				cancel()
				if !errors.Is(gotCtx.Err(), context.Canceled) {
					t.Error("request cancellation lost")
				}
				return &want, nil
			})
			rec := httptest.NewRecorder()
			authorizedTaskUpdate(s, actor, silentLogger()).ServeHTTP(rec, taskUpdateRequest(taskID.String(), body).WithContext(ctx))
			if rec.Code != 200 || calls != 1 {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
			}
			assertJSONContentType(t, rec)
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			values := map[string]any{"id": taskID, "team_id": team, "created_by": actor, "assignee_id": assigned, "title": want.Title, "description": "", "status": domain.Done, "created_at": want.CreatedAt, "updated_at": now, "closed_at": now, "version": uint32(5)}
			if len(fields) != len(values) {
				t.Errorf("unexpected response fields: %s", rec.Body.String())
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
		})
	}
}

func TestUpdateTaskOmittedFields(t *testing.T) {
	calls := 0
	s := updateTaskServiceFunc(func(_ context.Context, _, task uuid.UUID, input service.UpdateTaskInput) (*domain.Task, error) {
		calls++
		if !reflect.DeepEqual(input, service.UpdateTaskInput{Version: 4}) {
			t.Errorf("omitted fields became updates: %+v", input)
		}
		return &domain.Task{ID: task, Version: 4}, nil
	})
	rec := httptest.NewRecorder()
	authorizedTaskUpdate(s, uuid.New(), silentLogger()).ServeHTTP(rec, taskUpdateRequest(uuid.NewString(), `{"version":4}`))
	if rec.Code != 200 || calls != 1 {
		t.Errorf("status=%d calls=%d", rec.Code, calls)
	}
}

func TestUpdateTaskInvalidRequest(t *testing.T) {
	for _, tt := range []struct {
		name, id, body string
		status         int
	}{
		{"bad task UUID", "invalid", `{"version":4}`, 400},
		{"empty", "", "", 400}, {"malformed", "", `{"version":`, 400},
		{"null", "", `null`, 400}, {"array", "", `[]`, 400},
		{"second object", "", `{"version":4} {}`, 400}, {"trailing garbage", "", `{"version":4} bad`, 400},
		{"string version", "", `{"version":"4"}`, 400}, {"negative version", "", `{"version":-1}`, 400},
		{"fractional version", "", `{"version":1.5}`, 400}, {"overflow version", "", `{"version":4294967296}`, 400},
		{"wrong title", "", `{"title":1}`, 400}, {"wrong description", "", `{"description":false}`, 400}, {"wrong status", "", `{"status":[]}`, 400},
		{"bad assignee UUID", "", `{"assignee_id":"invalid"}`, 400},
		{"empty assignee", "", `{"assignee_id":""}`, 400}, {"number assignee", "", `{"assignee_id":123}`, 400},
		{"object assignee", "", `{"assignee_id":{}}`, 400}, {"array assignee", "", `{"assignee_id":[]}`, 400},
		{"oversized", "", `{"description":"` + strings.Repeat("a", 64<<10) + `"}`, 413},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id := tt.id
			if id == "" {
				id = uuid.NewString()
			}
			s := updateTaskServiceFunc(func(context.Context, uuid.UUID, uuid.UUID, service.UpdateTaskInput) (*domain.Task, error) {
				t.Fatal("invalid request reached service")
				return nil, nil
			})
			rec := httptest.NewRecorder()
			authorizedTaskUpdate(s, uuid.New(), silentLogger()).ServeHTTP(rec, taskUpdateRequest(id, tt.body))
			assertErrorResponse(t, rec, tt.status)
		})
	}
}

func TestUpdateTaskServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		err    error
		status int
	}{
		{"invalid", domain.ErrInvalidInput, 400}, {"forbidden", domain.ErrForbidden, 403},
		{"missing", domain.ErrTaskNotFound, 404}, {"conflict", domain.ErrTaskVersionConflict, 409},
		{"internal", errors.New("private update diagnostic"), 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			s := updateTaskServiceFunc(func(context.Context, uuid.UUID, uuid.UUID, service.UpdateTaskInput) (*domain.Task, error) {
				calls++
				return nil, fmt.Errorf("update: %w", tt.err)
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			rec := httptest.NewRecorder()
			authorizedTaskUpdate(s, uuid.New(), log).ServeHTTP(rec, taskUpdateRequest(uuid.NewString(), `{"version":4}`))
			assertErrorResponse(t, rec, tt.status)
			if calls != 1 {
				t.Errorf("calls=%d, want 1", calls)
			}
			if tt.status == 500 && (strings.Contains(rec.Body.String(), tt.err.Error()) || !strings.Contains(logs.String(), tt.err.Error())) {
				t.Error("internal diagnostic must be logged and hidden from client")
			}
		})
	}
}

func TestUpdateTaskMissingIdentity(t *testing.T) {
	s := updateTaskServiceFunc(func(context.Context, uuid.UUID, uuid.UUID, service.UpdateTaskInput) (*domain.Task, error) {
		t.Fatal("missing identity reached service")
		return nil, nil
	})
	rec := httptest.NewRecorder()
	taskUpdateEndpoint(s, silentLogger()).ServeHTTP(rec, taskUpdateRequest(uuid.NewString(), `{"version":4}`))
	assertErrorResponse(t, rec, 401)
}

func TestUpdateTaskMarshalFailure(t *testing.T) {
	s := updateTaskServiceFunc(func(context.Context, uuid.UUID, uuid.UUID, service.UpdateTaskInput) (*domain.Task, error) {
		return &domain.Task{UpdatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
	})
	rec := httptest.NewRecorder()
	authorizedTaskUpdate(s, uuid.New(), silentLogger()).ServeHTTP(rec, taskUpdateRequest(uuid.NewString(), `{"version":4}`))
	assertErrorResponse(t, rec, 500)
}

func TestUpdateTaskWriteFailure(t *testing.T) {
	for _, scenario := range []string{"unauthorized", "invalid ID", "invalid assignee", "success"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			s := updateTaskServiceFunc(func(context.Context, uuid.UUID, uuid.UUID, service.UpdateTaskInput) (*domain.Task, error) {
				calls++
				return &domain.Task{}, nil
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			w := &failingAuthResponseWriter{header: make(http.Header), err: errors.New("connection closed")}
			endpoint := authorizedTaskUpdate(s, uuid.New(), log)
			id, body, status, wantCalls := uuid.NewString(), `{"version":4}`, 400, 0
			switch scenario {
			case "unauthorized":
				endpoint = taskUpdateEndpoint(s, log)
				status = 401
			case "invalid ID":
				id = "invalid"
			case "invalid assignee":
				body = `{"assignee_id":"invalid"}`
			case "success":
				status, wantCalls = 200, 1
			}
			endpoint.ServeHTTP(w, taskUpdateRequest(id, body))
			if w.status != status || w.writes != 1 || calls != wantCalls {
				t.Errorf("status=%d writes=%d calls=%d, want %d/1/%d", w.status, w.writes, calls, status, wantCalls)
			}
			if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), w.err.Error()) {
				t.Errorf("expected one write error log: %s", logs.String())
			}
		})
	}
}
