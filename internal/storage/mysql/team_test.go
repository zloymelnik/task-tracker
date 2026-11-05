package mysql_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

func TestTeamRepositoryList(t *testing.T) {
	db := newTeamTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := mysql.NewTeamRepository(db, log, 5*time.Second)
	members := mysql.NewTeamMemberRepository(db, log, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	user, other := uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	teams := []domain.Team{
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000001"), Name: "Owner", CreatedBy: user, CreatedAt: now},
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000002"), Name: "Admin", CreatedBy: other, CreatedAt: now},
		{ID: uuid.New(), Name: "Member", CreatedBy: other, CreatedAt: now.Add(-time.Hour)},
		{ID: uuid.New(), Name: "Unrelated", CreatedBy: other, CreatedAt: now.Add(time.Hour)},
		{ID: uuid.New(), Name: "Creator without membership", CreatedBy: user, CreatedAt: now.Add(time.Hour)},
	}
	for i := range teams {
		if err := repo.Save(ctx, &teams[i]); err != nil {
			t.Fatalf("seed team: %v", err)
		}
		memberID := user
		role := domain.MemberRole
		switch i {
		case 0:
			role = domain.OwnerRole
		case 1:
			role = domain.AdminRole
		case 3, 4:
			memberID = other
		}
		if err := members.Save(ctx, &domain.TeamMember{TeamID: teams[i].ID, UserID: memberID, Role: role, JoinedAt: now}); err != nil {
			t.Fatalf("seed membership: %v", err)
		}
	}
	// A second member must not duplicate teams when no user filter is used.
	if err := members.Save(ctx, &domain.TeamMember{TeamID: teams[0].ID, UserID: other, Role: domain.MemberRole, JoinedAt: now}); err != nil {
		t.Fatal(err)
	}
	unknown := uuid.New()
	for _, tt := range []struct {
		name   string
		filter domain.TeamFilter
		want   []domain.Team
		total  int
	}{
		{"all memberships", domain.TeamFilter{UserID: &user, Limit: 20}, []domain.Team{teams[1], teams[0], teams[2]}, 3},
		{"first page", domain.TeamFilter{UserID: &user, Limit: 1}, []domain.Team{teams[1]}, 3},
		{"second page", domain.TeamFilter{UserID: &user, Limit: 1, Offset: 1}, []domain.Team{teams[0]}, 3},
		{"last page", domain.TeamFilter{UserID: &user, Limit: 2, Offset: 2}, []domain.Team{teams[2]}, 3},
		{"past end", domain.TeamFilter{UserID: &user, Limit: 2, Offset: 20}, []domain.Team{}, 3},
		{"unknown user", domain.TeamFilter{UserID: &unknown, Limit: 20}, []domain.Team{}, 0},
		{"no user filter", domain.TeamFilter{Limit: 1}, nil, 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := repo.List(ctx, tt.filter)
			if err != nil || got == nil || total != tt.total {
				t.Fatalf("List()=(%v,%d,%v), want total %d and non-nil list", got, total, err, tt.total)
			}
			if tt.want != nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("List()=%+v, want %+v", got, tt.want)
			}
			if tt.want == nil && len(got) != 1 {
				t.Errorf("page length=%d, want 1", len(got))
			}
		})
	}
	zero := uuid.Nil
	got, total, err := repo.List(ctx, domain.TeamFilter{UserID: &zero, Limit: 20})
	if got != nil || total != 0 || !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("zero user List()=(%v,%d,%v)", got, total, err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	got, total, err = repo.List(canceled, domain.TeamFilter{UserID: &user, Limit: 20})
	if got != nil || total != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("canceled list=(%v, %v), want nil and context.Canceled", got, err)
	}
}
