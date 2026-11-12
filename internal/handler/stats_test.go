package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/handler"
)

type statsServiceFunc func(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*domain.TeamStats, error)

func (f statsServiceFunc) Get(
	ctx context.Context,
	actorID, teamID uuid.UUID,
) (*domain.TeamStats, error) {
	return f(ctx, actorID, teamID)
}

type statsTokenParser func(string) (uuid.UUID, error)

func (f statsTokenParser) ParseToken(token string) (uuid.UUID, error) {
	return f(token)
}

func TestStatsHandlerGetSuccess(t *testing.T) {
	actorID, teamID := uuid.New(), uuid.New()
	want := domain.TeamStats{TeamID: teamID}

	calls := 0
	svc := statsServiceFunc(func(
		ctx context.Context,
		gotActorID, gotTeamID uuid.UUID,
	) (*domain.TeamStats, error) {
		calls++
		if gotActorID != actorID || gotTeamID != teamID {
			t.Error("service received unexpected IDs")
		}
		return &want, nil
	})

	parser := statsTokenParser(func(token string) (uuid.UUID, error) {
		if token != "test-token" {
			t.Errorf("token = %q, want test-token", token)
		}
		return actorID, nil
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.NewStatsHandler(svc, log)

	router := chi.NewRouter()
	router.Get("/teams/{team_id}/stats", h.Get)

	endpoint := handler.AuthMiddleware(parser, log)(router)

	req := httptest.NewRequest(
		http.MethodGet,
		"/teams/"+teamID.String()+"/stats",
		nil,
	)
	req.Header.Set("Authorization", "Bearer test-token")

	rec := httptest.NewRecorder()
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Errorf("service calls = %d, want 1", calls)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body struct {
		TeamID              uuid.UUID         `json:"team_id"`
		TopAssignees        []json.RawMessage `json:"top_assignees"`
		AverageCloseSeconds *float64          `json:"average_close_seconds"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.TeamID != teamID {
		t.Errorf("team_id = %s, want %s", body.TeamID, teamID)
	}
	if body.TopAssignees == nil || len(body.TopAssignees) != 0 {
		t.Errorf("top_assignees = %v, want []", body.TopAssignees)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatalf("decode response fields: %v", err)
	}
	if string(fields["average_close_seconds"]) != "null" {
		t.Errorf("average_close_seconds = %s, want null",
			fields["average_close_seconds"])
	}
}

func TestStatsHandlerGetNotEmptySuccess(t *testing.T) {
	actorID, teamID := uuid.New(), uuid.New()
	averageCloseSeconds := 50.5
	want := domain.TeamStats{
		TeamID: teamID,
		TasksByStatus: domain.TaskStatusCounts{
			Todo:       1,
			InProgress: 2,
			Done:       3,
		},
		TopAssignees: []domain.AssigneeStats{
			{UserID: actorID, Name: "Test user", ClosedTasks: 3},
		},
		AverageCloseSeconds: &averageCloseSeconds,
		CommentsCount:       3,
	}

	calls := 0
	svc := statsServiceFunc(func(
		ctx context.Context,
		gotActorID, gotTeamID uuid.UUID,
	) (*domain.TeamStats, error) {
		calls++
		if gotActorID != actorID || gotTeamID != teamID {
			t.Error("service received unexpected IDs")
		}
		return &want, nil
	})

	parser := statsTokenParser(func(token string) (uuid.UUID, error) {
		if token != "test-token" {
			t.Errorf("token = %q, want test-token", token)
		}
		return actorID, nil
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.NewStatsHandler(svc, log)

	router := chi.NewRouter()
	router.Get("/teams/{team_id}/stats", h.Get)

	endpoint := handler.AuthMiddleware(parser, log)(router)

	req := httptest.NewRequest(
		http.MethodGet,
		"/teams/"+teamID.String()+"/stats",
		nil,
	)
	req.Header.Set("Authorization", "Bearer test-token")

	rec := httptest.NewRecorder()
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Errorf("service calls = %d, want 1", calls)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body struct {
		TeamID        uuid.UUID `json:"team_id"`
		TasksByStatus struct {
			Todo       int `json:"todo"`
			InProgress int `json:"in_progress"`
			Done       int `json:"done"`
		} `json:"tasks_by_status"`
		TopAssignees []struct {
			UserID      uuid.UUID `json:"user_id"`
			Name        string    `json:"name"`
			ClosedTasks int       `json:"closed_tasks"`
		} `json:"top_assignees"`
		AverageCloseSeconds *float64 `json:"average_close_seconds"`
		CommentsCount       int      `json:"comments_count"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.TeamID != teamID {
		t.Errorf("team_id = %s, want %s", body.TeamID, teamID)
	}

	if body.TasksByStatus.Todo != want.TasksByStatus.Todo {
		t.Errorf("body.TasksByStatus.Todo = %d, want %d", body.TasksByStatus.Todo, want.TasksByStatus.Todo)
	}
	if body.TasksByStatus.InProgress != want.TasksByStatus.InProgress {
		t.Errorf("body.TasksByStatus.InProgress = %d, want %d", body.TasksByStatus.InProgress, want.TasksByStatus.InProgress)
	}
	if body.TasksByStatus.Done != want.TasksByStatus.Done {
		t.Errorf("body.TasksByStatus.Done = %d, want %d", body.TasksByStatus.Done, want.TasksByStatus.Done)
	}
	if body.CommentsCount != want.CommentsCount {
		t.Errorf("comments_count = %d, want %d", body.CommentsCount, want.CommentsCount)
	}
	if body.AverageCloseSeconds == nil {
		t.Fatal("average_close_seconds must not be null or missing")
	}
	if *body.AverageCloseSeconds != averageCloseSeconds {
		t.Errorf("average_close_seconds = %f, want %f", *body.AverageCloseSeconds, averageCloseSeconds)
	}

	if body.TopAssignees == nil || len(body.TopAssignees) != len(want.TopAssignees) {
		t.Fatalf("top_assignees length = %d, want %d", len(body.TopAssignees), len(want.TopAssignees))
	}
	if body.TopAssignees[0].UserID != want.TopAssignees[0].UserID {
		t.Errorf("body.TopAssignees[0].UserID = %s, want %s", body.TopAssignees[0].UserID, want.TopAssignees[0].UserID)
	}
	if body.TopAssignees[0].Name != want.TopAssignees[0].Name {
		t.Errorf("body.TopAssignees[0].Name = %q, want %q", body.TopAssignees[0].Name, want.TopAssignees[0].Name)
	}
	if body.TopAssignees[0].ClosedTasks != want.TopAssignees[0].ClosedTasks {
		t.Errorf("body.TopAssignees[0].ClosedTasks = %d, want %d", body.TopAssignees[0].ClosedTasks, want.TopAssignees[0].ClosedTasks)
	}
}

func TestStatsHandlerGetInvalidTeamID(t *testing.T) {
	actorID := uuid.New()

	svc := statsServiceFunc(func(
		ctx context.Context,
		gotActorID, gotTeamID uuid.UUID,
	) (*domain.TeamStats, error) {
		t.Fatal("stats service must not be called")
		return nil, nil
	})

	parser := statsTokenParser(func(token string) (uuid.UUID, error) {
		if token != "test-token" {
			t.Errorf("token = %q, want test-token", token)
		}
		return actorID, nil
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.NewStatsHandler(svc, log)

	router := chi.NewRouter()
	router.Get("/teams/{team_id}/stats", h.Get)

	endpoint := handler.AuthMiddleware(parser, log)(router)

	req := httptest.NewRequest(
		http.MethodGet,
		"/teams/not-a-uuid/stats",
		nil,
	)
	req.Header.Set("Authorization", "Bearer test-token")

	rec := httptest.NewRecorder()
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error != "invalid input" {
		t.Errorf("error = %q, want invalid input", body.Error)
	}
}

func TestStatsHandlerGetUnauthorized(t *testing.T) {
	teamID := uuid.New()

	svc := statsServiceFunc(func(
		ctx context.Context,
		gotActorID, gotTeamID uuid.UUID,
	) (*domain.TeamStats, error) {
		t.Fatal("stats service must not be called")
		return nil, nil
	})

	parser := statsTokenParser(func(token string) (uuid.UUID, error) {
		t.Fatal("token parser must not be called without Authorization")
		return uuid.Nil, nil
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.NewStatsHandler(svc, log)

	router := chi.NewRouter()
	router.Get("/teams/{team_id}/stats", h.Get)

	endpoint := handler.AuthMiddleware(parser, log)(router)

	req := httptest.NewRequest(
		http.MethodGet,
		"/teams/"+teamID.String()+"/stats",
		nil,
	)

	rec := httptest.NewRecorder()
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("WWW-Authenticate must be Bearer")
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error != "Unauthorized" {
		t.Errorf("error = %q, want Unauthorized", body.Error)
	}
}

func TestStatsHandlerGetServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{name: "forbidden", err: domain.ErrForbidden, status: http.StatusForbidden, message: "forbidden"},
		{name: "internal error", err: errors.New("private database diagnostic"), status: http.StatusInternalServerError, message: "internal server error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			actorID, teamID := uuid.New(), uuid.New()
			calls := 0
			svc := statsServiceFunc(func(ctx context.Context, gotActorID, gotTeamID uuid.UUID) (*domain.TeamStats, error) {
				calls++
				if gotActorID != actorID || gotTeamID != teamID {
					t.Error("service received unexpected IDs")
				}
				return nil, tt.err
			})
			parser := statsTokenParser(func(token string) (uuid.UUID, error) {
				if token != "test-token" {
					t.Errorf("token = %q, want test-token", token)
				}
				return actorID, nil
			})

			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			h := handler.NewStatsHandler(svc, log)
			router := chi.NewRouter()
			router.Get("/teams/{team_id}/stats", h.Get)
			endpoint := handler.AuthMiddleware(parser, log)(router)

			req := httptest.NewRequest(http.MethodGet, "/teams/"+teamID.String()+"/stats", nil)
			req.Header.Set("Authorization", "Bearer test-token")
			rec := httptest.NewRecorder()
			endpoint.ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.status, rec.Body.String())
			}
			if calls != 1 {
				t.Errorf("service calls = %d, want 1", calls)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			var message string
			if err := json.Unmarshal(body["error"], &message); err != nil {
				t.Fatalf("decode error message: %v", err)
			}
			if message != tt.message {
				t.Errorf("error = %q, want %q", message, tt.message)
			}
			if len(body) != 1 {
				t.Errorf("unexpected fields in error response: %s", rec.Body.String())
			}
			if tt.status == http.StatusInternalServerError && strings.Contains(rec.Body.String(), tt.err.Error()) {
				t.Error("internal diagnostic leaked into response")
			}
		})
	}
}
