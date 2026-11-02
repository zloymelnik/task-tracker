package service_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type memberRepositoryStub struct {
	update func(context.Context, uuid.UUID, uuid.UUID, domain.TeamRole) error
	save   func(context.Context, *domain.TeamMember) error
	get    func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error)
	lock   func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error)
}

func (r memberRepositoryStub) UpdateRole(ctx context.Context, teamID, userID uuid.UUID, role domain.TeamRole) error {
	return r.update(ctx, teamID, userID, role)
}

func (r memberRepositoryStub) Save(ctx context.Context, member *domain.TeamMember) error {
	return r.save(ctx, member)
}

func (r memberRepositoryStub) GetByTeamAndUserID(ctx context.Context, teamID, userID uuid.UUID) (*domain.TeamMember, error) {
	return r.get(ctx, teamID, userID)
}

func (r memberRepositoryStub) GetByTeamAndUserIDForUpdate(ctx context.Context, teamID, userID uuid.UUID) (*domain.TeamMember, error) {
	return r.lock(ctx, teamID, userID)
}

type memberUserRepositoryFunc func(context.Context, string) (*domain.User, error)

func (f memberUserRepositoryFunc) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return f(ctx, email)
}

func TestAddMemberSuccess(t *testing.T) {
	for _, actorRole := range []domain.TeamRole{domain.OwnerRole, domain.AdminRole} {
		for _, targetRole := range []domain.TeamRole{domain.AdminRole, domain.MemberRole} {
			t.Run(string(actorRole)+" invites "+string(targetRole), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				txCtx, txCancel := context.WithCancel(ctx)
				defer txCancel()
				actorID, teamID, candidateID := uuid.New(), uuid.New(), uuid.New()
				var calls []string
				var saved domain.TeamMember
				check := func(got context.Context) {
					if got != txCtx {
						t.Error("repository did not receive transaction context")
					}
				}
				members := memberRepositoryStub{
					lock: func(got context.Context, team, user uuid.UUID) (*domain.TeamMember, error) {
						check(got)
						calls = append(calls, "lock actor")
						if team != teamID || user != actorID {
							t.Error("wrong actor lookup IDs")
						}
						return &domain.TeamMember{TeamID: team, UserID: user, Role: actorRole}, nil
					},
					get: func(got context.Context, team, user uuid.UUID) (*domain.TeamMember, error) {
						check(got)
						calls = append(calls, "check candidate")
						if team != teamID || user != candidateID {
							t.Error("wrong candidate lookup IDs")
						}
						return nil, domain.ErrTeamMemberNotFound
					},
					save: func(got context.Context, member *domain.TeamMember) error {
						check(got)
						calls = append(calls, "save")
						saved = *member
						return nil
					},
				}
				users := memberUserRepositoryFunc(func(got context.Context, email string) (*domain.User, error) {
					check(got)
					calls = append(calls, "find user")
					if email != "candidate@example.com" {
						t.Errorf("email=%q, want normalized email", email)
					}
					return &domain.User{ID: candidateID}, nil
				})
				tx := transactionManagerFunc(func(got context.Context, fn func(context.Context) error) error {
					if got != ctx {
						t.Error("wrong transaction context")
					}
					calls = append(calls, "begin")
					if err := fn(txCtx); err != nil {
						return err
					}
					calls = append(calls, "commit")
					return nil
				})
				before := time.Now().UTC().Truncate(time.Millisecond)
				got, err := service.NewTeamService(tx, nil, members, users, testLogger()).AddMember(ctx, actorID, teamID,
					service.AddTeamMemberInput{Email: " \tCandidate@Example.COM\n", Role: targetRole})
				after := time.Now().UTC()
				if err != nil || got == nil {
					t.Fatalf("AddMember()=(%v, %v), want membership", got, err)
				}
				if *got != saved || got.TeamID != teamID || got.UserID != candidateID || got.Role != targetRole {
					t.Errorf("unexpected membership: %+v", got)
				}
				if got.JoinedAt.Before(before) || got.JoinedAt.After(after) || got.JoinedAt.Location() != time.UTC || got.JoinedAt.Nanosecond()%int(time.Millisecond) != 0 {
					t.Errorf("invalid JoinedAt: %v", got.JoinedAt)
				}
				if !reflect.DeepEqual(calls, []string{"begin", "lock actor", "find user", "check candidate", "save", "commit"}) {
					t.Errorf("unexpected operation order: %v", calls)
				}
			})
		}
	}
}

