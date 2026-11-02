package service_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type transactionManagerFunc func(context.Context, func(context.Context) error) error

func (f transactionManagerFunc) Do(ctx context.Context, fn func(context.Context) error) error {
	return f(ctx, fn)
}

type teamRepositoryFunc func(context.Context, *domain.Team) error

func (f teamRepositoryFunc) Save(ctx context.Context, team *domain.Team) error {
	return f(ctx, team)
}

func (f teamRepositoryFunc) ListByUserID(context.Context, uuid.UUID) ([]domain.Team, error) {
	panic("unexpected ListByUserID call in create-team repository stub")
}

type teamMemberRepositoryFunc func(context.Context, *domain.TeamMember) error

func (f teamMemberRepositoryFunc) UpdateRole(context.Context, uuid.UUID, uuid.UUID, domain.TeamRole) error {
	panic("unexpected UpdateRole call in create-team repository stub")
}

func (f teamMemberRepositoryFunc) GetByTeamAndUserID(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
	panic("unexpected GetByTeamAndUserID call in create-team repository stub")
}

func (f teamMemberRepositoryFunc) GetByTeamAndUserIDForUpdate(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
	panic("unexpected GetByTeamAndUserIDForUpdate call in create-team repository stub")
}

type listTeamRepositoryFunc func(context.Context, uuid.UUID) ([]domain.Team, error)

func (f listTeamRepositoryFunc) Save(context.Context, *domain.Team) error {
	panic("unexpected Save call in list-team repository stub")
}

func (f listTeamRepositoryFunc) ListByUserID(ctx context.Context, id uuid.UUID) ([]domain.Team, error) {
	return f(ctx, id)
}

func TestListTeams(t *testing.T) {
	userID := uuid.New()
	want := []domain.Team{{ID: uuid.New(), Name: "Team", CreatedBy: uuid.New()}}
	for _, tt := range []struct {
		name    string
		id      uuid.UUID
		teams   []domain.Team
		err     error
		wantErr error
	}{
		{"success", userID, want, nil, nil},
		{"empty", userID, []domain.Team{}, nil, nil},
		{"nil user", uuid.Nil, nil, nil, domain.ErrInvalidInput},
		{"database error", userID, nil, context.DeadlineExceeded, context.DeadlineExceeded},
		{"canceled", userID, nil, context.Canceled, context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if errors.Is(tt.err, context.Canceled) {
				cancel()
			}
			calls := 0
			repo := listTeamRepositoryFunc(func(gotCtx context.Context, id uuid.UUID) ([]domain.Team, error) {
				calls++
				if gotCtx != ctx || id != tt.id {
					t.Error("repository received wrong context or user ID")
				}
				if err := gotCtx.Err(); err != nil {
					return nil, err
				}
				return tt.teams, tt.err
			})
			got, err := service.NewTeamService(nil, repo, nil, nil, testLogger()).List(ctx, tt.id)
			if !errors.Is(err, tt.wantErr) || !reflect.DeepEqual(got, tt.teams) {
				t.Errorf("List()=(%v, %v), want (%v, %v)", got, err, tt.teams, tt.wantErr)
			}
			wantCalls := 1
			if tt.id == uuid.Nil {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Errorf("repository calls=%d, want %d", calls, wantCalls)
			}
		})
	}
}

func (f teamMemberRepositoryFunc) Save(ctx context.Context, member *domain.TeamMember) error {
	return f(ctx, member)
}

