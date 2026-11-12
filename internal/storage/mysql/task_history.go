package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
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

func (t *TaskHistoryRepository) List(ctx context.Context, filter domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error) {
	t.log.Debug("MySQL: Task History: Get list", "filter", filter)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)

	var conditions strings.Builder
	var args []interface{}
	if filter.TaskID != nil {
		if *filter.TaskID == uuid.Nil {
			return nil, 0, fmt.Errorf("MySQL: Task History: %w: task_id must not be zero", domain.ErrInvalidInput)
		}
		conditions.WriteString(" AND task_id = ?")
		args = append(args, uuidToBytes(*filter.TaskID))
	}
	from := "FROM task_history WHERE 1 = 1" + conditions.String()

	var total int
	err := executor.GetContext(ctx, &total, "SELECT COUNT(*) "+from, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("MySQL: Task History: Failed to get total task history: %w", err)
	}

	var rows []rawTaskHistory
	args = append(args, filter.Limit, filter.Offset)
	err = executor.SelectContext(
		ctx,
		&rows,
		"SELECT id, task_id, changed_by, changes, created_at "+from+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?",
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("MySQL: Task History: Failed to get list: %w", err)
	}
	history := make([]domain.TaskHistory, 0, len(rows))
	for _, r := range rows {
		t, err := rawToTaskHistory(&r)
		if err != nil {
			return nil, 0, fmt.Errorf("MySQL: Task History: failed to parse history row: %w", err)
		}
		history = append(history, *t)
	}
	t.log.Debug("MySQL: Task History: Get list successfully", "filter", filter, "total", total)
	return history, total, nil
}

type rawTaskHistory struct {
	ID        []byte    `db:"id"`
	TaskID    []byte    `db:"task_id"`
	ChangedBy []byte    `db:"changed_by"`
	Changes   []byte    `db:"changes"`
	CreatedAt time.Time `db:"created_at"`
}

func rawToTaskHistory(r *rawTaskHistory) (*domain.TaskHistory, error) {
	id, err := bytesToUUID(r.ID)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Task History: Wrong parse id: %w", err)
	}
	taskID, err := bytesToUUID(r.TaskID)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Task History: Wrong parse task id: %w", err)
	}
	changedBy, err := bytesToUUID(r.ChangedBy)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Task History: Wrong parse created by: %w", err)
	}
	var changes domain.TaskChanges
	if err := json.Unmarshal(r.Changes, &changes); err != nil {
		return nil, fmt.Errorf("MySQL: Task History: Wrong parse changes: %w", err)
	}
	return &domain.TaskHistory{
		ID:        id,
		TaskID:    taskID,
		ChangedBy: changedBy,
		Changes:   changes,
		CreatedAt: r.CreatedAt,
	}, nil
}
