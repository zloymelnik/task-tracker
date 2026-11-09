package service_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type taskCommentRepoStub struct {
	save func(context.Context, *domain.TaskComment) error
	list func(context.Context, domain.TaskCommentFilter) ([]domain.TaskComment, int, error)
}

func (s taskCommentRepoStub) Save(ctx context.Context, c *domain.TaskComment) error {
	return s.save(ctx, c)
}
func (s taskCommentRepoStub) List(ctx context.Context, f domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	return s.list(ctx, f)
}

func TestAddTaskComment(t *testing.T) {
	for _, role := range []domain.TeamRole{domain.OwnerRole, domain.AdminRole, domain.MemberRole} {
		for _, body := range []string{" \nКомментарий\n ", strings.Repeat("a", 65535)} {
			t.Run(fmt.Sprintf("%s/bytes=%d", role, len(body)), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				txCtx, stop := context.WithCancel(ctx)
				defer stop()
				task, actor := updateTestTask(), uuid.New()
				var calls []string
				checkCtx := func(got context.Context) {
					if got != txCtx {
						t.Error("transaction context lost")
					}
				}
				repo := updateTaskRepositoryStub{get: func(got context.Context, id uuid.UUID) (*domain.Task, error) {
					checkCtx(got)
					calls = append(calls, "task")
					if id != task.ID {
						t.Error("wrong task ID")
					}
					return &task, nil
				}}
				members := taskMembershipFunc(func(got context.Context, team, user uuid.UUID) (*domain.TeamMember, error) {
					checkCtx(got)
					calls = append(calls, "membership")
					if team != task.TeamID || user != actor {
						t.Error("wrong membership")
					}
					return &domain.TeamMember{Role: role}, nil
				})
				var saved domain.TaskComment
				comments := taskCommentRepoStub{save: func(got context.Context, c *domain.TaskComment) error {
					checkCtx(got)
					calls = append(calls, "save")
					saved = *c
					return nil
				}}
				tx := transactionManagerFunc(func(got context.Context, fn func(context.Context) error) error {
					if got != ctx {
						t.Error("outer context lost")
					}
					calls = append(calls, "begin")
					if err := fn(txCtx); err != nil {
						return err
					}
					calls = append(calls, "commit")
					return nil
				})
				before := time.Now().UTC().Truncate(time.Millisecond)
				got, err := service.NewTaskService(tx, repo, nil, comments, members, nil, service.TaskCacheOptions{}, testLogger()).AddComment(ctx, actor, task.ID, service.CreateTaskCommentInput{Body: body})
				if err != nil || got == nil {
					t.Fatalf("AddComment()=(%v,%v)", got, err)
				}
				if *got != saved || got.ID == uuid.Nil || got.ID.Version() != 4 || got.TaskID != task.ID || got.CreatedBy != actor || got.Body != body {
					t.Errorf("incorrect comment: %+v", got)
				}
				if got.CreatedAt.Before(before) || got.CreatedAt.After(time.Now()) || got.CreatedAt.Location() != time.UTC || got.CreatedAt.Nanosecond()%int(time.Millisecond) != 0 {
					t.Errorf("incorrect timestamp: %v", got.CreatedAt)
				}
				if !reflect.DeepEqual(calls, []string{"begin", "task", "membership", "save", "commit"}) {
					t.Errorf("calls=%v", calls)
				}
			})
		}
	}
}

func TestAddTaskCommentInvalidInput(t *testing.T) {
	for _, name := range []string{"actor", "task", "empty", "whitespace", "Unicode whitespace", "too long", "too long Unicode"} {
		t.Run(name, func(t *testing.T) {
			actor, task, body := uuid.New(), uuid.New(), "Comment"
			switch name {
			case "actor":
				actor = uuid.Nil
			case "task":
				task = uuid.Nil
			case "empty":
				body = ""
			case "whitespace":
				body = " \n\t\r "
			case "Unicode whitespace":
				body = "\u2003\u00a0"
			case "too long":
				body = strings.Repeat("a", 65536)
			case "too long Unicode":
				body = strings.Repeat("я", 32768)
			}
			got, err := service.NewTaskService(nil, nil, nil, nil, nil, nil, service.TaskCacheOptions{}, testLogger()).AddComment(context.Background(), actor, task, service.CreateTaskCommentInput{Body: body})
			if got != nil || !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("AddComment()=(%v,%v), want invalid input", got, err)
			}
		})
	}
}

