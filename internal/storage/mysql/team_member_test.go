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

func TestTeamMemberRepositoryGetByTeamAndUserID(t *testing.T) {
	db := newTeamTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := mysql.NewTeamMemberRepository(db, log, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	teamID, userID := uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, role := range []domain.TeamRole{domain.OwnerRole, domain.AdminRole, domain.MemberRole} {
		t.Run(string(role), func(t *testing.T) {
			want := domain.TeamMember{TeamID: teamID, UserID: uuid.New(), Role: role, JoinedAt: now}
			if err := repo.Save(ctx, &want); err != nil {
				t.Fatalf("seed membership: %v", err)
			}
			got, err := repo.GetByTeamAndUserID(ctx, want.TeamID, want.UserID)
			if err != nil || got == nil {
				t.Fatalf("GetByTeamAndUserID()=(%v, %v), want member", got, err)
			}
			if *got != want {
				t.Errorf("member=%+v, want %+v", got, want)
			}
		})
	}
	// A matching user in another team or matching team with another user
	// must not be returned for the requested pair.
	for _, member := range []domain.TeamMember{
		{TeamID: uuid.New(), UserID: userID, Role: domain.MemberRole, JoinedAt: now},
		{TeamID: teamID, UserID: uuid.New(), Role: domain.AdminRole, JoinedAt: now},
	} {
		if err := repo.Save(ctx, &member); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.GetByTeamAndUserID(ctx, teamID, userID)
	if got != nil || !errors.Is(err, domain.ErrTeamMemberNotFound) {
		t.Errorf("missing pair=(%v, %v), want nil and ErrTeamMemberNotFound", got, err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	got, err = repo.GetByTeamAndUserID(canceled, teamID, userID)
	if got != nil || !errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrTeamMemberNotFound) {
		t.Errorf("canceled lookup=(%v, %v), want nil and context.Canceled", got, err)
	}
}

func TestTeamMemberRepositoryReadInsideTransaction(t *testing.T) {
	db := newTeamTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := mysql.NewTeamMemberRepository(db, log, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	want := domain.TeamMember{
		TeamID: uuid.New(), UserID: uuid.New(), Role: domain.OwnerRole,
		JoinedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	rollbackErr := errors.New("force rollback after reading membership")
	err := mysql.NewTxManager(db).Do(ctx, func(txCtx context.Context) error {
		if err := repo.Save(txCtx, &want); err != nil {
			return err
		}
		got, err := repo.GetByTeamAndUserID(txCtx, want.TeamID, want.UserID)
		if err != nil {
			return err
		}
		if got == nil || *got != want {
			t.Errorf("read inside transaction=%+v, want %+v", got, want)
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("Do() error=%v, want rollback sentinel", err)
	}
	got, err := repo.GetByTeamAndUserID(ctx, want.TeamID, want.UserID)
	if got != nil || !errors.Is(err, domain.ErrTeamMemberNotFound) {
		t.Errorf("lookup after rollback=(%v, %v), want missing membership", got, err)
	}
}

func TestTeamMemberRepositoryDatabaseError(t *testing.T) {
	db := newTeamTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := mysql.NewTeamMemberRepository(db, log, 5*time.Second)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByTeamAndUserID(context.Background(), uuid.New(), uuid.New())
	if got != nil || err == nil || errors.Is(err, domain.ErrTeamMemberNotFound) {
		t.Errorf("closed database lookup=(%v, %v), want storage error, not missing membership", got, err)
	}
}