func TestAddMemberInvalidInput(t *testing.T) {
	for _, name := range []string{"nil actor", "nil team", "invalid email", "empty role", "owner role", "unknown role"} {
		t.Run(name, func(t *testing.T) {
			actor, team := uuid.New(), uuid.New()
			input := service.AddTeamMemberInput{Email: "candidate@example.com", Role: domain.MemberRole}
			switch name {
			case "nil actor":
				actor = uuid.Nil
			case "nil team":
				team = uuid.Nil
			case "invalid email":
				input.Email = "not-an-email"
			case "empty role":
				input.Role = ""
			case "owner role":
				input.Role = domain.OwnerRole
			case "unknown role":
				input.Role = "superuser"
			}
			tx := transactionManagerFunc(func(context.Context, func(context.Context) error) error {
				t.Fatal("invalid input must not start a transaction")
				return nil
			})
			got, err := service.NewTeamService(tx, nil, nil, nil, testLogger()).AddMember(context.Background(), actor, team, input)
			if got != nil || !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("got (%v, %v), want invalid input", got, err)
			}
		})
	}
}

func TestAddMemberErrors(t *testing.T) {
	storageErr := errors.New("storage failure")
	for _, tt := range []struct {
		name  string
		want  error
		steps int
	}{
		{"begin", storageErr, 0},
		{"missing actor", domain.ErrForbidden, 1},
		{"member actor", domain.ErrForbidden, 1},
		{"actor storage error", storageErr, 1},
		{"missing user", domain.ErrUserNotFound, 2},
		{"user storage error", storageErr, 2},
		{"existing membership", domain.ErrTeamMemberAlreadyExists, 3},
		{"candidate storage error", storageErr, 3},
		{"concurrent duplicate", domain.ErrTeamMemberAlreadyExists, 4},
		{"save error", storageErr, 4},
		{"commit", storageErr, 4},
		{"canceled context", context.Canceled, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.name == "canceled context" {
				cancel()
			}
			steps := 0
			members := memberRepositoryStub{
				lock: func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
					steps++
					switch tt.name {
					case "missing actor":
						return nil, domain.ErrTeamMemberNotFound
					case "actor storage error":
						return nil, storageErr
					case "member actor":
						return &domain.TeamMember{Role: domain.MemberRole}, nil
					}
					return &domain.TeamMember{Role: domain.AdminRole}, nil
				},
				get: func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
					steps++
					switch tt.name {
					case "existing membership":
						return &domain.TeamMember{}, nil
					case "candidate storage error":
						return nil, storageErr
					}
					return nil, domain.ErrTeamMemberNotFound
				},
				save: func(context.Context, *domain.TeamMember) error {
					steps++
					if tt.name == "concurrent duplicate" {
						return domain.ErrTeamMemberAlreadyExists
					}
					if tt.name == "save error" {
						return storageErr
					}
					return nil
				},
			}
			users := memberUserRepositoryFunc(func(context.Context, string) (*domain.User, error) {
				steps++
				if tt.name == "missing user" {
					return nil, domain.ErrUserNotFound
				}
				if tt.name == "user storage error" {
					return nil, storageErr
				}
				return &domain.User{ID: uuid.New()}, nil
			})
			tx := transactionManagerFunc(func(ctx context.Context, fn func(context.Context) error) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if tt.name == "begin" {
					return storageErr
				}
				if err := fn(ctx); err != nil {
					return err
				}
				if tt.name == "commit" {
					return storageErr
				}
				return nil
			})
			got, err := service.NewTeamService(tx, nil, members, users, testLogger()).AddMember(ctx, uuid.New(), uuid.New(), service.AddTeamMemberInput{Email: "user@example.com", Role: domain.MemberRole})
			if got != nil || !errors.Is(err, tt.want) || steps != tt.steps {
				t.Errorf("got (%v, %v), steps=%d; want nil, %v, steps=%d", got, err, steps, tt.want, tt.steps)
			}
		})
	}
}
