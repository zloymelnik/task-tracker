package mysql_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

func seedUpdateTask(t *testing.T, repo *mysql.TaskRepository, team, actor uuid.UUID) domain.Task {
	t.Helper()
	now := time.Date(2026, 11, 1, 12, 0, 0, 123000000, time.UTC)
	task := domain.Task{ID: uuid.New(), TeamID: team, CreatedBy: actor, Title: "Original", Description: "Description", Status: domain.Todo, CreatedAt: now, UpdatedAt: now, Version: 1}
	if err := repo.Save(context.Background(), &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestTaskRepositoryGetAndUpdate(t *testing.T) {
	db, repos, team, actor, assignee := newTaskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	old := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
	got, err := repos.Task.GetByID(ctx, old.ID)
	if err != nil || got == nil || !reflect.DeepEqual(*got, old) {
		t.Fatalf("GetByID()=(%v,%v), want %+v", got, err, old)
	}
	missing, err := repos.Task.GetByID(ctx, uuid.New())
	if missing != nil || !errors.Is(err, domain.ErrTaskNotFound) {
		t.Errorf("missing GetByID()=(%v,%v)", missing, err)
	}

	closed := old.UpdatedAt.Add(time.Hour)
	updated := old
	updated.Title, updated.Description, updated.AssigneeID, updated.Status, updated.UpdatedAt, updated.ClosedAt = "Updated", "", &assignee.ID, domain.Done, closed, &closed
	// Attempts to change immutable fields must not reach SQL SET.
	updated.TeamID, updated.CreatedBy, updated.CreatedAt = uuid.New(), uuid.New(), closed
	if err := repos.Task.Update(ctx, &updated, 1); err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 {
		t.Errorf("version=%d, want 2", updated.Version)
	}
	want := updated
	want.TeamID, want.CreatedBy, want.CreatedAt = old.TeamID, old.CreatedBy, old.CreatedAt
	got, err = repos.Task.GetByID(ctx, old.ID)
	if err != nil || got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("updated task=(%v,%v), want %+v", got, err, want)
	}

	stale := old
	stale.Title = "Must not overwrite"
	if err := repos.Task.Update(ctx, &stale, 1); !errors.Is(err, domain.ErrTaskVersionConflict) {
		t.Errorf("stale update error=%v", err)
	}
	if stale.Version != 1 {
		t.Error("failed update changed in-memory version")
	}
	got, err = repos.Task.GetByID(ctx, old.ID)
	if err != nil || got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatal("stale update overwrote task")
	}
	missingTask := old
	missingTask.ID = uuid.New()
	if err := repos.Task.Update(ctx, &missingTask, 1); !errors.Is(err, domain.ErrTaskVersionConflict) {
		t.Errorf("missing update error=%v", err)
	}

	want.AssigneeID, want.ClosedAt, want.Status = nil, nil, domain.InProgress
	want.UpdatedAt = closed.Add(time.Hour)
	if err := repos.Task.Update(ctx, &want, 2); err != nil {
		t.Fatal(err)
	}
	got, err = repos.Task.GetByID(ctx, old.ID)
	if err != nil || got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("nullable update=(%v,%v), want %+v", got, err, want)
	}
	// Even an otherwise identical update changes the version, so zero rows
	// unambiguously means a missing task or version conflict here.
	if err := repos.Task.Update(ctx, &want, 3); err != nil || want.Version != 4 {
		t.Errorf("identical field update err=%v version=%d", err, want.Version)
	}
	for _, v := range []uint32{0, math.MaxUint32} {
		copyTask := want
		if err := repos.Task.Update(ctx, &copyTask, v); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("invalid version %d error=%v", v, err)
		}
		if copyTask.Version != want.Version {
			t.Error("invalid version changed task")
		}
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if task, err := repos.Task.GetByID(canceled, old.ID); task != nil || !errors.Is(err, context.Canceled) {
		t.Errorf("canceled read=(%v,%v)", task, err)
	}
	if err := repos.Task.Update(canceled, &want, want.Version); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled update=%v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Task.GetByID(ctx, old.ID); err == nil {
		t.Error("closed database read succeeded")
	}
	if err := repos.Task.Update(ctx, &want, want.Version); err == nil {
		t.Error("closed database update succeeded")
	}
}

type failAfterTaskHistorySave struct {
	repo *mysql.TaskHistoryRepository
	err  error
}

func (r failAfterTaskHistorySave) List(ctx context.Context, filter domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
	return r.repo.List(ctx, filter)
}

func (r failAfterTaskHistorySave) Save(ctx context.Context, history *domain.TaskHistory) error {
	if err := r.repo.Save(ctx, history); err != nil {
		return err
	}
	return r.err
}

