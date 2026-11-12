package app_test

import (
	"bytes"
	"context"
	"encoding/json"
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

type createTeamServiceFunc func(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error)

func (f createTeamServiceFunc) UpdateRole(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, domain.TeamRole) error {
	panic("unexpected UpdateRole call in create-team route stub")
}

func (f createTeamServiceFunc) AddMember(context.Context, uuid.UUID, uuid.UUID, service.AddTeamMemberInput) (*domain.TeamMember, error) {
	panic("unexpected AddMember call in create-team route stub")
}

func (f createTeamServiceFunc) Create(ctx context.Context, id uuid.UUID, input service.CreateTeamInput) (*domain.Team, error) {
	return f(ctx, id, input)
}

func (f createTeamServiceFunc) List(context.Context, uuid.UUID, domain.TeamFilter) ([]domain.Team, int, error) {
	panic("unexpected List call in create-team route stub")
}

type listTeamServiceFunc func(context.Context, uuid.UUID, domain.TeamFilter) ([]domain.Team, int, error)

func (f listTeamServiceFunc) UpdateRole(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, domain.TeamRole) error {
	panic("unexpected UpdateRole call in list-team route stub")
}

func (f listTeamServiceFunc) AddMember(context.Context, uuid.UUID, uuid.UUID, service.AddTeamMemberInput) (*domain.TeamMember, error) {
	panic("unexpected AddMember call in list-team route stub")
}

func (f listTeamServiceFunc) Create(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error) {
	panic("unexpected Create call in list-team route stub")
}

func (f listTeamServiceFunc) List(ctx context.Context, id uuid.UUID, filter domain.TeamFilter) ([]domain.Team, int, error) {
	return f(ctx, id, filter)
}

func TestListTeamsRoute(t *testing.T) {
	const secret = "test-only-list-route-secret-32-bytes"
	userID := uuid.New()
	for _, tokenKind := range []string{"valid", "missing", "malformed", "expired"} {
		t.Run(tokenKind, func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
			parser := service.NewAuthService(config.Auth{JWTSecret: secret}, nil, log)
			calls := 0
			s := listTeamServiceFunc(func(ctx context.Context, id uuid.UUID, filter domain.TeamFilter) ([]domain.Team, int, error) {
				calls++
				if id != userID || filter.UserID != nil || filter.Limit != 5 || filter.Offset != 10 {
					t.Errorf("userID=%s, want %s", id, userID)
				}
				return []domain.Team{}, 42, nil
			})
			h := &handler.Handler{Auth: handler.NewAuthHandler(parser, log), Team: handler.NewTeamHandler(s, log)}
			router := app.NewRouter(h, false, parser, log)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/teams?limit=5&offset=10&user_id="+uuid.NewString(), nil)
			if tokenKind != "missing" {
				token := "invalid-token"
				if tokenKind != "malformed" {
					expires := time.Now().Add(time.Hour)
					if tokenKind == "expired" {
						expires = time.Now().Add(-time.Hour)
					}
					var err error
					token, err = jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": userID.String(), "exp": expires.Unix()}).SignedString([]byte(secret))
					if err != nil {
						t.Fatal(err)
					}
				}
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if tokenKind == "valid" {
				if rec.Code != http.StatusOK || calls != 1 || strings.TrimSpace(rec.Body.String()) != `{"items":[],"total":42,"limit":5,"offset":10}` {
					t.Errorf("status=%d calls=%d body=%s, want 200/1 and page with total=42 limit=5 offset=10", rec.Code, calls, rec.Body.String())
				}
			} else if rec.Code != http.StatusUnauthorized || calls != 0 || rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("status=%d calls=%d, want 401/0 and Bearer challenge", rec.Code, calls)
			}
		})
	}
}

func TestCreateTeamRoute(t *testing.T) {
	const secret = "test-only-team-route-secret-32-bytes"
	creator := uuid.New()
	for _, tt := range []struct {
		name      string
		tokenKind string
		status    int
		wantCalls int
	}{
		{"authenticated", "valid", 201, 1},
		{"no token", "missing", 401, 0},
		{"malformed token", "malformed", 401, 0},
		{"wrong signature", "wrong key", 401, 0},
		{"expired token", "expired", 401, 0},
		{"missing expiration", "no exp", 401, 0},
		{"wrong algorithm", "HS384", 401, 0},
		{"service panic", "panic", 500, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&logs, nil))
			parser := service.NewAuthService(config.Auth{JWTSecret: secret, JWTTtl: 5 * time.Minute}, nil, log)
			calls := 0
			teamID := uuid.New()
			s := createTeamServiceFunc(func(ctx context.Context, id uuid.UUID, input service.CreateTeamInput) (*domain.Team, error) {
				calls++
				if id != creator || input.Name != "Team" {
					t.Errorf("creator=%s input=%+v, want authenticated creator and Team", id, input)
				}
				if got, ok := handler.UserIDFromContext(ctx); !ok || got != creator {
					t.Error("authenticated identity missing from service context")
				}
				if tt.tokenKind == "panic" {
					panic("private team panic")
				}
				return &domain.Team{ID: teamID, Name: input.Name, CreatedBy: id, CreatedAt: time.Now().UTC()}, nil
			})
			h := &handler.Handler{
				Auth: handler.NewAuthHandler(parser, log),
				Team: handler.NewTeamHandler(s, log),
			}
			router := app.NewRouter(h, false, parser, log)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/teams", strings.NewReader(`{"name":"Team"}`))
			if tt.tokenKind != "missing" {
				token := "invalid-token"
				if tt.tokenKind != "malformed" {
					claims := jwt.MapClaims{"sub": creator.String(), "exp": time.Now().Add(time.Hour).Unix()}
					key := secret
					method := jwt.SigningMethodHS256
					switch tt.tokenKind {
					case "wrong key":
						key = "different-test-key"
					case "expired":
						claims["exp"] = time.Now().Add(-time.Hour).Unix()
					case "no exp":
						delete(claims, "exp")
					case "HS384":
						method = jwt.SigningMethodHS384
					}
					var err error
					token, err = jwt.NewWithClaims(method, claims).SignedString([]byte(key))
					if err != nil {
						t.Fatalf("prepare token: %v", err)
					}
				}
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.status || calls != tt.wantCalls {
				t.Fatalf("status=%d service calls=%d, want %d/%d", rec.Code, calls, tt.status, tt.wantCalls)
			}
			if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("X-Request-ID") == "" {
				t.Errorf("missing JSON content type or request ID: %v", rec.Header())
			}
			if tt.status == 201 {
				var body handler.CreateTeamResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.ID != teamID || body.CreatedBy != creator {
					t.Errorf("unexpected team response: %+v", body)
				}
			} else {
				var body map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
					t.Errorf("invalid error response: %s, error=%v", rec.Body.String(), err)
				}
				if strings.Contains(rec.Body.String(), "private team panic") {
					t.Error("panic diagnostic leaked into response")
				}
			}
			if tt.status == 401 && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Error("unauthorized response missing Bearer challenge")
			}
			// Verify Logger observes the status produced by AuthMiddleware or Recovery.
			completed := 0
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var entry struct {
					Message   string `json:"msg"`
					Status    int    `json:"status"`
					RequestID string `json:"request_id"`
				}
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				if entry.Message == "request completed" {
					completed++
					if entry.Status != tt.status || entry.RequestID != rec.Header().Get("X-Request-ID") {
						t.Errorf("incorrect completion log: %+v", entry)
					}
				}
			}
			if completed != 1 {
				t.Errorf("completion logs=%d, want 1", completed)
			}
		})
	}
}
