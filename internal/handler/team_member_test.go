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

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/app"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type inviteServiceFunc func(context.Context, uuid.UUID, uuid.UUID, service.AddTeamMemberInput) (*domain.TeamMember, error)

func (f inviteServiceFunc) UpdateRole(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, domain.TeamRole) error {
	panic("unexpected UpdateRole call in invite service stub")
}

func (f inviteServiceFunc) Create(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error) {
	panic("unexpected Create call in invite stub")
}

func (f inviteServiceFunc) List(context.Context, uuid.UUID) ([]domain.Team, error) {
	panic("unexpected List call in invite stub")
}

func (f inviteServiceFunc) AddMember(ctx context.Context, actor, team uuid.UUID, input service.AddTeamMemberInput) (*domain.TeamMember, error) {
	return f(ctx, actor, team, input)
}

func inviteRouter(s handler.TeamService, actor uuid.UUID, log *slog.Logger) http.Handler {
	parser := tokenParserFunc(func(token string) (uuid.UUID, error) {
		if token != "valid-token" {
			return uuid.Nil, errors.New("invalid token")
		}
		return actor, nil
	})
	h := &handler.Handler{
		Health: handler.NewHealthHandler(log),
		Auth:   handler.NewAuthHandler(nil, log),
		Team:   handler.NewTeamHandler(s, log),
	}
	return app.NewRouter(h, parser, log)
}

func TestInviteTeamRouteSuccess(t *testing.T) {
	actor, teamID, userID := uuid.New(), uuid.New(), uuid.New()
	want := domain.TeamMember{TeamID: teamID, UserID: userID, Role: domain.MemberRole, JoinedAt: time.Now().UTC().Truncate(time.Millisecond)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	s := inviteServiceFunc(func(gotCtx context.Context, gotActor, gotTeam uuid.UUID, input service.AddTeamMemberInput) (*domain.TeamMember, error) {
		calls++
		if gotActor != actor || gotTeam != teamID || input != (service.AddTeamMemberInput{Email: "Candidate@Example.COM", Role: domain.MemberRole}) {
			t.Errorf("wrong service arguments: %s/%s/%+v", gotActor, gotTeam, input)
		}
		if id, ok := handler.UserIDFromContext(gotCtx); !ok || id != actor {
			t.Error("missing actor context")
		}
		cancel()
		if !errors.Is(gotCtx.Err(), context.Canceled) {
			t.Error("request cancellation was lost")
		}
		return &want, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/"+teamID.String()+"/invite", strings.NewReader(`{"email":"Candidate@Example.COM","role":"member"}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	inviteRouter(s, actor, silentLogger()).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || calls != 1 {
		t.Fatalf("status=%d calls=%d, want 201/1", rec.Code, calls)
	}
	assertJSONContentType(t, rec)
	var got handler.InviteUserResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.TeamID != teamID || got.UserID != userID || got.Role != string(want.Role) || !got.JoinedAt.Equal(want.JoinedAt) {
		t.Errorf("response=%+v, want fields of %+v", got, want)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 {
		t.Errorf("unexpected response fields: %v", fields)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("missing request ID")
	}
}

func TestInviteTeamRouteErrors(t *testing.T) {
	const body = `{"email":"candidate@example.com","role":"member"}`
	for _, tt := range []struct {
		name, token, teamID, body string
		err                       error
		status, calls             int
	}{
		{"missing token", "", uuid.New().String(), body, nil, 401, 0},
		{"invalid token", "invalid-token", uuid.New().String(), body, nil, 401, 0},
		{"invalid UUID", "valid-token", "bad-id", body, nil, 400, 0},
		{"empty body", "valid-token", uuid.New().String(), "", nil, 400, 0},
		{"malformed JSON", "valid-token", uuid.New().String(), `{"email":`, nil, 400, 0},
		{"null", "valid-token", uuid.New().String(), "null", nil, 400, 0},
		{"wrong field type", "valid-token", uuid.New().String(), `{"role":123}`, nil, 400, 0},
		{"extra JSON", "valid-token", uuid.New().String(), body + " {}", nil, 400, 0},
		{"oversized", "valid-token", uuid.New().String(), `{"email":"` + strings.Repeat("a", 64<<10) + `"}`, nil, 413, 0},
		{"invalid input", "valid-token", uuid.New().String(), body, fmt.Errorf("invite: %w", domain.ErrInvalidInput), 400, 1},
		{"forbidden", "valid-token", uuid.New().String(), body, fmt.Errorf("invite: %w", domain.ErrForbidden), 403, 1},
		{"unknown user", "valid-token", uuid.New().String(), body, fmt.Errorf("invite: %w", domain.ErrUserNotFound), 404, 1},
		{"duplicate", "valid-token", uuid.New().String(), body, fmt.Errorf("invite: %w", domain.ErrTeamMemberAlreadyExists), 409, 1},
		{"storage failure", "valid-token", uuid.New().String(), body, errors.New("private invite diagnostic"), 500, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			s := inviteServiceFunc(func(context.Context, uuid.UUID, uuid.UUID, service.AddTeamMemberInput) (*domain.TeamMember, error) {
				calls++
				return nil, tt.err
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/"+tt.teamID+"/invite", strings.NewReader(tt.body))
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()
			inviteRouter(s, uuid.New(), log).ServeHTTP(rec, req)
			assertErrorResponse(t, rec, tt.status)
			if calls != tt.calls {
				t.Errorf("service calls=%d, want %d", calls, tt.calls)
			}
			if tt.status == 401 && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Error("missing Bearer challenge")
			}
			if tt.status == 500 && (strings.Contains(rec.Body.String(), tt.err.Error()) || !strings.Contains(logs.String(), tt.err.Error())) {
				t.Error("internal diagnostic must be logged and hidden from client")
			}
		})
	}
}

func TestInviteTeamResponseErrors(t *testing.T) {
	for _, encodeError := range []bool{false, true} {
		t.Run(fmt.Sprintf("encode error=%v", encodeError), func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			s := inviteServiceFunc(func(context.Context, uuid.UUID, uuid.UUID, service.AddTeamMemberInput) (*domain.TeamMember, error) {
				member := &domain.TeamMember{JoinedAt: time.Now().UTC()}
				if encodeError {
					member.JoinedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
				}
				return member, nil
			})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/"+uuid.New().String()+"/invite", strings.NewReader(`{"email":"user@example.com","role":"member"}`))
			req.Header.Set("Authorization", "Bearer valid-token")
			if encodeError {
				rec := httptest.NewRecorder()
				inviteRouter(s, uuid.New(), log).ServeHTTP(rec, req)
				assertErrorResponse(t, rec, 500)
			} else {
				w := &failingAuthResponseWriter{header: make(http.Header), err: errors.New("write failed")}
				inviteRouter(s, uuid.New(), log).ServeHTTP(w, req)
				if w.status != 201 || w.writes != 1 {
					t.Errorf("status=%d writes=%d, want 201/1", w.status, w.writes)
				}
				if !strings.Contains(logs.String(), w.err.Error()) {
					t.Error("write error was not logged")
				}
			}
		})
	}
}
