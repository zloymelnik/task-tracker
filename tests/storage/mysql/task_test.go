package mysql_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

func newTaskFixture(t *testing.T) (*sqlx.DB, *mysql.Repository, *domain.Team, *domain.User, *domain.User) {
	t.Helper()
	db := newIsolatedRepositoryDB(t, true)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repos := mysql.NewRepository(config.DB{MaxRequestTime: 5 * time.Second}, db, log)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	actor, assignee := testUser(), testUser()
	for _, user := range []*domain.User{actor, assignee} {
		if err := repos.User.Save(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	team := &domain.Team{ID: uuid.New(), Name: "Task tests", CreatedBy: actor.ID, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := repos.Team.Save(ctx, team); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*domain.TeamMember{
		{TeamID: team.ID, UserID: actor.ID, Role: domain.OwnerRole, JoinedAt: team.CreatedAt},
		{TeamID: team.ID, UserID: assignee.ID, Role: domain.MemberRole, JoinedAt: team.CreatedAt},
	} {
		if err := repos.TeamMember.Save(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	return db, repos, team, actor, assignee
}

func TestCreateTaskTransactionCommit(t *testing.T) {
	db, repos, team, actor, assignee := newTaskFixture(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewTaskService(repos.Transactions, repos.Task, repos.TaskHistory, nil, repos.TeamMember, nil, service.TaskCacheOptions{}, log)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, assignment := range []string{"unassigned", "assigned"} {
		t.Run(assignment, func(t *testing.T) {
			input := service.CreateTaskInput{TeamID: team.ID, Title: " \tЗадача\n", Description: " \nСохранить форматирование\n "}
			if assignment == "assigned" {
				input.AssigneeID = &assignee.ID
			}
			got, err := svc.Create(ctx, actor.ID, input)
			if err != nil || got == nil {
				t.Fatalf("Create()=(%v,%v)", got, err)
			}
			var row struct {
				ID          []byte       `db:"id"`
				TeamID      []byte       `db:"team_id"`
				CreatedBy   []byte       `db:"created_by"`
				AssigneeID  []byte       `db:"assignee_id"`
				Title       string       `db:"title"`
				Description string       `db:"description"`
				Status      string       `db:"status"`
				CreatedAt   time.Time    `db:"created_at"`
				UpdatedAt   time.Time    `db:"updated_at"`
				ClosedAt    sql.NullTime `db:"closed_at"`
				Version     uint32       `db:"version"`
			}
			if err := db.GetContext(ctx, &row, "SELECT id, team_id, created_by, assignee_id, title, description, status, created_at, updated_at, closed_at, version FROM tasks WHERE id = ?", got.ID[:]); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(row.ID, got.ID[:]) || !reflect.DeepEqual(row.TeamID, team.ID[:]) || !reflect.DeepEqual(row.CreatedBy, actor.ID[:]) || row.Title != "Задача" || row.Description != input.Description || row.Status != "todo" || row.Version != 1 || row.ClosedAt.Valid || !row.CreatedAt.Equal(got.CreatedAt) || !row.UpdatedAt.Equal(got.CreatedAt) {
				t.Errorf("unexpected persisted task: %+v", row)
			}
			if input.AssigneeID == nil {
				if row.AssigneeID != nil {
					t.Errorf("assignee=%v, want SQL NULL", row.AssigneeID)
				}
			} else if !reflect.DeepEqual(row.AssigneeID, assignee.ID[:]) {
				t.Error("assigned UUID not stored as binary")
			}
			var history struct {
				ID        []byte    `db:"id"`
				ChangedBy []byte    `db:"changed_by"`
				Changes   []byte    `db:"changes"`
				CreatedAt time.Time `db:"created_at"`
			}
			if err := db.GetContext(ctx, &history, "SELECT id, changed_by, changes, created_at FROM task_history WHERE task_id = ?", got.ID[:]); err != nil {
				t.Fatal(err)
			}
			id, err := uuid.FromBytes(history.ID)
			if err != nil || id == uuid.Nil || id == got.ID || !reflect.DeepEqual(history.ChangedBy, actor.ID[:]) || !history.CreatedAt.Equal(got.CreatedAt) {
				t.Errorf("unexpected history metadata: %+v, UUID error=%v", history, err)
			}
			var changes map[string]struct {
				Old any `json:"old"`
				New any `json:"new"`
			}
			if err := json.Unmarshal(history.Changes, &changes); err != nil {
				t.Fatal(err)
			}
			var assigned any
			if input.AssigneeID != nil {
				assigned = assignee.ID.String()
			}
			want := map[string]any{"title": "Задача", "description": input.Description, "status": "todo", "assignee_id": assigned}
			if len(changes) != 4 {
				t.Errorf("unexpected history: %s", history.Changes)
			}
			for field, value := range want {
				change, ok := changes[field]
				if !ok || change.Old != nil || !reflect.DeepEqual(change.New, value) {
					t.Errorf("history[%s]=%+v, want null -> %v", field, change, value)
				}
			}
		})
	}
}

type duplicateTaskHistoryRepository struct{ repo *mysql.TaskHistoryRepository }

func (r duplicateTaskHistoryRepository) List(ctx context.Context, filter domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
	return r.repo.List(ctx, filter)
}

func (r duplicateTaskHistoryRepository) Save(ctx context.Context, h *domain.TaskHistory) error {
	if err := r.repo.Save(ctx, h); err != nil {
		return err
	}
	return r.repo.Save(ctx, h)
}

func TestCreateTaskRollbackOnHistoryFailure(t *testing.T) {
	db, repos, team, actor, _ := newTaskFixture(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	history := duplicateTaskHistoryRepository{repo: repos.TaskHistory}
	svc := service.NewTaskService(repos.Transactions, repos.Task, history, nil, repos.TeamMember, nil, service.TaskCacheOptions{}, log)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := svc.Create(ctx, actor.ID, service.CreateTaskInput{TeamID: team.ID, Title: "Must roll back"})
	var mysqlErr *mysqldriver.MySQLError
	if got != nil || !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
		t.Fatalf("Create()=(%v,%v), want wrapped history duplicate-key error", got, err)
	}
	for _, query := range []string{"SELECT COUNT(*) FROM tasks", "SELECT COUNT(*) FROM task_history"} {
		var count int
		if err := db.GetContext(ctx, &count, query); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("%s=%d, want 0 after rollback", query, count)
		}
	}
}

func TestTaskRepositoryNullableAndErrors(t *testing.T) {
	db, repos, team, actor, assignee := newTaskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	closed := now.Add(time.Hour)
	task := &domain.Task{ID: uuid.New(), TeamID: team.ID, CreatedBy: actor.ID, AssigneeID: &assignee.ID, Title: "Closed", Description: "", Status: domain.Done, CreatedAt: now, UpdatedAt: closed, ClosedAt: &closed, Version: 2}
	if err := repos.Task.Save(ctx, task); err != nil {
		t.Fatal(err)
	}
	var stored sql.NullTime
	if err := db.GetContext(ctx, &stored, "SELECT closed_at FROM tasks WHERE id = ?", task.ID[:]); err != nil {
		t.Fatal(err)
	}
	if !stored.Valid || !stored.Time.Equal(closed) {
		t.Errorf("closed_at=%v, want %v", stored, closed)
	}
	var duplicate *mysqldriver.MySQLError
	if err := repos.Task.Save(ctx, task); !errors.As(err, &duplicate) || duplicate.Number != 1062 {
		t.Errorf("duplicate task error=%v", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := repos.Task.Save(canceled, task); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled Save()=%v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repos.Task.Save(ctx, task); err == nil {
		t.Error("closed database must fail")
	}
}

func TestTaskHistorySerializationAndErrors(t *testing.T) {
	db, repos, team, actor, assignee := newTaskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{ID: uuid.New(), TeamID: team.ID, CreatedBy: actor.ID, Title: "Task", Status: domain.Todo, CreatedAt: now, UpdatedAt: now, Version: 1}
	if err := repos.Task.Save(ctx, task); err != nil {
		t.Fatal(err)
	}
	history := &domain.TaskHistory{ID: uuid.New(), TaskID: task.ID, ChangedBy: actor.ID, CreatedAt: now, Changes: domain.TaskChanges{
		"title": {Old: "Old", New: "Новый заголовок"}, "description": {Old: "", New: "\nText\n"},
		"status": {Old: domain.Todo, New: domain.InProgress}, "assignee_id": {Old: nil, New: &assignee.ID},
	}}
	if err := repos.TaskHistory.Save(ctx, history); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := db.GetContext(ctx, &stored, "SELECT changes FROM task_history WHERE id = ?", history.ID[:]); err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(history.Changes)
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(stored, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wantJSON, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored JSON=%s, want %s", stored, wantJSON)
	}
	bad := *history
	bad.ID = uuid.New()
	bad.Changes = domain.TaskChanges{"bad": {New: make(chan int)}}
	var jsonErr *json.UnsupportedTypeError
	if err := repos.TaskHistory.Save(ctx, &bad); !errors.As(err, &jsonErr) {
		t.Errorf("serialization error=%v, want UnsupportedTypeError", err)
	}
	var count int
	if err := db.GetContext(ctx, &count, "SELECT COUNT(*) FROM task_history"); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("history count=%d, want 1 after serialization failure", count)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := repos.TaskHistory.Save(canceled, history); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled history Save()=%v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskHistory.Save(ctx, history); err == nil {
		t.Error("closed database must fail")
	}
}
