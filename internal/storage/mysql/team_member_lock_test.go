package mysql_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

func TestTeamMemberForUpdateRequiresTransaction(t *testing.T) {
	db := newTeamTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := mysql.NewTeamMemberRepository(db, log, time.Second)
	got, err := repo.GetByTeamAndUserIDForUpdate(context.Background(), uuid.New(), uuid.New())
	if got != nil || err == nil || errors.Is(err, domain.ErrTeamMemberNotFound) {
		t.Errorf("lookup without transaction=(%v,%v), want transaction error", got, err)
	}
	want := domain.TeamMember{TeamID: uuid.New(), UserID: uuid.New(), Role: domain.AdminRole, JoinedAt: time.Now().UTC().Truncate(time.Millisecond)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = mysql.NewTxManager(db).Do(ctx, func(txCtx context.Context) error {
		if err := repo.Save(txCtx, &want); err != nil {
			return err
		}
		got, err := repo.GetByTeamAndUserIDForUpdate(txCtx, want.TeamID, want.UserID)
		if err != nil {
			return err
		}
		if got == nil || *got != want {
			t.Errorf("locked lookup=%+v, want %+v", got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, &want); !errors.Is(err, domain.ErrTeamMemberAlreadyExists) {
		t.Errorf("duplicate Save()=%v, want ErrTeamMemberAlreadyExists", err)
	}
}

type pausedInviteUserRepository struct {
	repo   *mysql.UserRepository
	locked chan struct{}
	resume chan struct{}
}

func (r pausedInviteUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	close(r.locked)
	select {
	case <-r.resume:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.repo.GetByEmail(ctx, email)
}

func TestInviteLocksActorUntilCommit(t *testing.T) {
	db := newLockingTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	users := mysql.NewUserRepository(db, log, 5*time.Second)
	teams := mysql.NewTeamRepository(db, log, 5*time.Second)
	members := mysql.NewTeamMemberRepository(db, log, 5*time.Second)
	manager := mysql.NewTxManager(db)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	actor, candidate := testUser(), testUser()
	for _, u := range []*domain.User{actor, candidate} {
		if err := users.Save(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	base := service.NewTeamService(manager, teams, members, users, log)
	team, err := base.Create(ctx, actor.ID, service.CreateTeamInput{Name: "Lock test"})
	if err != nil {
		t.Fatal(err)
	}
	paused := pausedInviteUserRepository{repo: users, locked: make(chan struct{}), resume: make(chan struct{})}
	svc := service.NewTeamService(manager, teams, members, paused, log)
	done := make(chan error, 1)
	var release sync.Once
	var worker sync.WaitGroup
	worker.Add(1)
	go func() {
		defer worker.Done()
		_, err := svc.AddMember(ctx, actor.ID, team.ID, service.AddTeamMemberInput{Email: candidate.Email, Role: domain.MemberRole})
		done <- err
	}()
	defer func() { cancel(); release.Do(func() { close(paused.resume) }); worker.Wait() }()
	select {
	case <-paused.locked:
	case err := <-done:
		t.Fatalf("invite ended before actor lock: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	updateCtx, stop := context.WithTimeout(ctx, 250*time.Millisecond)
	_, err = db.ExecContext(updateCtx, "UPDATE team_members SET role = 'member' WHERE team_id = ? AND user_id = ?", team.ID[:], actor.ID[:])
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("concurrent role change=%v, want deadline while actor is locked", err)
	}
	release.Do(func() { close(paused.resume) })
	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("invite: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err = db.ExecContext(ctx, "UPDATE team_members SET role = 'member' WHERE team_id = ? AND user_id = ?", team.ID[:], actor.ID[:]); err != nil {
		t.Fatal(err)
	}
	got, err := members.GetByTeamAndUserID(ctx, team.ID, candidate.ID)
	if err != nil || got == nil || got.Role != domain.MemberRole {
		t.Fatalf("committed invite=(%v,%v)", got, err)
	}
	_, err = base.AddMember(ctx, actor.ID, team.ID, service.AddTeamMemberInput{Email: candidate.Email, Role: domain.MemberRole})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("invitation after demotion=%v, want ErrForbidden", err)
	}
}
