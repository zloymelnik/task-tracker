package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

type statsRouteFunc func(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*domain.TeamStats, error)

func (f statsRouteFunc) Get(
	ctx context.Context,
	actorID, teamID uuid.UUID,
) (*domain.TeamStats, error) {
	return f(ctx, actorID, teamID)
}

func TestStatsRouteSuccess(t *testing.T) {
	actorID, teamID := uuid.New(), uuid.New()
	want := domain.TeamStats{TeamID: teamID}

	calls := 0
	svc := statsRouteFunc(func(
		ctx context.Context,
		gotActorID, gotTeamID uuid.UUID,
	) (*domain.TeamStats, error) {
		calls++
		if gotActorID != actorID || gotTeamID != teamID {
			t.Error("service received unexpected IDs")
		}
		return &want, nil
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	const secret = "test-only-stats-route-secret"
	parser := service.NewAuthService(
		config.Auth{JWTSecret: secret},
		nil,
		log,
	)

	h := handler.New(&service.Service{Auth: parser}, log)
	h.Stats = handler.NewStatsHandler(svc, log)

	router := app.NewRouter(h, parser, log)

	token, err := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		jwt.MapClaims{
			"sub": actorID.String(),
			"exp": time.Now().Add(time.Hour).Unix(),
		},
	).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/teams/"+teamID.String()+"/stats",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Errorf("service calls = %d, want 1", calls)
	}

	var body struct {
		TeamID uuid.UUID `json:"team_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.TeamID != teamID {
		t.Errorf("team_id = %s, want %s", body.TeamID, teamID)
	}
}

func TestStatsRouteRejectedRequests(t *testing.T) {
	for _, tt := range []struct {
		name    string
		teamID  string
		token   bool
		status  int
		message string
	}{
		{name: "missing token", teamID: uuid.NewString(), status: http.StatusUnauthorized, message: "Unauthorized"},
		{name: "invalid team ID", teamID: "not-a-uuid", token: true, status: http.StatusBadRequest, message: "invalid input"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			const secret = "test-only-stats-route-secret"
			parser := service.NewAuthService(config.Auth{JWTSecret: secret}, nil, log)
			svc := statsRouteFunc(func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamStats, error) {
				t.Fatal("stats service must not be called for rejected request")
				return nil, nil
			})
			h := handler.New(&service.Service{Auth: parser}, log)
			h.Stats = handler.NewStatsHandler(svc, log)
			router := app.NewRouter(h, parser, log)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/"+tt.teamID+"/stats", nil)
			if tt.token {
				token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
					"sub": uuid.NewString(),
					"exp": time.Now().Add(time.Hour).Unix(),
				}).SignedString([]byte(secret))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+token)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.status, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if tt.status == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("WWW-Authenticate = %q, want Bearer", rec.Header().Get("WWW-Authenticate"))
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if body.Error != tt.message {
				t.Errorf("error = %q, want %q", body.Error, tt.message)
			}
		})
	}
}
