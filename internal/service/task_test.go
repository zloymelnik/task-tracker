package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type taskRepositoryFunc func(context.Context, *domain.Task) error

func (f taskRepositoryFunc) GetByID(context.Context, uuid.UUID) (*domain.Task, error) {
	panic("unexpected GetByID in create-task test")
}

func (f taskRepositoryFunc) Update(context.Context, *domain.Task, uint32) error {
	panic("unexpected Update in create-task test")
}

func (f taskRepositoryFunc) Save(ctx context.Context, task *domain.Task) error { return f(ctx, task) }

func (f taskRepositoryFunc) List(context.Context, domain.TaskFilter) ([]domain.Task, int, error) {
	panic("unexpected List in create-task test")
}

type taskHistoryRepositoryFunc func(context.Context, *domain.TaskHistory) error

func (f taskHistoryRepositoryFunc) List(context.Context, domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
	panic("unexpected List in history-save test")
}

func (f taskHistoryRepositoryFunc) Save(ctx context.Context, history *domain.TaskHistory) error {
	return f(ctx, history)
}

type taskMembershipFunc func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error)

func (f taskMembershipFunc) GetByTeamAndUserID(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
	panic("unexpected unlocked membership read in create-task test")
}

func (f taskMembershipFunc) GetByTeamAndUserIDForUpdate(ctx context.Context, team, user uuid.UUID) (*domain.TeamMember, error) {
	return f(ctx, team, user)
}

func TestCreateTask(t *testing.T) {
	for _, role := range []domain.TeamRole{domain.OwnerRole, domain.AdminRole, domain.MemberRole} {
		for _, assignment := range []string{"unassigned", "self", "other member"} {
			t.Run(string(role)+"/"+assignment, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				txCtx, stop := context.WithCancel(ctx)
				defer stop()
				actor, team, other := uuid.New(), uuid.New(), uuid.New()
				input := service.CreateTaskInput{TeamID: team, Title: " \tНовая задача\n", Description: " \nОписание с пробелами\n "}
				switch assignment {
				case "self":
					input.AssigneeID = &actor
				case "other member":
					input.AssigneeID = &other
				}
				var calls []string
				var savedTask domain.Task
				var savedHistory domain.TaskHistory
				checkContext := func(got context.Context) {
					if got != txCtx {
						t.Error("repository did not receive transaction context")
					}
				}
				members := taskMembershipFunc(func(got context.Context, gotTeam, user uuid.UUID) (*domain.TeamMember, error) {
					checkContext(got)
					if gotTeam != team {
						t.Error("membership lookup used wrong team")
					}
					memberRole := role
					if user == actor {
						calls = append(calls, "actor")
					} else {
						calls = append(calls, "assignee")
						if assignment != "other member" || user != other {
							t.Error("unexpected assignee lookup")
						}
						memberRole = domain.MemberRole
					}
					return &domain.TeamMember{TeamID: team, UserID: user, Role: memberRole}, nil
				})
				tasks := taskRepositoryFunc(func(got context.Context, task *domain.Task) error {
					checkContext(got)
					calls = append(calls, "task")
					savedTask = *task
					return nil
				})
				history := taskHistoryRepositoryFunc(func(got context.Context, h *domain.TaskHistory) error {
					checkContext(got)
					calls = append(calls, "history")
					savedHistory = *h
					return nil
				})
				tx := transactionManagerFunc(func(got context.Context, fn func(context.Context) error) error {
					if got != ctx {
						t.Error("manager received wrong context")
					}
					calls = append(calls, "begin")
					if err := fn(txCtx); err != nil {
						return err
					}
					calls = append(calls, "commit")
					return nil
				})
				before := time.Now().UTC().Truncate(time.Millisecond)
				got, err := service.NewTaskService(tx, tasks, history, nil, members, testLogger()).Create(ctx, actor, input)
				after := time.Now().UTC()
				if err != nil || got == nil {
					t.Fatalf("Create()=(%v,%v), want task", got, err)
				}
				if !reflect.DeepEqual(*got, savedTask) || got.ID == uuid.Nil || got.ID.Version() != 4 || got.TeamID != team || got.CreatedBy != actor {
					t.Errorf("unexpected saved/returned task: %+v", got)
				}
				if got.Title != "Новая задача" || got.Description != input.Description || !reflect.DeepEqual(got.AssigneeID, input.AssigneeID) || got.Status != domain.Todo || got.Version != 1 || got.ClosedAt != nil {
					t.Errorf("unexpected initial task values: %+v", got)
				}
				if got.CreatedAt.Before(before) || got.CreatedAt.After(after) || got.CreatedAt.Location() != time.UTC || got.CreatedAt.Nanosecond()%int(time.Millisecond) != 0 || got.UpdatedAt != got.CreatedAt {
					t.Errorf("unexpected timestamps: %v/%v", got.CreatedAt, got.UpdatedAt)
				}
				if savedHistory.ID == uuid.Nil || savedHistory.ID == got.ID || savedHistory.ID.Version() != 4 || savedHistory.TaskID != got.ID || savedHistory.ChangedBy != actor || savedHistory.CreatedAt != got.CreatedAt {
					t.Errorf("unexpected initial history: %+v", savedHistory)
				}
				assertInitialTaskChanges(t, savedHistory.Changes, got)
				wantCalls := []string{"begin", "actor"}
				if assignment == "other member" {
					wantCalls = append(wantCalls, "assignee")
				}
				wantCalls = append(wantCalls, "task", "history", "commit")
				if !reflect.DeepEqual(calls, wantCalls) {
					t.Errorf("calls=%v, want %v", calls, wantCalls)
				}
			})
		}
	}
}

