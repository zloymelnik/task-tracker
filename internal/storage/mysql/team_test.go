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

func TestTeamRepositoryListByUserID(t *testing.T) {
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
	got, err := repo.ListByUserID(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Team{teams[1], teams[0], teams[2]}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListByUserID()=%+v, want ordered memberships %+v", got, want)
	}
	empty, err := repo.ListByUserID(ctx, uuid.New())
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("unknown user list=(%v, %v), want empty non-nil slice", empty, err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	got, err = repo.ListByUserID(canceled, user)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Errorf("canceled list=(%v, %v), want nil and context.Canceled", got, err)
	}
}
