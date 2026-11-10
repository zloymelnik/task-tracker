package service_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type listTaskHistoryFunc func(context.Context, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error)

func (f listTaskHistoryFunc) List(ctx context.Context, filter domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
	return f(ctx, filter)
}
func (f listTaskHistoryFunc) Save(context.Context, *domain.TaskHistory) error {
	panic("unexpected Save in history-list test")
}

func TestTaskHistory(t *testing.T) {
	for _, role := range []domain.TeamRole{domain.OwnerRole, domain.AdminRole, domain.MemberRole} {
		for _, page := range []string{"items", "empty", "past end"} {
			t.Run(string(role)+"/"+page, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				task, actor := updateTestTask(), uuid.New()
				filter := domain.TaskHistoryFilter{TaskID: &task.ID, Limit: 20}
				want := []domain.TaskHistory{{ID: uuid.New(), TaskID: task.ID, ChangedBy: task.CreatedBy, Changes: domain.TaskChanges{"title": {Old: "Old", New: "New"}}}}
				total := 42
				switch page {
				case "empty":
					want, total, filter.Limit = []domain.TaskHistory{}, 0, 1
				case "past end":
					want, filter.Offset, filter.Limit = []domain.TaskHistory{}, 1000, 100
				}
				var calls []string
				repo := updateTaskRepositoryStub{get: func(got context.Context, id uuid.UUID) (*domain.Task, error) {
					calls = append(calls, "task")
					if got != ctx || id != task.ID {
						t.Error("incorrect task lookup")
					}
					return &task, nil
				}}
				members := listTaskMembershipFunc(func(got context.Context, team, user uuid.UUID) (*domain.TeamMember, error) {
					calls = append(calls, "membership")
					if got != ctx || team != task.TeamID || user != actor {
						t.Error("membership must use task's team and authenticated user")
					}
					return &domain.TeamMember{Role: role}, nil
				})
				history := listTaskHistoryFunc(func(got context.Context, f domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
					calls = append(calls, "history")
					if got != ctx || !reflect.DeepEqual(f, filter) {
						t.Errorf("history filter/context lost: %+v", f)
					}
					cancel()
					if !errors.Is(got.Err(), context.Canceled) {
						t.Error("cancellation lost")
					}
					return want, total, nil
				})
				got, count, err := service.NewTaskService(nil, repo, history, nil, members, nil, service.TaskCacheOptions{}, testLogger()).History(ctx, actor, filter)
				if err != nil || got == nil || !reflect.DeepEqual(got, want) || count != total || !reflect.DeepEqual(calls, []string{"task", "membership", "history"}) {
					t.Errorf("History()=(%v,%d,%v), calls=%v", got, count, err, calls)
				}
			})
		}
	}
}

func TestTaskHistoryInvalidInput(t *testing.T) {
	for _, name := range []string{"actor", "missing task", "zero task", "zero limit", "negative limit", "large limit", "negative offset"} {
		t.Run(name, func(t *testing.T) {
			actor, task, zero := uuid.New(), uuid.New(), uuid.Nil
			filter := domain.TaskHistoryFilter{TaskID: &task, Limit: 20}
			switch name {
			case "actor":
				actor = uuid.Nil
			case "missing task":
				filter.TaskID = nil
			case "zero task":
				filter.TaskID = &zero
			case "zero limit":
				filter.Limit = 0
			case "negative limit":
				filter.Limit = -1
			case "large limit":
				filter.Limit = 101
			case "negative offset":
				filter.Offset = -1
			}
			got, total, err := service.NewTaskService(nil, nil, nil, nil, nil, nil, service.TaskCacheOptions{}, testLogger()).History(context.Background(), actor, filter)
			if got != nil || total != 0 || !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("History()=(%v,%d,%v), want invalid input", got, total, err)
			}
		})
	}
}

func TestTaskHistoryErrors(t *testing.T) {
	failure := errors.New("database failure")
	for _, name := range []string{"task missing", "task lookup", "membership missing", "membership lookup", "unknown role", "history lookup", "canceled"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "canceled" {
				cancel()
			}
			task := updateTestTask()
			var calls []string
			repo := updateTaskRepositoryStub{get: func(got context.Context, _ uuid.UUID) (*domain.Task, error) {
				calls = append(calls, "task")
				switch name {
				case "task missing":
					return nil, fmt.Errorf("get: %w", domain.ErrTaskNotFound)
				case "task lookup":
					return nil, failure
				case "canceled":
					return nil, got.Err()
				}
				return &task, nil
			}}
			members := listTaskMembershipFunc(func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
				calls = append(calls, "membership")
				switch name {
				case "membership missing":
					return nil, fmt.Errorf("get: %w", domain.ErrTeamMemberNotFound)
				case "membership lookup":
					return nil, failure
				case "unknown role":
					return &domain.TeamMember{Role: "unknown"}, nil
				}
				return &domain.TeamMember{Role: domain.MemberRole}, nil
			})
			history := listTaskHistoryFunc(func(context.Context, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
				calls = append(calls, "history")
				return []domain.TaskHistory{{ID: uuid.New()}}, 999, failure
			})
			want, wantCalls := failure, "task,membership"
			switch name {
			case "task missing":
				want, wantCalls = domain.ErrTaskNotFound, "task"
			case "task lookup":
				wantCalls = "task"
			case "membership missing", "unknown role":
				want = domain.ErrForbidden
			case "history lookup":
				wantCalls = "task,membership,history"
			case "canceled":
				want, wantCalls = context.Canceled, "task"
			}
			got, total, err := service.NewTaskService(nil, repo, history, nil, members, nil, service.TaskCacheOptions{}, testLogger()).History(ctx, uuid.New(), domain.TaskHistoryFilter{TaskID: &task.ID, Limit: 20})
			if got != nil || total != 0 || !errors.Is(err, want) || strings.Join(calls, ",") != wantCalls {
				t.Errorf("History()=(%v,%d,%v) calls=%v, want nil/0/%v calls=%s", got, total, err, calls, want, wantCalls)
			}
		})
	}
}