func TestAddTaskCommentErrors(t *testing.T) {
	failure := errors.New("comment storage failure")
	for _, name := range []string{"begin", "task lookup", "missing task", "membership lookup", "missing membership", "unknown role", "save", "commit", "canceled"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "canceled" {
				cancel()
			}
			task := updateTestTask()
			var calls []string
			repo := updateTaskRepositoryStub{get: func(context.Context, uuid.UUID) (*domain.Task, error) {
				calls = append(calls, "task")
				if name == "task lookup" {
					return nil, failure
				}
				if name == "missing task" {
					return nil, fmt.Errorf("get: %w", domain.ErrTaskNotFound)
				}
				return &task, nil
			}}
			members := taskMembershipFunc(func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
				calls = append(calls, "membership")
				switch name {
				case "membership lookup":
					return nil, failure
				case "missing membership":
					return nil, fmt.Errorf("get: %w", domain.ErrTeamMemberNotFound)
				case "unknown role":
					return &domain.TeamMember{Role: "unknown"}, nil
				}
				return &domain.TeamMember{Role: domain.MemberRole}, nil
			})
			comments := taskCommentRepoStub{save: func(context.Context, *domain.TaskComment) error {
				calls = append(calls, "save")
				if name == "save" {
					return failure
				}
				return nil
			}}
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
			want, wantCalls := failure, "task,membership"
			switch name {
			case "begin":
				wantCalls = ""
			case "task lookup":
				wantCalls = "task"
			case "missing task":
				want, wantCalls = domain.ErrTaskNotFound, "task"
			case "missing membership", "unknown role":
				want = domain.ErrForbidden
			case "save", "commit":
				wantCalls = "task,membership,save"
			case "canceled":
				want, wantCalls = context.Canceled, ""
			}
			got, err := service.NewTaskService(tx, repo, nil, comments, members, nil, service.TaskCacheOptions{}, testLogger()).AddComment(ctx, uuid.New(), task.ID, service.CreateTaskCommentInput{Body: "Comment"})
			if got != nil || !errors.Is(err, want) || strings.Join(calls, ",") != wantCalls {
				t.Errorf("AddComment()=(%v,%v) calls=%v, want nil/%v calls=%s", got, err, calls, want, wantCalls)
			}
		})
	}
}

func TestTaskComments(t *testing.T) {
	for _, role := range []domain.TeamRole{domain.OwnerRole, domain.AdminRole, domain.MemberRole} {
		for _, page := range []string{"items", "empty", "past end"} {
			t.Run(string(role)+"/"+page, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				task, actor := updateTestTask(), uuid.New()
				filter := domain.TaskCommentFilter{TaskID: &task.ID, Limit: 20}
				want := []domain.TaskComment{{ID: uuid.New(), TaskID: task.ID, CreatedBy: task.CreatedBy, Body: "Comment"}}
				total := 42
				switch page {
				case "empty":
					want, total, filter.Limit = []domain.TaskComment{}, 0, 1
				case "past end":
					want, filter.Offset, filter.Limit = []domain.TaskComment{}, 1000, 100
				}
				var calls []string
				repo := updateTaskRepositoryStub{get: func(got context.Context, id uuid.UUID) (*domain.Task, error) {
					calls = append(calls, "task")
					if got != ctx || id != task.ID {
						t.Error("wrong task lookup")
					}
					return &task, nil
				}}
				members := listTaskMembershipFunc(func(got context.Context, team, user uuid.UUID) (*domain.TeamMember, error) {
					calls = append(calls, "membership")
					if got != ctx || team != task.TeamID || user != actor {
						t.Error("wrong membership")
					}
					return &domain.TeamMember{Role: role}, nil
				})
				comments := taskCommentRepoStub{list: func(got context.Context, f domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
					calls = append(calls, "comments")
					if got != ctx || !reflect.DeepEqual(f, filter) {
						t.Error("wrong list context/filter")
					}
					cancel()
					if !errors.Is(got.Err(), context.Canceled) {
						t.Error("cancellation lost")
					}
					return want, total, nil
				}}
				got, count, err := service.NewTaskService(nil, repo, nil, comments, members, nil, service.TaskCacheOptions{}, testLogger()).Comments(ctx, actor, filter)
				if err != nil || got == nil || !reflect.DeepEqual(got, want) || count != total || !reflect.DeepEqual(calls, []string{"task", "membership", "comments"}) {
					t.Errorf("Comments()=(%v,%d,%v), calls=%v", got, count, err, calls)
				}
			})
		}
	}
}

