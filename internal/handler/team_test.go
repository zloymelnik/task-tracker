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
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type teamServiceFunc func(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error)

func (f teamServiceFunc) UpdateRole(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, domain.TeamRole) error {
	panic("unexpected UpdateRole call in create-team service stub")
}

func (f teamServiceFunc) AddMember(context.Context, uuid.UUID, uuid.UUID, service.AddTeamMemberInput) (*domain.TeamMember, error) {
	panic("unexpected AddMember call in create-team service stub")
}

func (f teamServiceFunc) Create(ctx context.Context, id uuid.UUID, input service.CreateTeamInput) (*domain.Team, error) {
	return f(ctx, id, input)
}

func (f teamServiceFunc) List(context.Context, uuid.UUID) ([]domain.Team, error) {
	panic("unexpected List call in create-team service stub")
}

type listTeamServiceFunc func(context.Context, uuid.UUID) ([]domain.Team, error)

func (f listTeamServiceFunc) UpdateRole(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, domain.TeamRole) error {
	panic("unexpected UpdateRole call in list-team service stub")
}

func (f listTeamServiceFunc) AddMember(context.Context, uuid.UUID, uuid.UUID, service.AddTeamMemberInput) (*domain.TeamMember, error) {
	panic("unexpected AddMember call in list-team service stub")
}

func (f listTeamServiceFunc) Create(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error) {
	panic("unexpected Create call in list-team service stub")
}

func (f listTeamServiceFunc) List(ctx context.Context, id uuid.UUID) ([]domain.Team, error) {
	return f(ctx, id)
}

func TestListTeams(t *testing.T) {
	userID := uuid.New()
	teams := []domain.Team{
		{ID: uuid.New(), Name: "First", CreatedBy: uuid.New(), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)},
		{ID: uuid.New(), Name: "Second", CreatedBy: userID, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)},
	}
	for _, tt := range []struct {
		name  string
		teams []domain.Team
	}{
		{"two teams", teams}, {"empty slice", []domain.Team{}}, {"nil slice", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			s := listTeamServiceFunc(func(gotCtx context.Context, id uuid.UUID) ([]domain.Team, error) {
				calls++
				if id != userID {
					t.Errorf("userID=%s, want authenticated user %s", id, userID)
				}
				cancel()
				if !errors.Is(gotCtx.Err(), context.Canceled) {
					t.Error("request cancellation was lost")
				}
				return tt.teams, nil
			})
			parser := tokenParserFunc(func(string) (uuid.UUID, error) { return userID, nil })
			endpoint := handler.AuthMiddleware(parser, silentLogger())(http.HandlerFunc(handler.NewTeamHandler(s, silentLogger()).List))
			req := httptest.NewRequest(http.MethodGet, "/teams?user_id="+uuid.New().String(), nil).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer test-token")
			rec := httptest.NewRecorder()
			endpoint.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || calls != 1 {
				t.Fatalf("status=%d calls=%d, want 200/1", rec.Code, calls)
			}
			assertJSONContentType(t, rec)
			var got []handler.CreateTeamResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got == nil || len(got) != len(tt.teams) {
				t.Fatalf("response=%s, want array of %d teams", rec.Body.String(), len(tt.teams))
			}
			for i, want := range tt.teams {
				if got[i].ID != want.ID || got[i].Name != want.Name || got[i].CreatedBy != want.CreatedBy || !got[i].CreatedAt.Equal(want.CreatedAt) {
					t.Errorf("team[%d]=%+v, want fields of %+v", i, got[i], want)
				}
			}
		})
	}
}

func TestListTeamsErrors(t *testing.T) {
	for _, tt := range []struct {
		name       string
		authorized bool
		serviceErr error
		status     int
	}{
		{"missing identity", false, nil, 401},
		{"invalid input", true, fmt.Errorf("list: %w", domain.ErrInvalidInput), 400},
		{"database error", true, errors.New("private list diagnostic"), 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			s := listTeamServiceFunc(func(context.Context, uuid.UUID) ([]domain.Team, error) {
				calls++
				return nil, tt.serviceErr
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			var endpoint http.Handler = http.HandlerFunc(handler.NewTeamHandler(s, log).List)
			req := httptest.NewRequest(http.MethodGet, "/teams", nil)
			wantCalls := 0
			if tt.authorized {
				parser := tokenParserFunc(func(string) (uuid.UUID, error) { return uuid.New(), nil })
				endpoint = handler.AuthMiddleware(parser, log)(endpoint)
				req.Header.Set("Authorization", "Bearer test-token")
				wantCalls = 1
			}
			rec := httptest.NewRecorder()
			endpoint.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, tt.status)
			if calls != wantCalls {
				t.Errorf("service calls=%d, want %d", calls, wantCalls)
			}
			if tt.status == 500 && (strings.Contains(rec.Body.String(), tt.serviceErr.Error()) || !strings.Contains(logs.String(), tt.serviceErr.Error())) {
				t.Error("internal error must be logged and hidden from client")
			}
		})
	}
}

// Authorize requests without depending on JWT encoding in handler unit tests.
func authorizedTeamHandler(s handler.TeamService, id uuid.UUID, log *slog.Logger) http.Handler {
	parser := tokenParserFunc(func(string) (uuid.UUID, error) { return id, nil })
	return handler.AuthMiddleware(parser, log)(http.HandlerFunc(handler.NewTeamHandler(s, log).Create))
}

func teamRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/teams", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

