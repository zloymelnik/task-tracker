package mysql_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

func TestTaskCommentRepositoryList(t *testing.T) {
	_, repos, team, actor, other := newTaskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	task := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
	otherTask := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
	now := task.CreatedAt
	comments := []domain.TaskComment{
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000001"), TaskID: task.ID, CreatedBy: actor.ID, Body: " \nКомментарий\n ", CreatedAt: now.Add(-time.Hour)},
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000002"), TaskID: task.ID, CreatedBy: other.ID, Body: "Second", CreatedAt: now},
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000003"), TaskID: task.ID, CreatedBy: actor.ID, Body: "Third", CreatedAt: now},
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000004"), TaskID: otherTask.ID, CreatedBy: other.ID, Body: "Other task", CreatedAt: now.Add(time.Hour)},
	}
	for i := range comments {
		if err := repos.TaskComment.Save(ctx, &comments[i]); err != nil {
			t.Fatal(err)
		}
	}
	unknown := uuid.New()
	for _, tt := range []struct {
		name    string
		filter  domain.TaskCommentFilter
		indices []int
		total   int
	}{
		{"task", domain.TaskCommentFilter{TaskID: &task.ID, Limit: 20}, []int{2, 1, 0}, 3},
		{"first page", domain.TaskCommentFilter{TaskID: &task.ID, Limit: 1}, []int{2}, 3},
		{"next page", domain.TaskCommentFilter{TaskID: &task.ID, Limit: 1, Offset: 1}, []int{1}, 3},
		{"last page", domain.TaskCommentFilter{TaskID: &task.ID, Limit: 2, Offset: 2}, []int{0}, 3},
		{"past end", domain.TaskCommentFilter{TaskID: &task.ID, Limit: 2, Offset: 100}, nil, 3},
		{"other task", domain.TaskCommentFilter{TaskID: &otherTask.ID, Limit: 20}, []int{3}, 1},
		{"no matches", domain.TaskCommentFilter{TaskID: &unknown, Limit: 20}, nil, 0},
		{"no filter", domain.TaskCommentFilter{Limit: 20}, []int{3, 2, 1, 0}, 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := repos.TaskComment.List(ctx, tt.filter)
			if err != nil || got == nil || len(got) != len(tt.indices) || total != tt.total {
				t.Fatalf("List()=(%v,%d,%v), want %d rows total %d", got, total, err, len(tt.indices), tt.total)
			}
			for i, index := range tt.indices {
				if !reflect.DeepEqual(got[i], comments[index]) {
					t.Errorf("comment[%d]=%+v, want %+v", i, got[i], comments[index])
				}
			}
		})
	}
}

