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
)

func authorizedTeamList(s handler.TeamService, actor uuid.UUID, log *slog.Logger) http.Handler {
	parser := tokenParserFunc(func(string) (uuid.UUID, error) { return actor, nil })
	return handler.AuthMiddleware(parser, log)(http.HandlerFunc(handler.NewTeamHandler(s, log).List))
}

func teamListRequest(query string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams?"+query, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

func TestListTeamsPaginationQuery(t *testing.T) {
	actor := uuid.New()
	for _, tt := range []struct {
		query         string
		limit, offset int
	}{
		{"", 20, 0}, {"limit=5&offset=10", 5, 10}, {"limit=100", 100, 0},
		{"offset=20", 20, 20}, {"limit=0&offset=0", 0, 0}, {"limit=-1&offset=-1", -1, -1},
	} {
		t.Run(tt.query, func(t *testing.T) {
			calls := 0
			s := listTeamServiceFunc(func(ctx context.Context, id uuid.UUID, filter domain.TeamFilter) ([]domain.Team, int, error) {
				calls++
				if id != actor || filter.UserID != nil || filter.Limit != tt.limit || filter.Offset != tt.offset {
					t.Errorf("wrong actor/filter: %s/%+v", id, filter)
				}
				return nil, 42, nil
			})
			rec := httptest.NewRecorder()
			authorizedTeamList(s, actor, silentLogger()).ServeHTTP(rec, teamListRequest(tt.query+"&user_id="+uuid.NewString()))
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

func TestListTeamsInvalidPaginationQuery(t *testing.T) {
	for _, query := range []string{
		"limit=", "limit=text", "limit=1.5", "limit=99999999999999999999999999",
		"offset=", "offset=text", "offset=1.5", "offset=99999999999999999999999999",
	} {
		t.Run(query, func(t *testing.T) {
			s := listTeamServiceFunc(func(context.Context, uuid.UUID, domain.TeamFilter) ([]domain.Team, int, error) {
				t.Fatal("invalid pagination reached service")
				return nil, 0, nil
			})
			rec := httptest.NewRecorder()
			authorizedTeamList(s, uuid.New(), silentLogger()).ServeHTTP(rec, teamListRequest(query))
			assertErrorResponse(t, rec, 400)
		})
	}
}

func TestListTeamsMarshalFailure(t *testing.T) {
	s := listTeamServiceFunc(func(context.Context, uuid.UUID, domain.TeamFilter) ([]domain.Team, int, error) {
		return []domain.Team{{CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}, 1, nil
	})
	rec := httptest.NewRecorder()
	authorizedTeamList(s, uuid.New(), silentLogger()).ServeHTTP(rec, teamListRequest(""))
	assertErrorResponse(t, rec, 500)
}

func TestListTeamsWriteFailure(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprint(authenticated), func(t *testing.T) {
			calls := 0
			s := listTeamServiceFunc(func(context.Context, uuid.UUID, domain.TeamFilter) ([]domain.Team, int, error) {
				calls++
				return nil, 0, nil
			})
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			w := &failingAuthResponseWriter{header: make(http.Header), err: errors.New("connection closed")}
			var endpoint http.Handler = http.HandlerFunc(handler.NewTeamHandler(s, log).List)
			status, wantCalls := 401, 0
			if authenticated {
				endpoint = authorizedTeamList(s, uuid.New(), log)
				status, wantCalls = 200, 1
			}
			endpoint.ServeHTTP(w, teamListRequest(""))
			if w.status != status || w.writes != 1 || calls != wantCalls {
				t.Errorf("status=%d writes=%d calls=%d, want %d/1/%d", w.status, w.writes, calls, status, wantCalls)
			}
			if strings.Count(logs.String(), "level=ERROR") != 1 || !strings.Contains(logs.String(), w.err.Error()) {
				t.Errorf("expected one write error log: %s", logs.String())
			}
		})
	}
}
