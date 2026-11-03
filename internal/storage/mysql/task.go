package mysql

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type TaskRepository struct {
	db      *sqlx.DB
	log     *slog.Logger
	timeout time.Duration
}

func NewTaskRepository(db *sqlx.DB, log *slog.Logger, timeout time.Duration) *TaskRepository {
	return &TaskRepository{
		db:      db,
		log:     log,
		timeout: timeout,
	}
}

func (t *TaskRepository) Save(ctx context.Context, task *domain.Task) error {
	t.log.Debug("MySQL: Task: Save task data", "id", task.ID, "team_id", task.TeamID, "title", task.Title)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)
	var assigneeID any
	if task.AssigneeID != nil {
		assigneeID = uuidToBytes(*task.AssigneeID)
	}
	_, err := executor.ExecContext(ctx,
		"INSERT INTO tasks (id, team_id, created_by, assignee_id, title, description, status, created_at, updated_at, closed_at, version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		uuidToBytes(task.ID),
		uuidToBytes(task.TeamID),
		uuidToBytes(task.CreatedBy),
		assigneeID,
		task.Title,
		task.Description,
		task.Status,
		task.CreatedAt,
		task.UpdatedAt,
		task.ClosedAt,
		task.Version,
	)
	if err != nil {
		return fmt.Errorf("MySQL: Task: Failed to save task data: %w", err)
	}
	t.log.Debug("MySQL: Task: Save task data sucecssfully", "id", task.ID, "team_id", task.TeamID, "title", task.Title)
	return nil
}
