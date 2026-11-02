package mysql_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

func TestTeamMemberUpdateRole(t *testing.T) {
	db := newTeamTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := mysql.NewTeamMemberRepository(db, log, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	team, user := uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	seed := []domain.TeamMember{
		{TeamID: team, UserID: user, Role: domain.MemberRole, JoinedAt: now},
		{TeamID: team, UserID: uuid.New(), Role: domain.MemberRole, JoinedAt: now},
		{TeamID: uuid.New(), UserID: user, Role: domain.MemberRole, JoinedAt: now},
	}
	for i := range seed {
		if err := repo.Save(ctx, &seed[i]); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want domain.TeamRole) {
		t.Helper()
		for i, original := range seed {
			got, err := repo.GetByTeamAndUserID(ctx, original.TeamID, original.UserID)
			if err != nil || got == nil {
				t.Fatalf("read membership=(%v,%v)", got, err)
			}
			expected := original
			if i == 0 {
				expected.Role = want
			}
			if *got != expected {
				t.Errorf("membership=%+v, want %+v", got, expected)
			}
		}
	}
	for _, role := range []domain.TeamRole{domain.AdminRole, domain.AdminRole, domain.MemberRole} {
		if err := repo.UpdateRole(ctx, team, user, role); err != nil {
			t.Fatalf("UpdateRole(%s): %v", role, err)
		}
		check(role)
	}
	manager := mysql.NewTxManager(db)
	rollbackErr := errors.New("force role rollback")
	err := manager.Do(ctx, func(txCtx context.Context) error {
		if _, err := repo.GetByTeamAndUserIDForUpdate(txCtx, team, user); err != nil {
			return err
		}
		if err := repo.UpdateRole(txCtx, team, user, domain.AdminRole); err != nil {
			return err
		}
		got, err := repo.GetByTeamAndUserID(txCtx, team, user)
		if err != nil {
			return err
		}
		if got.Role != domain.AdminRole {
			t.Error("role update not visible inside transaction")
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("rollback error=%v, want sentinel", err)
	}
	check(domain.MemberRole)
	err = manager.Do(ctx, func(txCtx context.Context) error { return repo.UpdateRole(txCtx, team, user, domain.AdminRole) })
	if err != nil {
		t.Fatalf("commit role update: %v", err)
	}
	check(domain.AdminRole)
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := repo.UpdateRole(canceled, team, user, domain.MemberRole); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled update=%v, want context.Canceled", err)
	}
	check(domain.AdminRole)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateRole(ctx, team, user, domain.MemberRole); err == nil {
		t.Error("closed database update must fail")
	}
}