func TestTaskCommentRepositoryErrors(t *testing.T) {
	db, repos, team, actor, _ := newTaskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	task := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
	comment := domain.TaskComment{ID: uuid.New(), TaskID: task.ID, CreatedBy: actor.ID, Body: "Comment", CreatedAt: task.CreatedAt}
	if err := repos.TaskComment.Save(ctx, &comment); err != nil {
		t.Fatal(err)
	}
	var dbErr *mysqldriver.MySQLError
	if err := repos.TaskComment.Save(ctx, &comment); !errors.As(err, &dbErr) || dbErr.Number != 1062 {
		t.Errorf("duplicate error=%v", err)
	}
	for _, field := range []string{"task", "author"} {
		invalid := comment
		invalid.ID = uuid.New()
		if field == "task" {
			invalid.TaskID = uuid.New()
		} else {
			invalid.CreatedBy = uuid.New()
		}
		if err := repos.TaskComment.Save(ctx, &invalid); !errors.As(err, &dbErr) || dbErr.Number != 1452 {
			t.Errorf("missing %s FK error=%v", field, err)
		}
	}
	zero := uuid.Nil
	rows, total, err := repos.TaskComment.List(ctx, domain.TaskCommentFilter{TaskID: &zero, Limit: 20})
	if rows != nil || total != 0 || !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("zero task List()=(%v,%d,%v)", rows, total, err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := repos.TaskComment.Save(canceled, &comment); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled Save()=%v", err)
	}
	rows, total, err = repos.TaskComment.List(canceled, domain.TaskCommentFilter{Limit: 20})
	if rows != nil || total != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("canceled List()=(%v,%d,%v)", rows, total, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskComment.Save(ctx, &comment); err == nil {
		t.Error("closed DB Save succeeded")
	}
	rows, total, err = repos.TaskComment.List(ctx, domain.TaskCommentFilter{Limit: 20})
	if rows != nil || total != 0 || err == nil {
		t.Errorf("closed DB List()=(%v,%d,%v)", rows, total, err)
	}
}

type failAfterCommentSave struct {
	repo *mysql.TaskCommentRepository
	err  error
}

func (r failAfterCommentSave) Save(ctx context.Context, c *domain.TaskComment) error {
	if err := r.repo.Save(ctx, c); err != nil {
		return err
	}
	return r.err
}
func (r failAfterCommentSave) List(ctx context.Context, f domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	return r.repo.List(ctx, f)
}

func TestAddCommentTransaction(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "commit"
		if rollback {
			name = "rollback after insert"
		}
		t.Run(name, func(t *testing.T) {
			db, repos, team, actor, _ := newTaskFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			task := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			failure := errors.New("save failed after insert")
			var comments service.TaskCommentRepository = repos.TaskComment
			if rollback {
				comments = failAfterCommentSave{repo: repos.TaskComment, err: failure}
			}
			svc := service.NewTaskService(repos.Transactions, repos.Task, repos.TaskHistory, comments, repos.TeamMember, log)
			body := " \nСохранить форматирование\n "
			got, err := svc.AddComment(ctx, actor.ID, task.ID, service.CreateTaskCommentInput{Body: body})
			rows, total, listErr := repos.TaskComment.List(ctx, domain.TaskCommentFilter{TaskID: &task.ID, Limit: 20})
			if listErr != nil {
				t.Fatal(listErr)
			}
			if rollback {
				if got != nil || !errors.Is(err, failure) || total != 0 || len(rows) != 0 {
					t.Errorf("rollback got=%v err=%v rows=%v total=%d", got, err, rows, total)
				}
			} else {
				if err != nil || got == nil || total != 1 || len(rows) != 1 || !reflect.DeepEqual(rows[0], *got) || got.Body != body || got.CreatedBy != actor.ID {
					t.Fatalf("commit got=%v err=%v rows=%v total=%d", got, err, rows, total)
				}
				page, count, err := svc.Comments(ctx, actor.ID, domain.TaskCommentFilter{TaskID: &task.ID, Limit: 20})
				if err != nil || count != 1 || !reflect.DeepEqual(page, rows) {
					t.Errorf("Comments()=(%v,%d,%v)", page, count, err)
				}
				outsider := testUser()
				if err := repos.User.Save(ctx, outsider); err != nil {
					t.Fatal(err)
				}
				if denied, count, err := svc.Comments(ctx, outsider.ID, domain.TaskCommentFilter{TaskID: &task.ID, Limit: 20}); denied != nil || count != 0 || !errors.Is(err, domain.ErrForbidden) {
					t.Errorf("outsider Comments()=(%v,%d,%v)", denied, count, err)
				}
				if denied, err := svc.AddComment(ctx, outsider.ID, task.ID, service.CreateTaskCommentInput{Body: "Forbidden"}); denied != nil || !errors.Is(err, domain.ErrForbidden) {
					t.Errorf("outsider AddComment()=(%v,%v)", denied, err)
				}
			}
			stored, err := repos.Task.GetByID(ctx, task.ID)
			if err != nil || stored == nil || !reflect.DeepEqual(*stored, task) {
				t.Errorf("comment changed task: %+v err=%v", stored, err)
			}
			var historyCount int
			if err := db.GetContext(ctx, &historyCount, "SELECT COUNT(*) FROM task_history WHERE task_id = ?", task.ID[:]); err != nil || historyCount != 0 {
				t.Errorf("comment changed history: count=%d err=%v", historyCount, err)
			}
		})
	}
}

func TestTaskCommentListInTransaction(t *testing.T) {
	_, repos, team, actor, _ := newTaskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	task := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
	comment := domain.TaskComment{ID: uuid.New(), TaskID: task.ID, CreatedBy: actor.ID, Body: "Uncommitted", CreatedAt: task.CreatedAt}
	rollback := errors.New("force rollback")
	err := repos.Transactions.Do(ctx, func(txCtx context.Context) error {
		if err := repos.TaskComment.Save(txCtx, &comment); err != nil {
			return err
		}
		rows, total, err := repos.TaskComment.List(txCtx, domain.TaskCommentFilter{TaskID: &task.ID, Limit: 20})
		if err != nil {
			return err
		}
		if total != 1 || len(rows) != 1 || !reflect.DeepEqual(rows[0], comment) {
			t.Errorf("uncommitted comment invisible: rows=%v total=%d", rows, total)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	rows, total, err := repos.TaskComment.List(ctx, domain.TaskCommentFilter{TaskID: &task.ID, Limit: 20})
	if err != nil || rows == nil || len(rows) != 0 || total != 0 {
		t.Errorf("rolled-back comment visible: rows=%v total=%d err=%v", rows, total, err)
	}
}
