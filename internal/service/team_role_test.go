package service_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

func TestUpdateRole(t *testing.T) {
	storageErr := errors.New("role storage failure")
	for _, tt := range []struct {
		name                           string
		actorRole, targetRole, newRole domain.TeamRole
		failAt                         string
		want                           error
		wantCalls                      []string
	}{
		{"promote", domain.OwnerRole, domain.MemberRole, domain.AdminRole, "", nil, []string{"begin", "actor", "target", "update", "commit"}},
		{"demote", domain.OwnerRole, domain.AdminRole, domain.MemberRole, "", nil, []string{"begin", "actor", "target", "update", "commit"}},
		{"same role", domain.OwnerRole, domain.MemberRole, domain.MemberRole, "", nil, []string{"begin", "actor", "target", "update", "commit"}},
		{"admin denied", domain.AdminRole, domain.MemberRole, domain.AdminRole, "", domain.ErrForbidden, []string{"begin", "actor"}},
		{"member denied", domain.MemberRole, domain.MemberRole, domain.AdminRole, "", domain.ErrForbidden, []string{"begin", "actor"}},
		{"owner protected", domain.OwnerRole, domain.OwnerRole, domain.MemberRole, "", domain.ErrForbidden, []string{"begin", "actor", "target"}},
		{"missing actor", domain.OwnerRole, domain.MemberRole, domain.AdminRole, "missing actor", domain.ErrForbidden, []string{"begin", "actor"}},
		{"missing target", domain.OwnerRole, domain.MemberRole, domain.AdminRole, "missing target", domain.ErrTeamMemberNotFound, []string{"begin", "actor", "target"}},
		{"begin failure", domain.OwnerRole, domain.MemberRole, domain.AdminRole, "begin", storageErr, []string{"begin"}},
		{"actor query failure", domain.OwnerRole, domain.MemberRole, domain.AdminRole, "actor", storageErr, []string{"begin", "actor"}},
		{"target query failure", domain.OwnerRole, domain.MemberRole, domain.AdminRole, "target", storageErr, []string{"begin", "actor", "target"}},
		{"update failure", domain.OwnerRole, domain.MemberRole, domain.AdminRole, "update", storageErr, []string{"begin", "actor", "target", "update"}},
		{"commit failure", domain.OwnerRole, domain.MemberRole, domain.AdminRole, "commit", storageErr, []string{"begin", "actor", "target", "update", "commit"}},
		{"canceled", domain.OwnerRole, domain.MemberRole, domain.AdminRole, "canceled", context.Canceled, []string{"begin"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.failAt == "canceled" {
				cancel()
			}
			txCtx, stop := context.WithCancel(ctx)
			defer stop()
			actorID, teamID, userID := uuid.New(), uuid.New(), uuid.New()
			var calls []string
			repo := memberRepositoryStub{
				lock: func(got context.Context, team, user uuid.UUID) (*domain.TeamMember, error) {
					if got != txCtx || team != teamID {
						t.Error("wrong lock context or team ID")
					}
					role := tt.targetRole
					stage := "target"
					if user == actorID {
						stage = "actor"
						role = tt.actorRole
					} else if user != userID {
						t.Error("wrong target user ID")
					}
					calls = append(calls, stage)
					if tt.failAt == stage {
						return nil, storageErr
					}
					if tt.failAt == "missing "+stage {
						return nil, domain.ErrTeamMemberNotFound
					}
					return &domain.TeamMember{TeamID: team, UserID: user, Role: role}, nil
				},
				update: func(got context.Context, team, user uuid.UUID, role domain.TeamRole) error {
					calls = append(calls, "update")
					if got != txCtx || team != teamID || user != userID || role != tt.newRole {
						t.Error("wrong update context or arguments")
					}
					if tt.failAt == "update" {
						return storageErr
					}
					return nil
				},
			}
			tx := transactionManagerFunc(func(got context.Context, fn func(context.Context) error) error {
				calls = append(calls, "begin")
				if got != ctx {
					t.Error("wrong transaction context")
				}
				if err := got.Err(); err != nil {
					return err
				}
				if tt.failAt == "begin" {
					return storageErr
				}
				if err := fn(txCtx); err != nil {
					return err
				}
				calls = append(calls, "commit")
				if tt.failAt == "commit" {
					return storageErr
				}
				return nil
			})
			err := service.NewTeamService(tx, nil, repo, nil, testLogger()).UpdateRole(ctx, actorID, teamID, userID, tt.newRole)
			if !errors.Is(err, tt.want) {
				t.Errorf("error=%v, want %v", err, tt.want)
			}
			if !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Errorf("calls=%v, want %v", calls, tt.wantCalls)
			}
		})
	}
}

func TestUpdateRoleInvalidInput(t *testing.T) {
	for _, name := range []string{"nil actor", "nil team", "nil target", "empty role", "owner role", "unknown role"} {
		t.Run(name, func(t *testing.T) {
			actor, team, target := uuid.New(), uuid.New(), uuid.New()
			role := domain.AdminRole
			switch name {
			case "nil actor":
				actor = uuid.Nil
			case "nil team":
				team = uuid.Nil
			case "nil target":
				target = uuid.Nil
			case "empty role":
				role = ""
			case "owner role":
				role = domain.OwnerRole
			case "unknown role":
				role = "superuser"
			}
			tx := transactionManagerFunc(func(context.Context, func(context.Context) error) error {
				t.Fatal("invalid input must not start transaction")
				return nil
			})
			err := service.NewTeamService(tx, nil, nil, nil, testLogger()).UpdateRole(context.Background(), actor, team, target, role)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("error=%v, want ErrInvalidInput", err)
			}
		})
	}
}
