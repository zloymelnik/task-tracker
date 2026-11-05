package service_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type listTaskRepositoryFunc func(context.Context, domain.TaskFilter) ([]domain.Task, int, error)

func (f listTaskRepositoryFunc) List(ctx context.Context, filter domain.TaskFilter) ([]domain.Task, int, error) {
	return f(ctx, filter)
}

func (f listTaskRepositoryFunc) Save(context.Context, *domain.Task) error {
	panic("unexpected Save in list-task test")
}

type listTaskMembershipFunc func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error)

func (f listTaskMembershipFunc) GetByTeamAndUserID(ctx context.Context, team, user uuid.UUID) (*domain.TeamMember, error) {
	return f(ctx, team, user)
}

func (f listTaskMembershipFunc) GetByTeamAndUserIDForUpdate(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
	panic("list must not lock membership")
}

func TestListTasks(t *testing.T) {
	for _, role := range []domain.TeamRole{domain.OwnerRole, domain.AdminRole, domain.MemberRole} {
		for _, status := range []domain.TaskStatus{"", domain.Todo, domain.InProgress, domain.Done} {
			t.Run(string(role)+"/"+string(status), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				actor, team, assignee := uuid.New(), uuid.New(), uuid.New()
				filter := domain.TaskFilter{TeamID: &team, Limit: 1, Offset: 20}
				if status != "" {
					filter.Status, filter.AssigneeID = &status, &assignee
				}
				var calls []string
				members := listTaskMembershipFunc(func(got context.Context, tid, uid uuid.UUID) (*domain.TeamMember, error) {
					calls = append(calls, "membership")
					if got != ctx || tid != team || uid != actor {
						t.Error("wrong context or membership identity")
					}
					return &domain.TeamMember{Role: role}, nil
				})
				want := []domain.Task{{ID: uuid.New(), TeamID: team}}
				repo := listTaskRepositoryFunc(func(got context.Context, f domain.TaskFilter) ([]domain.Task, int, error) {
					calls = append(calls, "list")
					if got != ctx || !reflect.DeepEqual(f, filter) {
						t.Errorf("request context or filter lost: %+v", f)
					}
					return want, 41, nil
				})
				got, total, err := service.NewTaskService(nil, repo, nil, members, testLogger()).List(ctx, actor, filter)
				if err != nil || total != 41 || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(calls, []string{"membership", "list"}) {
					t.Errorf("List()=(%v,%d,%v), calls=%v", got, total, err, calls)
				}
			})
		}
	}
}

func TestListTasksInvalidInput(t *testing.T) {
	for _, name := range []string{"zero actor", "missing team", "zero team", "empty status", "unknown status", "zero assignee", "zero limit", "negative limit", "large limit", "negative offset"} {
		t.Run(name, func(t *testing.T) {
			actor, team, zero := uuid.New(), uuid.New(), uuid.Nil
			filter := domain.TaskFilter{TeamID: &team, Limit: 20}
			switch name {
			case "zero actor":
				actor = zero
			case "missing team":
				filter.TeamID = nil
			case "zero team":
				filter.TeamID = &zero
			case "empty status", "unknown status":
				status := domain.TaskStatus("")
				if name == "unknown status" {
					status = "deleted"
				}
				filter.Status = &status
			case "zero assignee":
				filter.AssigneeID = &zero
			case "zero limit":
				filter.Limit = 0
			case "negative limit":
				filter.Limit = -1
			case "large limit":
				filter.Limit = 101
			case "negative offset":
				filter.Offset = -1
			}
			// Nil dependencies prove validation happens before any storage call.
			got, total, err := service.NewTaskService(nil, nil, nil, nil, testLogger()).List(context.Background(), actor, filter)
			if got != nil || total != 0 || !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("List()=(%v,%d,%v), want invalid input", got, total, err)
			}
		})
	}
}

func TestListTasksEmptyPages(t *testing.T) {
	for _, limit := range []int{1, 100} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			team := uuid.New()
			members := listTaskMembershipFunc(func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
				return &domain.TeamMember{Role: domain.MemberRole}, nil
			})
			repo := listTaskRepositoryFunc(func(context.Context, domain.TaskFilter) ([]domain.Task, int, error) {
				return []domain.Task{}, 12, nil
			})
			got, total, err := service.NewTaskService(nil, repo, nil, members, testLogger()).List(context.Background(), uuid.New(), domain.TaskFilter{TeamID: &team, Limit: limit, Offset: 1000})
			if err != nil || got == nil || len(got) != 0 || total != 12 {
				t.Errorf("List()=(%v,%d,%v), want empty page with total 12", got, total, err)
			}
		})
	}
}

func TestListTasksErrors(t *testing.T) {
	storageErr := errors.New("storage failure")
	for _, name := range []string{"missing membership", "membership error", "unknown role", "list error", "canceled"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "canceled" {
				cancel()
			}
			team := uuid.New()
			membershipCalls, listCalls := 0, 0
			members := listTaskMembershipFunc(func(got context.Context, _, _ uuid.UUID) (*domain.TeamMember, error) {
				membershipCalls++
				switch name {
				case "missing membership":
					return nil, fmt.Errorf("lookup: %w", domain.ErrTeamMemberNotFound)
				case "membership error":
					return nil, storageErr
				case "unknown role":
					return &domain.TeamMember{Role: "unknown"}, nil
				case "canceled":
					return nil, got.Err()
				}
				return &domain.TeamMember{Role: domain.MemberRole}, nil
			})
			repo := listTaskRepositoryFunc(func(context.Context, domain.TaskFilter) ([]domain.Task, int, error) {
				listCalls++
				return []domain.Task{{ID: uuid.New()}}, 999, storageErr
			})
			want, wantListCalls := error(domain.ErrForbidden), 0
			switch name {
			case "membership error":
				want = storageErr
			case "list error":
				want, wantListCalls = storageErr, 1
			case "canceled":
				want = context.Canceled
			}
			got, total, err := service.NewTaskService(nil, repo, nil, members, testLogger()).List(ctx, uuid.New(), domain.TaskFilter{TeamID: &team, Limit: 20})
			if got != nil || total != 0 || !errors.Is(err, want) || membershipCalls != 1 || listCalls != wantListCalls {
				t.Errorf("List()=(%v,%d,%v), membership/list calls=%d/%d, want nil/0/%v and 1/%d", got, total, err, membershipCalls, listCalls, want, wantListCalls)
			}
		})
	}
}
