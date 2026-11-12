package handler_test

import (
	"bytes"
	"context"
	"errors"
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

type updateRoleServiceFunc func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, domain.TeamRole) error

func (f updateRoleServiceFunc) Create(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error) {
	panic("unexpected Create call in update-role stub")
}

func (f updateRoleServiceFunc) List(context.Context, uuid.UUID, domain.TeamFilter) ([]domain.Team, int, error) {
	panic("unexpected List call in update-role stub")
}

func (f updateRoleServiceFunc) AddMember(context.Context, uuid.UUID, uuid.UUID, service.AddTeamMemberInput) (*domain.TeamMember, error) {
	panic("unexpected AddMember call in update-role stub")
}

func (f updateRoleServiceFunc) UpdateRole(ctx context.Context, actor, team, user uuid.UUID, role domain.TeamRole) error {
	return f(ctx, actor, team, user, role)
}

const roleTestSecret = "test-only-role-route-secret-32-bytes"

func roleToken(t *testing.T, actor uuid.UUID, expired bool) string {
	t.Helper()
	expires := time.Now().Add(time.Hour)
	if expired {
		expires = time.Now().Add(-time.Hour)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": actor.String(), "exp": expires.Unix(),
	}).SignedString([]byte(roleTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func roleRouter(s handler.TeamService, log *slog.Logger) http.Handler {
	parser := service.NewAuthService(config.Auth{JWTSecret: roleTestSecret}, nil, log)
	h := &handler.Handler{
		Health: handler.NewHealthHandler(log),
		Auth:   handler.NewAuthHandler(parser, log),
		Team:   handler.NewTeamHandler(s, log),
	}
	return app.NewRouter(h, false, parser, log)
}

func TestUpdateRoleRouteSuccess(t *testing.T) {
	actor, team, target := uuid.New(), uuid.New(), uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	s := updateRoleServiceFunc(func(gotCtx context.Context, gotActor, gotTeam, gotTarget uuid.UUID, role domain.TeamRole) error {
		calls++
		if gotActor != actor || gotTeam != team || gotTarget != target || role != domain.AdminRole {
			t.Errorf("service arguments=%s/%s/%s/%s, want JWT actor and URL team/target", gotActor, gotTeam, gotTarget, role)
		}
		if id, ok := handler.UserIDFromContext(gotCtx); !ok || id != actor {
			t.Error("JWT identity missing from context")
		}
		cancel()
		if !errors.Is(gotCtx.Err(), context.Canceled) {
			t.Error("request cancellation lost")
		}
		return nil
	})
	// Body and query parameters must not replace the JWT actor or URL target.
	body := fmt.Sprintf(`{"role":"admin","actor_id":%q,"user_id":%q}`, target.String(), actor.String())
	path := "/api/v1/teams/" + team.String() + "/members/" + target.String() + "/role?actor_id=" + target.String()
	req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body)).WithContext(ctx)
	token := roleToken(t, actor, false)
	req.Header.Set("Authorization", "Bearer "+token)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	rec := httptest.NewRecorder()
	roleRouter(s, log).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || calls != 1 || rec.Body.Len() != 0 {
		t.Errorf("status=%d calls=%d body=%q, want 204/1/empty", rec.Code, calls, rec.Body.String())
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("missing request ID")
	}
	if strings.Contains(logs.String(), token) {
		t.Error("token leaked into logs")
	}
}

func TestUpdateRoleRouteErrors(t *testing.T) {
	actor, team, target := uuid.New(), uuid.New(), uuid.New()
	valid := roleToken(t, actor, false)
	for _, tt := range []struct {
		name, team, target, token, body string
		err                             error
		status, calls                   int
	}{
		{"no token", team.String(), target.String(), "", `{"role":"admin"}`, nil, 401, 0},
		{"invalid token", team.String(), target.String(), "bad-token", `{"role":"admin"}`, nil, 401, 0},
		{"expired token", team.String(), target.String(), roleToken(t, actor, true), `{"role":"admin"}`, nil, 401, 0},
		{"bad team ID", "bad-id", target.String(), valid, `{"role":"admin"}`, nil, 400, 0},
		{"bad target ID", team.String(), "bad-id", valid, `{"role":"admin"}`, nil, 400, 0},
		{"empty body", team.String(), target.String(), valid, "", nil, 400, 0},
		{"malformed JSON", team.String(), target.String(), valid, `{"role":`, nil, 400, 0},
		{"null", team.String(), target.String(), valid, "null", nil, 400, 0},
		{"wrong type", team.String(), target.String(), valid, `{"role":123}`, nil, 400, 0},
		{"second object", team.String(), target.String(), valid, `{"role":"admin"} {}`, nil, 400, 0},
		{"oversized body", team.String(), target.String(), valid, `{"role":"` + strings.Repeat("a", 64<<10) + `"}`, nil, 413, 0},
		{"invalid role", team.String(), target.String(), valid, `{"role":"owner"}`, fmt.Errorf("update: %w", domain.ErrInvalidInput), 400, 1},
		{"forbidden", team.String(), target.String(), valid, `{"role":"admin"}`, fmt.Errorf("update: %w", domain.ErrForbidden), 403, 1},
		{"target not found", team.String(), target.String(), valid, `{"role":"admin"}`, fmt.Errorf("update: %w", domain.ErrTeamMemberNotFound), 404, 1},
		{"storage error", team.String(), target.String(), valid, `{"role":"admin"}`, errors.New("private role diagnostic"), 500, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			s := updateRoleServiceFunc(func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, domain.TeamRole) error { calls++; return tt.err })
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/teams/"+tt.team+"/members/"+tt.target+"/role", strings.NewReader(tt.body))
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			rec := httptest.NewRecorder()
			roleRouter(s, log).ServeHTTP(rec, req)
			assertErrorResponse(t, rec, tt.status)
			if calls != tt.calls {
				t.Errorf("service calls=%d, want %d", calls, tt.calls)
			}
			if tt.status == 401 && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Error("missing Bearer challenge")
			}
			if tt.status == 500 && (strings.Contains(rec.Body.String(), tt.err.Error()) || !strings.Contains(logs.String(), tt.err.Error())) {
				t.Error("internal error must be logged and hidden from client")
			}
		})
	}
}

func TestUpdateRoleHandlerMissingIdentity(t *testing.T) {
	s := updateRoleServiceFunc(func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, domain.TeamRole) error {
		t.Fatal("request without identity must not reach service")
		return nil
	})
	rec := httptest.NewRecorder()
	handler.NewTeamHandler(s, silentLogger()).UpdateRole(rec, httptest.NewRequest(http.MethodPatch, "/", nil))
	assertErrorResponse(t, rec, http.StatusUnauthorized)
}