func assertInitialTaskChanges(t *testing.T, changes domain.TaskChanges, task *domain.Task) {
	t.Helper()
	data, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]struct {
		Old any `json:"old"`
		New any `json:"new"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	var assignee any
	if task.AssigneeID != nil {
		assignee = task.AssigneeID.String()
	}
	want := map[string]any{"title": task.Title, "description": task.Description, "status": "todo", "assignee_id": assignee}
	if len(got) != len(want) {
		t.Errorf("history fields=%d, want %d", len(got), len(want))
	}
	for field, value := range want {
		change, ok := got[field]
		if !ok || change.Old != nil || !reflect.DeepEqual(change.New, value) {
			t.Errorf("change[%s]=%+v, want null -> %v", field, change, value)
		}
	}
}

func TestCreateTaskInvalidInput(t *testing.T) {
	for _, name := range []string{"nil actor", "nil team", "empty title", "blank title", "long Unicode title", "long description", "long Unicode description", "zero assignee"} {
		t.Run(name, func(t *testing.T) {
			actor := uuid.New()
			input := service.CreateTaskInput{TeamID: uuid.New(), Title: "Task"}
			switch name {
			case "nil actor":
				actor = uuid.Nil
			case "nil team":
				input.TeamID = uuid.Nil
			case "empty title":
				input.Title = ""
			case "blank title":
				input.Title = " \t\n"
			case "long Unicode title":
				input.Title = strings.Repeat("я", 256)
			case "long description":
				input.Description = strings.Repeat("a", 65536)
			case "long Unicode description":
				input.Description = strings.Repeat("я", 32768)
			case "zero assignee":
				zero := uuid.Nil
				input.AssigneeID = &zero
			}
			tx := transactionManagerFunc(func(context.Context, func(context.Context) error) error {
				t.Fatal("invalid input must not start transaction")
				return nil
			})
			got, err := service.NewTaskService(tx, nil, nil, nil, nil, testLogger()).Create(context.Background(), actor, input)
			if got != nil || !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("Create()=(%v,%v), want invalid input", got, err)
			}
		})
	}
}

func TestCreateTaskTextBoundaries(t *testing.T) {
	for _, description := range []string{"", strings.Repeat("a", 65535)} {
		t.Run(fmtDescriptionCase(description), func(t *testing.T) {
			tx := transactionManagerFunc(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
			members := taskMembershipFunc(func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
				return &domain.TeamMember{Role: domain.MemberRole}, nil
			})
			tasks := taskRepositoryFunc(func(context.Context, *domain.Task) error { return nil })
			history := taskHistoryRepositoryFunc(func(context.Context, *domain.TaskHistory) error { return nil })
			got, err := service.NewTaskService(tx, tasks, history, nil, members, testLogger()).Create(context.Background(), uuid.New(), service.CreateTaskInput{TeamID: uuid.New(), Title: strings.Repeat("я", 255), Description: description})
			if err != nil || got == nil || got.Description != description {
				t.Errorf("boundary input rejected: %v", err)
			}
		})
	}
}

func fmtDescriptionCase(description string) string {
	if description == "" {
		return "empty description"
	}
	return "65535 byte description"
}

func TestCreateTaskErrors(t *testing.T) {
	storageErr := errors.New("task storage failure")
	for _, tt := range []struct {
		name  string
		want  error
		steps int
	}{
		{"begin", storageErr, 0}, {"missing actor", domain.ErrForbidden, 1}, {"unknown actor role", domain.ErrForbidden, 1},
		{"actor lookup", storageErr, 1}, {"missing assignee", domain.ErrInvalidInput, 2}, {"assignee lookup", storageErr, 2},
		{"task save", storageErr, 3}, {"history save", storageErr, 4}, {"commit", storageErr, 4}, {"canceled", context.Canceled, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.name == "canceled" {
				cancel()
			}
			actor, team, assignee := uuid.New(), uuid.New(), uuid.New()
			steps := 0
			members := taskMembershipFunc(func(_ context.Context, _ uuid.UUID, user uuid.UUID) (*domain.TeamMember, error) {
				steps++
				if user == actor {
					switch tt.name {
					case "missing actor":
						return nil, domain.ErrTeamMemberNotFound
					case "actor lookup":
						return nil, storageErr
					case "unknown actor role":
						return &domain.TeamMember{Role: "unknown"}, nil
					}
				} else {
					if tt.name == "missing assignee" {
						return nil, domain.ErrTeamMemberNotFound
					}
					if tt.name == "assignee lookup" {
						return nil, storageErr
					}
				}
				return &domain.TeamMember{Role: domain.MemberRole}, nil
			})
			tasks := taskRepositoryFunc(func(context.Context, *domain.Task) error {
				steps++
				if tt.name == "task save" {
					return storageErr
				}
				return nil
			})
			history := taskHistoryRepositoryFunc(func(context.Context, *domain.TaskHistory) error {
				steps++
				if tt.name == "history save" {
					return storageErr
				}
				return nil
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
			got, err := service.NewTaskService(tx, tasks, history, nil, members, testLogger()).Create(ctx, actor, service.CreateTaskInput{TeamID: team, Title: "Task", AssigneeID: &assignee})
			if got != nil || !errors.Is(err, tt.want) || steps != tt.steps {
				t.Errorf("Create()=(%v,%v) steps=%d, want nil/%v/%d", got, err, steps, tt.want, tt.steps)
			}
		})
	}
}