func TestTaskCommentsInvalidInput(t *testing.T) {
	for _, name := range []string{"actor", "missing task", "zero task", "zero limit", "negative limit", "large limit", "negative offset"} {
		t.Run(name, func(t *testing.T) {
			actor, task, zero := uuid.New(), uuid.New(), uuid.Nil
			filter := domain.TaskCommentFilter{TaskID: &task, Limit: 20}
			switch name {
			case "actor":
				actor = zero
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
			got, total, err := service.NewTaskService(nil, nil, nil, nil, nil, nil, service.TaskCacheOptions{}, testLogger()).Comments(context.Background(), actor, filter)
			if got != nil || total != 0 || !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("Comments()=(%v,%d,%v)", got, total, err)
			}
		})
	}
}

func TestTaskCommentsErrors(t *testing.T) {
	failure := errors.New("comments storage failure")
	for _, name := range []string{"task lookup", "missing task", "membership lookup", "missing membership", "unknown role", "list", "canceled"} {
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
				case "task lookup":
					return nil, failure
				case "missing task":
					return nil, fmt.Errorf("get: %w", domain.ErrTaskNotFound)
				case "canceled":
					return nil, got.Err()
				}
				return &task, nil
			}}
			members := listTaskMembershipFunc(func(context.Context, uuid.UUID, uuid.UUID) (*domain.TeamMember, error) {
				calls = append(calls, "membership")
				switch name {
				case "membership lookup":
					return nil, failure
				case "missing membership":
					return nil, fmt.Errorf("get: %w", domain.ErrTeamMemberNotFound)
				case "unknown role":
					return &domain.TeamMember{Role: "unknown"}, nil
				}
				return &domain.TeamMember{Role: domain.MemberRole}, nil
			})
			comments := taskCommentRepoStub{list: func(context.Context, domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
				calls = append(calls, "comments")
				return []domain.TaskComment{{ID: uuid.New()}}, 999, failure
			}}
			want, wantCalls := failure, "task,membership"
			switch name {
			case "task lookup":
				wantCalls = "task"
			case "missing task":
				want, wantCalls = domain.ErrTaskNotFound, "task"
			case "missing membership", "unknown role":
				want = domain.ErrForbidden
			case "list":
				wantCalls = "task,membership,comments"
			case "canceled":
				want, wantCalls = context.Canceled, "task"
			}
			got, total, err := service.NewTaskService(nil, repo, nil, comments, members, nil, service.TaskCacheOptions{}, testLogger()).Comments(ctx, uuid.New(), domain.TaskCommentFilter{TaskID: &task.ID, Limit: 20})
			if got != nil || total != 0 || !errors.Is(err, want) || strings.Join(calls, ",") != wantCalls {
				t.Errorf("Comments()=(%v,%d,%v) calls=%v, want nil/0/%v calls=%s", got, total, err, calls, want, wantCalls)
			}
		})
	}
}