func TestUpdateTaskTransaction(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "commit"
		if rollback {
			name = "rollback after history save"
		}
		t.Run(name, func(t *testing.T) {
			db, repos, team, actor, assignee := newTaskFixture(t)
			old := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			failure := errors.New("history save failed")
			var history service.TaskHistoryRepository = repos.TaskHistory
			if rollback {
				history = failAfterTaskHistorySave{repo: repos.TaskHistory, err: failure}
			}
			svc := service.NewTaskService(repos.Transactions, repos.Task, history, nil, repos.TeamMember, nil, service.TaskCacheOptions{}, log)
			title, status := "Changed", domain.Done
			got, err := svc.Update(ctx, actor.ID, old.ID, service.UpdateTaskInput{Version: 1, Title: &title, Status: &status, AssigneeSet: true, AssigneeID: &assignee.ID})
			stored, readErr := repos.Task.GetByID(ctx, old.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var count int
			if err := db.GetContext(ctx, &count, "SELECT COUNT(*) FROM task_history WHERE task_id = ?", old.ID[:]); err != nil {
				t.Fatal(err)
			}
			if rollback {
				if got != nil || !errors.Is(err, failure) || count != 0 || !reflect.DeepEqual(*stored, old) {
					t.Fatalf("rollback: got=%v err=%v count=%d stored=%+v", got, err, count, stored)
				}
				return
			}
			if err != nil || got == nil || count != 1 || !reflect.DeepEqual(*got, *stored) || got.Version != 2 || got.ClosedAt == nil || !got.ClosedAt.Equal(got.UpdatedAt) {
				t.Fatalf("commit: got=%v err=%v count=%d stored=%+v", got, err, count, stored)
			}
			var row struct {
				ChangedBy []byte    `db:"changed_by"`
				Changes   []byte    `db:"changes"`
				CreatedAt time.Time `db:"created_at"`
			}
			if err := db.GetContext(ctx, &row, "SELECT changed_by, changes, created_at FROM task_history WHERE task_id = ?", old.ID[:]); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(row.ChangedBy, actor.ID[:]) || !row.CreatedAt.Equal(got.UpdatedAt) {
				t.Errorf("incorrect history metadata: %+v", row)
			}
			var changes map[string]struct {
				Old any `json:"old"`
				New any `json:"new"`
			}
			if err := json.Unmarshal(row.Changes, &changes); err != nil {
				t.Fatal(err)
			}
			if len(changes) != 3 || changes["title"].Old != "Original" || changes["title"].New != title || changes["status"].Old != "todo" || changes["status"].New != "done" || changes["assignee_id"].Old != nil || changes["assignee_id"].New != assignee.ID.String() {
				t.Errorf("incorrect history JSON: %s", row.Changes)
			}
			// A no-op preserves both timestamps and version, and adds no history.
			again, err := svc.Update(ctx, actor.ID, old.ID, service.UpdateTaskInput{Version: got.Version, Title: &title})
			if err != nil || again == nil || !reflect.DeepEqual(*again, *got) {
				t.Errorf("no-op update=(%v,%v)", again, err)
			}
			if err := db.GetContext(ctx, &count, "SELECT COUNT(*) FROM task_history WHERE task_id = ?", old.ID[:]); err != nil || count != 1 {
				t.Errorf("no-op history count=%d err=%v", count, err)
			}
		})
	}
}

// Force both transactions to read the same version before either can update.
type synchronizedTaskReads struct {
	*mysql.TaskRepository
	ready   chan<- struct{}
	release <-chan struct{}
}

func (r synchronizedTaskReads) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	task, err := r.TaskRepository.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	select {
	case r.ready <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-r.release:
		return task, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestConcurrentTaskUpdates(t *testing.T) {
	db, repos, team, actor, _ := newTaskFixture(t)
	old := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ready, release := make(chan struct{}, 2), make(chan struct{})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewTaskService(repos.Transactions, synchronizedTaskReads{repos.Task, ready, release}, repos.TaskHistory, nil, repos.TeamMember, nil, service.TaskCacheOptions{}, log)
	type result struct {
		task *domain.Task
		err  error
	}
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for _, title := range []string{"First", "Second"} {
		workers.Add(1)
		go func(title string) {
			defer workers.Done()
			task, err := svc.Update(ctx, actor.ID, old.ID, service.UpdateTaskInput{Version: 1, Title: &title})
			results <- result{task, err}
		}(title)
	}
	// Drain goroutines even when the rendezvous fails, before fixture cleanup.
	defer func() { cancel(); workers.Wait() }()
	for range 2 {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("concurrent reads did not complete")
		}
	}
	close(release)
	var winner *domain.Task
	success, conflicts := 0, 0
	for range 2 {
		r := <-results
		if r.err == nil && r.task != nil {
			success++
			winner = r.task
		} else if errors.Is(r.err, domain.ErrTaskVersionConflict) && r.task == nil {
			conflicts++
		} else {
			t.Errorf("unexpected concurrent result: %+v", r)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d, want 1/1", success, conflicts)
	}
	stored, err := repos.Task.GetByID(ctx, old.ID)
	if err != nil || stored == nil || stored.Version != 2 || !reflect.DeepEqual(*stored, *winner) {
		t.Fatalf("stored=%v err=%v winner=%v", stored, err, winner)
	}
	var entries []struct {
		Changes []byte `db:"changes"`
	}
	if err := db.SelectContext(ctx, &entries, "SELECT changes FROM task_history WHERE task_id = ?", old.ID[:]); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("history entries=%d, want 1", len(entries))
	}
	var changes map[string]struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := json.Unmarshal(entries[0].Changes, &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes["title"].Old != old.Title || changes["title"].New != winner.Title {
		t.Errorf("history does not describe winner: %s", entries[0].Changes)
	}
}
