package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

func TestListTeamsInvalidPagination(t *testing.T) {
	for _, filter := range []domain.TeamFilter{
		{Limit: 0}, {Limit: -1}, {Limit: 101}, {Limit: 20, Offset: -1},
	} {
		t.Run(fmt.Sprintf("limit=%d/offset=%d", filter.Limit, filter.Offset), func(t *testing.T) {
			// Nil repository verifies validation precedes any storage call.
			got, total, err := service.NewTeamService(nil, nil, nil, nil, testLogger()).List(context.Background(), uuid.New(), filter)
			if got != nil || total != 0 || !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("List()=(%v,%d,%v), want invalid input", got, total, err)
			}
		})
	}
}

func TestListTeamsPaginationAndIdentity(t *testing.T) {
	actor, foreign := uuid.New(), uuid.New()
	zero := uuid.Nil
	for _, limit := range []int{1, 100} {
		for _, userFilter := range []*uuid.UUID{nil, &foreign, &zero} {
			t.Run(fmt.Sprintf("limit=%d/user=%v", limit, userFilter), func(t *testing.T) {
				calls := 0
				filter := domain.TeamFilter{UserID: userFilter, Limit: limit, Offset: 1000}
				repo := listTeamRepositoryFunc(func(ctx context.Context, f domain.TeamFilter) ([]domain.Team, int, error) {
					calls++
					if f.UserID == nil || *f.UserID != actor || f.Limit != limit || f.Offset != 1000 {
						t.Errorf("wrong authenticated filter: %+v", f)
					}
					return []domain.Team{}, 42, nil
				})
				got, total, err := service.NewTeamService(nil, repo, nil, nil, testLogger()).List(context.Background(), actor, filter)
				if err != nil || got == nil || len(got) != 0 || total != 42 || calls != 1 {
					t.Errorf("List()=(%v,%d,%v) calls=%d", got, total, err, calls)
				}
				if filter.UserID != userFilter {
					t.Error("caller filter was changed")
				}
			})
		}
	}
}
