package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type updateTaskRepositoryStub struct {
	get    func(context.Context, uuid.UUID) (*domain.Task, error)
	update func(context.Context, *domain.Task, uint32) error
}

func (s updateTaskRepositoryStub) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	return s.get(ctx, id)
}
func (s updateTaskRepositoryStub) Update(ctx context.Context, task *domain.Task, version uint32) error {
	return s.update(ctx, task, version)
}
func (s updateTaskRepositoryStub) Save(context.Context, *domain.Task) error { panic("unexpected Save") }
func (s updateTaskRepositoryStub) List(context.Context, domain.TaskFilter) ([]domain.Task, int, error) {
	panic("unexpected List")
}

func updateTestTask() domain.Task {
	now := time.Date(2026, 11, 1, 12, 0, 0, 123000000, time.UTC)
	return domain.Task{ID: uuid.New(), TeamID: uuid.New(), CreatedBy: uuid.New(), Title: "Old", Description: "Description", Status: domain.Todo, CreatedAt: now, UpdatedAt: now, Version: 4}
}

func TestUpdateTaskChanges(t *testing.T) {
	for _, name := range []string{"title", "clear description", "all fields", "assign", "replace assignee", "unassign", "close", "reopen", "edit closed", "no fields", "same normalized title", "same assignee value", "same status", "ignore unset assignee"} {
		t.Run(name, func(t *testing.T) {
			old := updateTestTask()
			assignee, replacement := uuid.New(), uuid.New()
			title, description, status := "  Новый заголовок  ", "", domain.Done
			input := service.UpdateTaskInput{Version: old.Version}
			want := old
			changes := domain.TaskChanges{}
			switch name {
			case "title", "all fields", "edit closed":
				input.Title, want.Title = &title, "Новый заголовок"
				changes["title"] = domain.FieldChange{Old: old.Title, New: want.Title}
			case "clear description":
				input.Description, want.Description = &description, ""
				changes["description"] = domain.FieldChange{Old: old.Description, New: ""}
			case "assign":
				input.AssigneeSet, input.AssigneeID = true, &assignee
			case "replace assignee":
				old.AssigneeID, want.AssigneeID = &assignee, &assignee
				input.AssigneeSet, input.AssigneeID = true, &replacement
			case "unassign":
				old.AssigneeID, want.AssigneeID = &assignee, &assignee
				input.AssigneeSet = true
			case "close":
				input.Status, want.Status = &status, domain.Done
				changes["status"] = domain.FieldChange{Old: domain.Todo, New: domain.Done}
			case "reopen":
				old.Status, want.Status = domain.Done, domain.InProgress
				closed := old.UpdatedAt
				old.ClosedAt = &closed
				status = domain.InProgress
				input.Status = &status
				changes["status"] = domain.FieldChange{Old: domain.Done, New: domain.InProgress}
			case "same normalized title":
				title = "  Old  "
				input.Title = &title
			case "same assignee value":
				old.AssigneeID, want.AssigneeID = &assignee, &assignee
				copyID := assignee
				input.AssigneeSet, input.AssigneeID = true, &copyID
			case "same status":
				status = domain.Todo
				input.Status = &status
			case "ignore unset assignee":
				input.AssigneeID = &assignee
			}
			if name == "edit closed" {
				closed := old.UpdatedAt
				old.Status, want.Status, old.ClosedAt, want.ClosedAt = domain.Done, domain.Done, &closed, &closed
			}
			if name == "all fields" {
				input.Description, input.Status, input.AssigneeSet, input.AssigneeID = &description, &status, true, &assignee
				want.Description, want.Status = "", domain.Done
				changes["description"] = domain.FieldChange{Old: old.Description, New: ""}
				changes["status"] = domain.FieldChange{Old: domain.Todo, New: domain.Done}
			}
			if input.AssigneeSet {
				want.AssigneeID = input.AssigneeID
				if name != "same assignee value" {
					changes["assignee_id"] = domain.FieldChange{Old: old.AssigneeID, New: want.AssigneeID}
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			txCtx, stop := context.WithCancel(ctx)
			defer stop()
			checkCtx := func(got context.Context) {
				if got != txCtx {
					t.Error("transaction context was lost")
				}
			}
			var calls []string
			repo := updateTaskRepositoryStub{
				get: func(got context.Context, id uuid.UUID) (*domain.Task, error) {
					checkCtx(got)
					calls = append(calls, "read")
					if id != old.ID {
						t.Error("wrong task ID")
					}
					copyTask := old
					return &copyTask, nil
				},
				update: func(got context.Context, task *domain.Task, version uint32) error {
					checkCtx(got)
					calls = append(calls, "update")
					if version != old.Version || task.Version != old.Version {
						t.Error("incorrect expected version or premature version increment")
					}
					task.Version = version + 1
					return nil
				},
			}
			members := taskMembershipFunc(func(got context.Context, team, user uuid.UUID) (*domain.TeamMember, error) {
				checkCtx(got)
				if team != old.TeamID {
					t.Error("wrong membership team")
				}
				if user == old.CreatedBy {
					calls = append(calls, "actor")
				} else {
					calls = append(calls, "assignee")
					if !input.AssigneeSet || input.AssigneeID == nil || user != *input.AssigneeID {
						t.Error("unexpected assignee lookup")
					}
				}
				return &domain.TeamMember{Role: domain.MemberRole}, nil
			})
			var history *domain.TaskHistory
			hr := taskHistoryRepositoryFunc(func(got context.Context, h *domain.TaskHistory) error {
				checkCtx(got)
				calls = append(calls, "history")
				history = h
				return nil
			})
			tx := transactionManagerFunc(func(got context.Context, fn func(context.Context) error) error {
				if got != ctx {
					t.Error("wrong outer context")
				}
				calls = append(calls, "begin")
				if err := fn(txCtx); err != nil {
					return err
				}
				calls = append(calls, "commit")
				return nil
			})
			before := time.Now().UTC().Truncate(time.Millisecond)
			got, err := service.NewTaskService(tx, repo, hr, nil, members, nil, service.TaskCacheOptions{}, testLogger()).Update(ctx, old.CreatedBy, old.ID, input)
			if err != nil || got == nil {
				t.Fatalf("Update()=(%v,%v)", got, err)
			}
			wantCalls := []string{"begin", "read", "actor"}
			if input.AssigneeSet && input.AssigneeID != nil {
				wantCalls = append(wantCalls, "assignee")
			}
			if len(changes) == 0 {
				if history != nil {
					t.Error("no-op wrote history")
				}
			} else {
				wantCalls = append(wantCalls, "update", "history")
				want.Version++
				want.UpdatedAt = got.UpdatedAt
				if got.UpdatedAt.Before(before) || got.UpdatedAt.After(time.Now()) || got.UpdatedAt.Location() != time.UTC || got.UpdatedAt.Nanosecond()%int(time.Millisecond) != 0 {
					t.Errorf("incorrect update time: %v", got.UpdatedAt)
				}
				if name == "close" || name == "all fields" {
					want.ClosedAt = &got.UpdatedAt
				}
				if history == nil {
					t.Fatal("missing history")
				}
				if history.ID == uuid.Nil || history.TaskID != old.ID || history.ChangedBy != old.CreatedBy || history.CreatedAt != got.UpdatedAt {
					t.Errorf("incorrect history metadata: %+v", history)
				}
				actualJSON, e1 := json.Marshal(history.Changes)
				expectedJSON, e2 := json.Marshal(changes)
				if e1 != nil || e2 != nil || string(actualJSON) != string(expectedJSON) {
					t.Errorf("changes=%s, want %s (errors %v/%v)", actualJSON, expectedJSON, e1, e2)
				}
			}
			wantCalls = append(wantCalls, "commit")
			if !reflect.DeepEqual(calls, wantCalls) {
				t.Errorf("calls=%v, want %v", calls, wantCalls)
			}
			if !reflect.DeepEqual(*got, want) {
				t.Errorf("task=%+v, want %+v", got, want)
			}
		})
	}
}

func TestUpdateTaskPermissions(t *testing.T) {
	for _, role := range []domain.TeamRole{domain.OwnerRole, domain.AdminRole, domain.MemberRole} {
		for _, identity := range []string{"author", "assignee", "other"} {
			for _, field := range []string{"title", "description", "status", "assignee"} {
				t.Run(string(role)+"/"+identity+"/"+field, func(t *testing.T) {
					old := updateTestTask()
					assigned := uuid.New()
					old.AssigneeID = &assigned
					actor := uuid.New()
					if identity == "author" {
						actor = old.CreatedBy
					}
					if identity == "assignee" {
						actor = assigned
					}
					title, description, status := "New", "", domain.InProgress
					input := service.UpdateTaskInput{Version: old.Version}
					switch field {
					case "title":
						input.Title = &title
					case "description":
						input.Description = &description
					case "status":
						input.Status = &status
					case "assignee":
						input.AssigneeSet = true
					}
					writes := 0
					repo := updateTaskRepositoryStub{get: func(context.Context, uuid.UUID) (*domain.Task, error) { return &old, nil }, update: func(_ context.Context, task *domain.Task, v uint32) error { writes++; task.Version = v + 1; return nil }}
					members := taskMembershipFunc(func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
						return &domain.TeamMember{Role: role}, nil
					})
					hr := taskHistoryRepositoryFunc(func(context.Context, *domain.TaskHistory) error { writes++; return nil })
					tx := transactionManagerFunc(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
					got, err := service.NewTaskService(tx, repo, hr, nil, members, nil, service.TaskCacheOptions{}, testLogger()).Update(context.Background(), actor, old.ID, input)
					allowed := role == domain.OwnerRole || role == domain.AdminRole ||
						identity == "author" || (identity == "assignee" && field == "status")
					if allowed {
						if err != nil || got == nil || writes != 2 {
							t.Errorf("allowed update: task=%v err=%v writes=%d", got, err, writes)
						}
					} else if got != nil || !errors.Is(err, domain.ErrForbidden) || writes != 0 {
						t.Errorf("forbidden update: task=%v err=%v writes=%d", got, err, writes)
					}
				})
			}
		}
	}
}

func TestUpdateTaskInvalidInput(t *testing.T) {
	for _, name := range []string{"actor", "task", "zero version", "max version"} {
		t.Run(name, func(t *testing.T) {
			actor, task := uuid.New(), uuid.New()
			input := service.UpdateTaskInput{Version: 1}
			switch name {
			case "actor":
				actor = uuid.Nil
			case "task":
				task = uuid.Nil
			case "zero version":
				input.Version = 0
			case "max version":
				input.Version = math.MaxUint32
			}
			got, err := service.NewTaskService(nil, nil, nil, nil, nil, nil, service.TaskCacheOptions{}, testLogger()).Update(context.Background(), actor, task, input)
			if got != nil || !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("Update()=(%v,%v), want invalid input", got, err)
			}
		})
	}
}

func TestUpdateTaskErrors(t *testing.T) {
	failure := errors.New("storage failure")
	for _, name := range []string{"begin", "read", "not found", "missing actor", "actor lookup", "unknown role", "stale version", "blank title", "long title", "long description", "bad status", "zero assignee", "missing assignee", "assignee lookup", "update", "concurrent conflict", "history", "commit", "canceled"} {
		t.Run(name, func(t *testing.T) {
			old := updateTestTask()
			title, description, status, assignee := "New", "", domain.TaskStatus("invalid"), uuid.New()
			input := service.UpdateTaskInput{Version: old.Version, Title: &title}
			want := failure
			switch name {
			case "not found":
				want = domain.ErrTaskNotFound
			case "missing actor", "unknown role":
				want = domain.ErrForbidden
			case "stale version":
				input.Version--
				want = domain.ErrTaskVersionConflict
			case "blank title":
				title = " \t "
				want = domain.ErrInvalidInput
			case "long title":
				title = strings.Repeat("я", 256)
				want = domain.ErrInvalidInput
			case "long description":
				description = strings.Repeat("я", 32768)
				input.Description = &description
				want = domain.ErrInvalidInput
			case "bad status":
				input.Status = &status
				want = domain.ErrInvalidInput
			case "zero assignee":
				assignee = uuid.Nil
				want = domain.ErrInvalidInput
			case "missing assignee":
				want = domain.ErrInvalidInput
			case "concurrent conflict":
				want = domain.ErrTaskVersionConflict
			case "canceled":
				want = context.Canceled
			}
			if strings.Contains(name, "assignee") {
				input.AssigneeSet, input.AssigneeID = true, &assignee
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "canceled" {
				cancel()
			}
			var calls []string
			repo := updateTaskRepositoryStub{
				get: func(context.Context, uuid.UUID) (*domain.Task, error) {
					calls = append(calls, "read")
					if name == "read" {
						return nil, failure
					}
					if name == "not found" {
						return nil, fmt.Errorf("get: %w", want)
					}
					return &old, nil
				},
				update: func(_ context.Context, task *domain.Task, v uint32) error {
					calls = append(calls, "update")
					if name == "update" || name == "concurrent conflict" {
						return want
					}
					task.Version = v + 1
					return nil
				},
			}
			members := taskMembershipFunc(func(_ context.Context, _ uuid.UUID, user uuid.UUID) (*domain.TeamMember, error) {
				if user == old.CreatedBy {
					calls = append(calls, "actor")
					switch name {
					case "missing actor":
						return nil, fmt.Errorf("get: %w", domain.ErrTeamMemberNotFound)
					case "actor lookup":
						return nil, failure
					case "unknown role":
						return &domain.TeamMember{Role: "unknown"}, nil
					}
				} else {
					calls = append(calls, "assignee")
					if name == "missing assignee" {
						return nil, domain.ErrTeamMemberNotFound
					}
					if name == "assignee lookup" {
						return nil, failure
					}
				}
				return &domain.TeamMember{Role: domain.MemberRole}, nil
			})
			hr := taskHistoryRepositoryFunc(func(context.Context, *domain.TaskHistory) error {
				calls = append(calls, "history")
				if name == "history" {
					return failure
				}
				return nil
			})
			tx := transactionManagerFunc(func(ctx context.Context, fn func(context.Context) error) error {
				if name == "begin" {
					return failure
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := fn(ctx); err != nil {
					return err
				}
				if name == "commit" {
					return failure
				}
				return nil
			})
			got, err := service.NewTaskService(tx, repo, hr, nil, members, nil, service.TaskCacheOptions{}, testLogger()).Update(ctx, old.CreatedBy, old.ID, input)
			if got != nil || !errors.Is(err, want) {
				t.Errorf("Update()=(%v,%v), want nil/%v", got, err, want)
			}
			wantCalls := []string{}
			switch name {
			case "begin", "canceled":
			case "read", "not found":
				wantCalls = []string{"read"}
			case "update", "concurrent conflict":
				wantCalls = []string{"read", "actor", "update"}
			case "history", "commit":
				wantCalls = []string{"read", "actor", "update", "history"}
			case "missing assignee", "assignee lookup":
				wantCalls = []string{"read", "actor", "assignee"}
			default:
				wantCalls = []string{"read", "actor"}
			}
			if strings.Join(calls, ",") != strings.Join(wantCalls, ",") {
				t.Errorf("calls=%v, want %v", calls, wantCalls)
			}
		})
	}
}