func TestCreateTeam(t *testing.T) {
	for _, name := range []string{" \tКоманда\n", strings.Repeat("я", 255)} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			txCtx, txCancel := context.WithCancel(ctx)
			defer txCancel()
			creator := uuid.New()
			var savedTeam domain.Team
			var savedMember domain.TeamMember
			var calls []string
			tx := transactionManagerFunc(func(gotCtx context.Context, fn func(context.Context) error) error {
				if gotCtx != ctx {
					t.Error("transaction manager received a different context")
				}
				calls = append(calls, "begin")
				if err := fn(txCtx); err != nil {
					return err
				}
				calls = append(calls, "commit")
				return nil
			})
			teams := teamRepositoryFunc(func(gotCtx context.Context, team *domain.Team) error {
				if gotCtx != txCtx {
					t.Error("team repository did not receive transaction context")
				}
				calls = append(calls, "team")
				savedTeam = *team
				return nil
			})
			members := teamMemberRepositoryFunc(func(gotCtx context.Context, member *domain.TeamMember) error {
				if gotCtx != txCtx {
					t.Error("member repository did not receive transaction context")
				}
				calls = append(calls, "member")
				savedMember = *member
				return nil
			})
			before := time.Now().UTC().Truncate(time.Millisecond)
			got, err := service.NewTeamService(tx, teams, members, nil, testLogger()).Create(ctx, creator, service.CreateTeamInput{Name: name})
			after := time.Now().UTC()
			if err != nil || got == nil {
				t.Fatalf("Create()=(%v, %v), want team and nil error", got, err)
			}
			if !reflect.DeepEqual(calls, []string{"begin", "team", "member", "commit"}) {
				t.Errorf("calls=%v, want begin/team/member/commit", calls)
			}
			if *got != savedTeam || got.ID == uuid.Nil || got.ID.Version() != 4 || got.CreatedBy != creator || got.Name != strings.TrimSpace(name) {
				t.Errorf("unexpected team: %+v; saved=%+v", got, savedTeam)
			}
			if got.CreatedAt.Before(before) || got.CreatedAt.After(after) || got.CreatedAt.Location() != time.UTC || got.CreatedAt.Nanosecond()%int(time.Millisecond) != 0 {
				t.Errorf("CreatedAt=%v, want current UTC time with millisecond precision", got.CreatedAt)
			}
			wantMember := domain.TeamMember{TeamID: got.ID, UserID: creator, Role: domain.OwnerRole, JoinedAt: got.CreatedAt}
			if savedMember != wantMember {
				t.Errorf("owner=%+v, want %+v", savedMember, wantMember)
			}
		})
	}
}

func TestCreateTeamInvalidInput(t *testing.T) {
	for _, tt := range []struct {
		name    string
		creator uuid.UUID
	}{
		{"", uuid.New()},
		{" \t\n", uuid.New()},
		{strings.Repeat("я", 256), uuid.New()},
		{"Team", uuid.Nil},
	} {
		t.Run(tt.name+"/"+tt.creator.String(), func(t *testing.T) {
			tx := transactionManagerFunc(func(context.Context, func(context.Context) error) error {
				t.Fatal("invalid input must not start a transaction")
				return nil
			})
			teams := teamRepositoryFunc(func(context.Context, *domain.Team) error {
				t.Fatal("invalid input must not save a team")
				return nil
			})
			members := teamMemberRepositoryFunc(func(context.Context, *domain.TeamMember) error {
				t.Fatal("invalid input must not save a member")
				return nil
			})
			got, err := service.NewTeamService(tx, teams, members, nil, testLogger()).Create(context.Background(), tt.creator, service.CreateTeamInput{Name: tt.name})
			if got != nil || !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("Create()=(%v, %v), want nil and ErrInvalidInput", got, err)
			}
		})
	}
}

func TestCreateTeamErrors(t *testing.T) {
	for _, stage := range []string{"begin", "team", "member", "commit", "canceled context"} {
		t.Run(stage, func(t *testing.T) {
			want := errors.New(stage + " failure")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if stage == "canceled context" {
				cancel()
				want = context.Canceled
			}
			var calls []string
			tx := transactionManagerFunc(func(gotCtx context.Context, fn func(context.Context) error) error {
				calls = append(calls, "begin")
				if gotCtx != ctx {
					t.Error("transaction manager received a different context")
				}
				if err := gotCtx.Err(); err != nil {
					return err
				}
				if stage == "begin" {
					return want
				}
				if err := fn(gotCtx); err != nil {
					return err
				}
				calls = append(calls, "commit")
				return want
			})
			teams := teamRepositoryFunc(func(context.Context, *domain.Team) error {
				calls = append(calls, "team")
				if stage == "team" {
					return want
				}
				return nil
			})
			members := teamMemberRepositoryFunc(func(context.Context, *domain.TeamMember) error {
				calls = append(calls, "member")
				if stage == "member" {
					return want
				}
				return nil
			})
			got, err := service.NewTeamService(tx, teams, members, nil, testLogger()).Create(ctx, uuid.New(), service.CreateTeamInput{Name: "Team"})
			if got != nil || !errors.Is(err, want) {
				t.Errorf("Create()=(%v, %v), want nil and wrapped %v", got, err, want)
			}
			wantCalls := map[string][]string{
				"begin": {"begin"}, "canceled context": {"begin"},
				"team": {"begin", "team"}, "member": {"begin", "team", "member"},
				"commit": {"begin", "team", "member", "commit"},
			}[stage]
			if !reflect.DeepEqual(calls, wantCalls) {
				t.Errorf("calls=%v, want %v", calls, wantCalls)
			}
		})
	}
}
