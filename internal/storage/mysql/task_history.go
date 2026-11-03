package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/types"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type TaskHistoryRepository struct {
	db      *sqlx.DB
	log     *slog.Logger
	timeout time.Duration
}

func NewTaskHistoryRepository(db *sqlx.DB, log *slog.Logger, timeout time.Duration) *TaskHistoryRepository {
	return &TaskHistoryRepository{
		db:      db,
		log:     log,
		timeout: timeout,
	}
}

func (t *TaskHistoryRepository) Save(ctx context.Context, history *domain.TaskHistory) error {
	t.log.Debug("MySQL: Task History: Save task history data", "id", history.ID, "task_id", history.TaskID)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)

	jsonChanges, err := json.Marshal(history.Changes)
	if err != nil {
		return fmt.Errorf("MySQL: Task History: Failed to convert changes to json: %w", err)
	}

	_, err = executor.ExecContext(ctx,
		"INSERT INTO task_history (id, task_id, changed_by, changes, created_at) VALUES (?, ?, ?, ?, ?)",
		uuidToBytes(history.ID),
		uuidToBytes(history.TaskID),
		uuidToBytes(history.ChangedBy),
		types.JSONText(jsonChanges),
		history.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("MySQL: Task: Failed to save task history data: %w", err)
	}
	t.log.Debug("MySQL: Task History: Save task history data succesfully", "id", history.ID, "task_id", history.TaskID)
	return nil
}