func TestCreateTeamSuccess(t *testing.T) {
	creator := uuid.New()
	want := domain.Team{
		ID: uuid.New(), Name: "Команда", CreatedBy: creator,
		CreatedAt: time.Date(2026, 11, 2, 10, 0, 0, 123000000, time.UTC),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	s := teamServiceFunc(func(gotCtx context.Context, id uuid.UUID, input service.CreateTeamInput) (*domain.Team, error) {
		calls++
		if id != creator || input.Name != "  Команда  " {
			t.Errorf("creator=%s input=%+v, want authenticated creator and original name", id, input)
		}
		if got, ok := handler.UserIDFromContext(gotCtx); !ok || got != creator {
			t.Error("service did not receive authenticated request context")
		}
		cancel()
		if !errors.Is(gotCtx.Err(), context.Canceled) {
			t.Error("request cancellation was lost")
		}
		return &want, nil
	})
	rec := httptest.NewRecorder()
	// A client-supplied creator must not override the authenticated user.
	body := fmt.Sprintf(`{"name":"  Команда  ","created_by":%q}`, uuid.New().String())
	authorizedTeamHandler(s, creator, silentLogger()).ServeHTTP(rec, teamRequest(body+" \n\t").WithContext(ctx))
	if rec.Code != http.StatusCreated || calls != 1 {
		t.Fatalf("status=%d calls=%d, want 201 and 1", rec.Code, calls)
	}
	assertJSONContentType(t, rec)
	var got handler.CreateTeamResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Name != want.Name || got.CreatedBy != creator || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("response=%+v, want fields of %+v", got, want)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 {
		t.Errorf("response fields=%v, want exactly four public fields", fields)
	}
}

func TestCreateTeamMissingIdentity(t *testing.T) {
	s := teamServiceFunc(func(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error) {
		t.Fatal("missing identity must not reach service")
		return nil, nil
	})
	rec := httptest.NewRecorder()
	handler.NewTeamHandler(s, silentLogger()).Create(rec, teamRequest(`{"name":"Team"}`))
	assertErrorResponse(t, rec, http.StatusUnauthorized)
}

func TestCreateTeamInvalidJSON(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
	}{
		{"empty", "", 400},
		{"malformed", `{"name":`, 400},
		{"null", "null", 400},
		{"array", "[]", 400},
		{"wrong field type", `{"name":123}`, 400},
		{"second object", `{"name":"Team"} {}`, 400},
		{"trailing garbage", `{"name":"Team"} garbage`, 400},
		{"oversized object", `{"name":"` + strings.Repeat("a", 64<<10) + `"}`, 413},
		{"oversized whitespace", `{"name":"Team"}` + strings.Repeat(" ", 64<<10), 413},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := teamServiceFunc(func(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error) {
				t.Fatal("invalid JSON must not reach service")
				return nil, nil
			})
			rec := httptest.NewRecorder()
			authorizedTeamHandler(s, uuid.New(), silentLogger()).ServeHTTP(rec, teamRequest(tt.body))
			assertErrorResponse(t, rec, tt.status)
		})
	}
}

func TestCreateTeamServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		err    error
		status int
	}{
		{"invalid name", fmt.Errorf("create team: %w", domain.ErrInvalidInput), 400},
		{"database failure", errors.New("private database diagnostic"), 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			s := teamServiceFunc(func(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error) {
				calls++
				return nil, tt.err
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			rec := httptest.NewRecorder()
			authorizedTeamHandler(s, uuid.New(), log).ServeHTTP(rec, teamRequest(`{"name":"Team"}`))
			assertErrorResponse(t, rec, tt.status)
			if calls != 1 {
				t.Errorf("service calls=%d, want 1", calls)
			}
			if tt.status == 500 {
				if strings.Contains(rec.Body.String(), tt.err.Error()) {
					t.Error("database diagnostic leaked into response")
				}
				if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), tt.err.Error()) {
					t.Errorf("want one log containing original error, got %s", logs.String())
				}
			}
		})
	}
}

func TestCreateTeamMarshalFailure(t *testing.T) {
	s := teamServiceFunc(func(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error) {
		return &domain.Team{CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
	})
	rec := httptest.NewRecorder()
	authorizedTeamHandler(s, uuid.New(), silentLogger()).ServeHTTP(rec, teamRequest(`{"name":"Team"}`))
	assertErrorResponse(t, rec, http.StatusInternalServerError)
}

func TestCreateTeamWriteError(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprintf("authenticated=%v", authenticated), func(t *testing.T) {
			calls := 0
			s := teamServiceFunc(func(context.Context, uuid.UUID, service.CreateTeamInput) (*domain.Team, error) {
				calls++
				return &domain.Team{Name: "Team"}, nil
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			w := &failingAuthResponseWriter{header: make(http.Header), err: errors.New("connection closed")}
			var endpoint http.Handler = http.HandlerFunc(handler.NewTeamHandler(s, log).Create)
			wantStatus, wantCalls := http.StatusUnauthorized, 0
			if authenticated {
				endpoint = authorizedTeamHandler(s, uuid.New(), log)
				wantStatus, wantCalls = http.StatusCreated, 1
			}
			endpoint.ServeHTTP(w, teamRequest(`{"name":"Team"}`))
			if w.status != wantStatus || w.writes != 1 || calls != wantCalls {
				t.Errorf("status=%d writes=%d calls=%d, want %d/1/%d", w.status, w.writes, calls, wantStatus, wantCalls)
			}
			if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), w.err.Error()) {
				t.Errorf("want one write error log, got %s", logs.String())
			}
		})
	}
}
