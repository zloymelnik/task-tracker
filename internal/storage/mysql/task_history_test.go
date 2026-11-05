package mysql_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

func TestTaskHistoryRepositoryList(t *testing.T) {
	_, repos, team, actor, assignee := newTaskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	task := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
	other := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
	now := task.CreatedAt
	entries := []domain.TaskHistory{
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000001"), TaskID: task.ID, ChangedBy: actor.ID, Changes: domain.TaskChanges{"title": {Old: nil, New: "Создана"}, "assignee_id": {Old: nil, New: nil}}, CreatedAt: now.Add(-time.Hour)},
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000002"), TaskID: task.ID, ChangedBy: assignee.ID, Changes: domain.TaskChanges{"status": {Old: domain.Todo, New: domain.Done}}, CreatedAt: now},
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000003"), TaskID: task.ID, ChangedBy: actor.ID, Changes: domain.TaskChanges{"assignee_id": {Old: &assignee.ID, New: nil}, "description": {Old: "\nОписание\n", New: ""}}, CreatedAt: now},
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000004"), TaskID: other.ID, ChangedBy: actor.ID, Changes: domain.TaskChanges{"assignee_id": {Old: nil, New: &assignee.ID}}, CreatedAt: now.Add(time.Hour)},
	}
	for i := range entries {
		if err := repos.TaskHistory.Save(ctx, &entries[i]); err != nil {
			t.Fatal(err)
		}
	}
	unknown := uuid.New()
	for _, tt := range []struct {
		name    string
		filter  domain.TaskHistoryFilter
		indices []int
		total   int
	}{
		{"all for task", domain.TaskHistoryFilter{TaskID: &task.ID, Limit: 20}, []int{2, 1, 0}, 3},
		{"first page", domain.TaskHistoryFilter{TaskID: &task.ID, Limit: 1}, []int{2}, 3},
		{"next page", domain.TaskHistoryFilter{TaskID: &task.ID, Limit: 1, Offset: 1}, []int{1}, 3},
		{"last page", domain.TaskHistoryFilter{TaskID: &task.ID, Limit: 2, Offset: 2}, []int{0}, 3},
		{"past end", domain.TaskHistoryFilter{TaskID: &task.ID, Limit: 2, Offset: 100}, nil, 3},
		{"other task", domain.TaskHistoryFilter{TaskID: &other.ID, Limit: 20}, []int{3}, 1},
		{"no matches", domain.TaskHistoryFilter{TaskID: &unknown, Limit: 20}, nil, 0},
		{"no filter", domain.TaskHistoryFilter{Limit: 20}, []int{3, 2, 1, 0}, 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := repos.TaskHistory.List(ctx, tt.filter)
			if err != nil || got == nil || len(got) != len(tt.indices) || total != tt.total {
				t.Fatalf("List()=(%v,%d,%v), want %d rows and total %d", got, total, err, len(tt.indices), tt.total)
			}
			for i, index := range tt.indices {
				want := entries[index]
				if got[i].ID != want.ID || got[i].TaskID != want.TaskID || got[i].ChangedBy != want.ChangedBy || !got[i].CreatedAt.Equal(want.CreatedAt) {
					t.Errorf("metadata[%d]=%+v, want %+v", i, got[i], want)
				}
				actual, e1 := json.Marshal(got[i].Changes)
				expected, e2 := json.Marshal(want.Changes)
				if e1 != nil || e2 != nil || string(actual) != string(expected) {
					t.Errorf("changes[%d]=%s, want %s (%v/%v)", i, actual, expected, e1, e2)
				}
			}
		})
	}
}

func TestTaskHistoryRepositoryListInTransaction(t *testing.T) {
	_, repos, team, actor, _ := newTaskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	task := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
	history := domain.TaskHistory{ID: uuid.New(), TaskID: task.ID, ChangedBy: actor.ID, Changes: domain.TaskChanges{"title": {Old: "Old", New: "New"}}, CreatedAt: task.CreatedAt}
	rollback := errors.New("force rollback")
	err := repos.Transactions.Do(ctx, func(txCtx context.Context) error {
		if err := repos.TaskHistory.Save(txCtx, &history); err != nil {
			return err
		}
		rows, total, err := repos.TaskHistory.List(txCtx, domain.TaskHistoryFilter{TaskID: &task.ID, Limit: 20})
		if err != nil {
			return err
		}
		if total != 1 || len(rows) != 1 || !reflect.DeepEqual(rows[0], history) {
			t.Errorf("uncommitted history not visible: %+v total=%d", rows, total)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	rows, total, err := repos.TaskHistory.List(ctx, domain.TaskHistoryFilter{TaskID: &task.ID, Limit: 20})
	if err != nil || rows == nil || len(rows) != 0 || total != 0 {
		t.Errorf("rolled-back history visible: rows=%v total=%d err=%v", rows, total, err)
	}
}

func TestTaskHistoryRepositoryListErrors(t *testing.T) {
	db, repos, team, actor, _ := newTaskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	zero := uuid.Nil
	rows, total, err := repos.TaskHistory.List(ctx, domain.TaskHistoryFilter{TaskID: &zero, Limit: 20})
	if rows != nil || total != 0 || !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("zero task List()=(%v,%d,%v)", rows, total, err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	rows, total, err = repos.TaskHistory.List(canceled, domain.TaskHistoryFilter{Limit: 20})
	if rows != nil || total != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("canceled List()=(%v,%d,%v)", rows, total, err)
	}
	// MySQL accepts a JSON array, but domain.TaskChanges requires an object.
	task := seedUpdateTask(t, repos.Task, team.ID, actor.ID)
	history := domain.TaskHistory{ID: uuid.New(), TaskID: task.ID, ChangedBy: actor.ID, Changes: domain.TaskChanges{}, CreatedAt: task.CreatedAt}
	if err := repos.TaskHistory.Save(ctx, &history); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE task_history SET changes = ? WHERE id = ?", "[]", history.ID[:]); err != nil {
		t.Fatal(err)
	}
	rows, total, err = repos.TaskHistory.List(ctx, domain.TaskHistoryFilter{TaskID: &task.ID, Limit: 20})
	var shapeErr *json.UnmarshalTypeError
	if rows != nil || total != 0 || !errors.As(err, &shapeErr) {
		t.Errorf("bad JSON shape List()=(%v,%d,%v), want wrapped decoding error", rows, total, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	rows, total, err = repos.TaskHistory.List(ctx, domain.TaskHistoryFilter{Limit: 20})
	if rows != nil || total != 0 || err == nil {
		t.Errorf("closed DB List()=(%v,%d,%v)", rows, total, err)
	}
}
