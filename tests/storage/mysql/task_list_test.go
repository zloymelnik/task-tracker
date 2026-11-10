package mysql_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

func TestTaskRepositoryList(t *testing.T) {
	_, repos, team, actor, assignee := newTaskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Date(2026, 11, 3, 12, 0, 0, 123000000, time.UTC)
	other := domain.Team{ID: uuid.New(), Name: "Other", CreatedBy: actor.ID, CreatedAt: now}
	if err := repos.Team.Save(ctx, &other); err != nil {
		t.Fatal(err)
	}
	closed := now.Add(time.Hour)
	// IDs intentionally run opposite insertion order for equal timestamps.
	tasks := []domain.Task{
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000001"), TeamID: team.ID, CreatedBy: actor.ID, Title: "Older", Status: domain.Todo, CreatedAt: now.Add(-time.Hour), UpdatedAt: now, Version: 1},
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000002"), TeamID: team.ID, CreatedBy: actor.ID, AssigneeID: &assignee.ID, Title: "Задача", Description: "\nОписание\n", Status: domain.Done, CreatedAt: now, UpdatedAt: closed, ClosedAt: &closed, Version: 3},
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000003"), TeamID: team.ID, CreatedBy: actor.ID, AssigneeID: &assignee.ID, Title: "Progress", Status: domain.InProgress, CreatedAt: now, UpdatedAt: now, Version: 2},
		{ID: uuid.MustParse("00000000-0000-4000-8000-000000000004"), TeamID: other.ID, CreatedBy: actor.ID, AssigneeID: &assignee.ID, Title: "Other team", Status: domain.Done, CreatedAt: now.Add(time.Hour), UpdatedAt: now, Version: 1},
	}
	for i := range tasks {
		if err := repos.Task.Save(ctx, &tasks[i]); err != nil {
			t.Fatal(err)
		}
	}
	done, progress, todo := domain.Done, domain.InProgress, domain.Todo
	missingTeam, missingAssignee := uuid.New(), uuid.New()
	for _, tt := range []struct {
		name    string
		filter  domain.TaskFilter
		indices []int
		total   int
	}{
		{"no filters", domain.TaskFilter{Limit: 20}, []int{3, 2, 1, 0}, 4},
		{"team", domain.TaskFilter{TeamID: &team.ID, Limit: 20}, []int{2, 1, 0}, 3},
		{"first page", domain.TaskFilter{TeamID: &team.ID, Limit: 1}, []int{2}, 3},
		{"next page", domain.TaskFilter{TeamID: &team.ID, Limit: 1, Offset: 1}, []int{1}, 3},
		{"last page", domain.TaskFilter{TeamID: &team.ID, Limit: 2, Offset: 2}, []int{0}, 3},
		{"past end", domain.TaskFilter{TeamID: &team.ID, Limit: 2, Offset: 99}, nil, 3},
		{"status across teams", domain.TaskFilter{Status: &done, Limit: 20}, []int{3, 1}, 2},
		{"assignee across teams", domain.TaskFilter{AssigneeID: &assignee.ID, Limit: 20}, []int{3, 2, 1}, 3},
		{"combined", domain.TaskFilter{TeamID: &team.ID, Status: &done, AssigneeID: &assignee.ID, Limit: 20}, []int{1}, 1},
		{"progress", domain.TaskFilter{TeamID: &team.ID, Status: &progress, Limit: 20}, []int{2}, 1},
		{"unassigned task", domain.TaskFilter{TeamID: &team.ID, Status: &todo, Limit: 20}, []int{0}, 1},
		{"filtered page past end", domain.TaskFilter{TeamID: &team.ID, Status: &done, Limit: 1, Offset: 1}, nil, 1},
		{"no matching team", domain.TaskFilter{TeamID: &missingTeam, Limit: 20}, nil, 0},
		{"no matching assignee", domain.TaskFilter{TeamID: &team.ID, AssigneeID: &missingAssignee, Limit: 20}, nil, 0},
		{"no matching combination", domain.TaskFilter{Status: &todo, AssigneeID: &assignee.ID, Limit: 20}, nil, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := repos.Task.List(ctx, tt.filter)
			if err != nil || got == nil || len(got) != len(tt.indices) || total != tt.total {
				t.Fatalf("List()=(%v,%d,%v), want %d rows and total %d", got, total, err, len(tt.indices), tt.total)
			}
			for i, index := range tt.indices {
				if !reflect.DeepEqual(got[i], tasks[index]) {
					t.Errorf("row[%d]=%+v, want %+v", i, got[i], tasks[index])
				}
			}
		})
	}
}

func TestTaskRepositoryListErrors(t *testing.T) {
	db, repos, _, _, _ := newTaskFixture(t)
	zero := uuid.Nil
	for _, filter := range []domain.TaskFilter{{TeamID: &zero, Limit: 20}, {AssigneeID: &zero, Limit: 20}} {
		got, total, err := repos.Task.List(context.Background(), filter)
		if got != nil || total != 0 || !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("List()=(%v,%d,%v), want invalid input", got, total, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, total, err := repos.Task.List(ctx, domain.TaskFilter{Limit: 20})
	if got != nil || total != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("canceled List()=(%v,%d,%v)", got, total, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, total, err = repos.Task.List(context.Background(), domain.TaskFilter{Limit: 20})
	if got != nil || total != 0 || err == nil {
		t.Errorf("closed DB List()=(%v,%d,%v)", got, total, err)
	}
}
