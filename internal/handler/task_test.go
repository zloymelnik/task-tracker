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

type taskServiceFunc func(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error)

func (f taskServiceFunc) List(context.Context, uuid.UUID, domain.TaskFilter) ([]domain.Task, int, error) {
	panic("unexpected List in create-task test")
}

func (f taskServiceFunc) Create(ctx context.Context, id uuid.UUID, input service.CreateTaskInput) (*domain.Task, error) {
	return f(ctx, id, input)
}

func authorizedTaskHandler(s handler.TaskService, id uuid.UUID, log *slog.Logger) http.Handler {
	parser := tokenParserFunc(func(string) (uuid.UUID, error) { return id, nil })
	return handler.AuthMiddleware(parser, log)(http.HandlerFunc(handler.NewTaskHandler(s, log).Create))
}

func taskRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

func TestCreateTaskSuccess(t *testing.T) {
	actor, team, assignee := uuid.New(), uuid.New(), uuid.New()
	for _, assignment := range []string{"omitted", "null", "assigned"} {
		t.Run(assignment, func(t *testing.T) {
			var assigneeID *uuid.UUID
			body := fmt.Sprintf(`{"team_id":%q,"title":"  Задача  ","description":"Описание\nзадачи","created_by":%q,"status":"done","version":99`, team, uuid.New())
			switch assignment {
			case "null":
				body += `,"assignee_id":null`
			case "assigned":
				assigneeID = &assignee
				body += fmt.Sprintf(`,"assignee_id":%q`, assignee)
			}
			body += "} \n\t"
			now := time.Date(2026, 11, 3, 12, 0, 0, 123000000, time.UTC)
			want := domain.Task{ID: uuid.New(), TeamID: team, CreatedBy: actor, AssigneeID: assigneeID,
				Title: "Задача", Description: "Описание\nзадачи", Status: domain.Todo,
				CreatedAt: now, UpdatedAt: now, Version: 1}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			s := taskServiceFunc(func(gotCtx context.Context, id uuid.UUID, input service.CreateTaskInput) (*domain.Task, error) {
				calls++
				if id != actor || input.TeamID != team || input.Title != "  Задача  " || input.Description != want.Description || !reflect.DeepEqual(input.AssigneeID, assigneeID) {
					t.Errorf("actor=%s input=%+v: request fields or authenticated author lost", id, input)
				}
				cancel()
				if !errors.Is(gotCtx.Err(), context.Canceled) {
					t.Error("request cancellation was lost")
				}
				return &want, nil
			})
			rec := httptest.NewRecorder()
			authorizedTaskHandler(s, actor, silentLogger()).ServeHTTP(rec, taskRequest(body).WithContext(ctx))
			if rec.Code != http.StatusCreated || calls != 1 {
				t.Fatalf("status=%d calls=%d body=%s, want 201/1", rec.Code, calls, rec.Body.String())
			}
			assertJSONContentType(t, rec)
			// Decode independently of the response DTO to catch incorrect JSON tags.
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"id", "team_id", "created_by", "assignee_id", "title", "description", "status", "created_at", "updated_at", "closed_at", "version"} {
				if _, ok := fields[key]; !ok {
					t.Errorf("missing JSON field %q", key)
				}
			}
			if len(fields) != 11 || string(fields["closed_at"]) != "null" || (assigneeID == nil && string(fields["assignee_id"]) != "null") {
				t.Errorf("unexpected response fields: %s", rec.Body.String())
			}
			// Check each public field without relying on the production DTO tags.
			values := map[string]any{"id": want.ID, "team_id": team, "created_by": actor,
				"assignee_id": assigneeID, "title": want.Title, "description": want.Description,
				"status": domain.Todo, "created_at": now, "updated_at": now, "closed_at": nil, "version": uint32(1)}
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

func TestCreateTaskInvalidJSON(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
	}{
		{"empty", "", 400}, {"malformed", `{"title":`, 400},
		{"null", `null`, 400}, {"array", `[]`, 400},
		{"wrong title type", `{"title":42}`, 400},
		{"invalid team UUID", `{"team_id":"invalid"}`, 400},
		{"invalid assignee UUID", `{"assignee_id":"invalid"}`, 400},
		{"second object", `{} {}`, 400}, {"trailing garbage", `{} garbage`, 400},
		{"oversized", `{"description":"` + strings.Repeat("a", 64<<10) + `"}`, 413},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := taskServiceFunc(func(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error) {
				t.Fatal("invalid JSON must not reach service")
				return nil, nil
			})
			rec := httptest.NewRecorder()
			authorizedTaskHandler(s, uuid.New(), silentLogger()).ServeHTTP(rec, taskRequest(tt.body))
			assertErrorResponse(t, rec, tt.status)
		})
	}
}

func TestCreateTaskMissingIdentity(t *testing.T) {
	s := taskServiceFunc(func(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error) {
		t.Fatal("missing identity must not reach service")
		return nil, nil
	})
	rec := httptest.NewRecorder()
	handler.NewTaskHandler(s, silentLogger()).Create(rec, taskRequest(`{}`))
	assertErrorResponse(t, rec, http.StatusUnauthorized)
}

func TestCreateTaskServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		err    error
		status int
	}{
		{"invalid input", fmt.Errorf("create: %w", domain.ErrInvalidInput), 400},
		{"forbidden", fmt.Errorf("membership: %w", domain.ErrForbidden), 403},
		{"database failure", errors.New("private database diagnostic"), 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			s := taskServiceFunc(func(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error) {
				calls++
				return nil, tt.err
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			rec := httptest.NewRecorder()
			authorizedTaskHandler(s, uuid.New(), log).ServeHTTP(rec, taskRequest(`{}`))
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

func TestCreateTaskMarshalFailure(t *testing.T) {
	s := taskServiceFunc(func(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error) {
		return &domain.Task{CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
	})
	rec := httptest.NewRecorder()
	authorizedTaskHandler(s, uuid.New(), silentLogger()).ServeHTTP(rec, taskRequest(`{}`))
	assertErrorResponse(t, rec, http.StatusInternalServerError)
}

func TestCreateTaskWriteFailure(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprint(authenticated), func(t *testing.T) {
			calls := 0
			s := taskServiceFunc(func(context.Context, uuid.UUID, service.CreateTaskInput) (*domain.Task, error) {
				calls++
				return &domain.Task{}, nil
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			w := &failingAuthResponseWriter{header: make(http.Header), err: errors.New("connection closed")}
			var endpoint http.Handler = http.HandlerFunc(handler.NewTaskHandler(s, log).Create)
			status, wantCalls := 401, 0
			if authenticated {
				endpoint = authorizedTaskHandler(s, uuid.New(), log)
				status, wantCalls = 201, 1
			}
			endpoint.ServeHTTP(w, taskRequest(`{}`))
			if w.status != status || w.writes != 1 || calls != wantCalls {
				t.Errorf("status=%d writes=%d calls=%d, want %d/1/%d", w.status, w.writes, calls, status, wantCalls)
			}
			if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), w.err.Error()) {
				t.Errorf("expected one write error log, got %s", logs.String())
			}
		})
	}
}
