package mysql

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type TaskCommentRepository struct {
	db      *sqlx.DB
	log     *slog.Logger
	timeout time.Duration
}

func NewTaskCommentRepository(db *sqlx.DB, log *slog.Logger, timeout time.Duration) *TaskCommentRepository {
	return &TaskCommentRepository{
		db:      db,
		log:     log,
		timeout: timeout,
	}
}

func (t *TaskCommentRepository) Save(ctx context.Context, comment *domain.TaskComment) error {
	t.log.Debug("MySQL: Task Comment: Save task comment data", "comment", comment)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)

	_, err := executor.ExecContext(ctx,
		"INSERT INTO task_comments (id, task_id, created_by, body, created_at) VALUES (?, ?, ?, ?, ?)",
		uuidToBytes(comment.ID),
		uuidToBytes(comment.TaskID),
		uuidToBytes(comment.CreatedBy),
		comment.Body,
		comment.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("MySQL: Task Comment: Failed to save task comment data: %w", err)
	}
	t.log.Debug("MySQL: Task Comment: Save task comment data succesfully", "comment", comment)
	return nil
}

func (t *TaskCommentRepository) List(ctx context.Context, filter domain.TaskCommentFilter) ([]domain.TaskComment, int, error) {
	t.log.Debug("MySQL: Task Comment: Get list", "filter", filter)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)

	var conditions strings.Builder
	var args []interface{}
	if filter.TaskID != nil {
		if *filter.TaskID == uuid.Nil {
			return nil, 0, fmt.Errorf("MySQL: Task Comment: %w: task_id must not be zero", domain.ErrInvalidInput)
		}
		conditions.WriteString(" AND task_id = ?")
		args = append(args, uuidToBytes(*filter.TaskID))
	}
	from := "FROM task_comments WHERE 1 = 1" + conditions.String()

	var total int
	err := executor.GetContext(ctx, &total, "SELECT COUNT(*) "+from, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("MySQL: Task Comment: Failed to get total task comments: %w", err)
	}

	var rows []rawTaskComment
	args = append(args, filter.Limit, filter.Offset)
	err = executor.SelectContext(
		ctx,
		&rows,
		"SELECT id, task_id, created_by, body, created_at "+from+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?",
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("MySQL: Task Comment: Failed to get list: %w", err)
	}
	comments := make([]domain.TaskComment, 0, len(rows))
	for _, r := range rows {
		t, err := rawToTaskComment(&r)
		if err != nil {
			return nil, 0, fmt.Errorf("MySQL: Task Comment: failed to parse comments row: %w", err)
		}
		comments = append(comments, *t)
	}
	t.log.Debug("MySQL: Task Comment: Get list successfully", "filter", filter, "total", total)
	return comments, total, nil
}

type rawTaskComment struct {
	ID        []byte    `db:"id"`
	TaskID    []byte    `db:"task_id"`
	CreatedBy []byte    `db:"created_by"`
	Body      string    `db:"body"`
	CreatedAt time.Time `db:"created_at"`
}

func rawToTaskComment(r *rawTaskComment) (*domain.TaskComment, error) {
	id, err := bytesToUUID(r.ID)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Task Comment: Wrong parse id: %w", err)
	}
	taskID, err := bytesToUUID(r.TaskID)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Task Comment: Wrong parse task id: %w", err)
	}
	createdBy, err := bytesToUUID(r.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Task Comment: Wrong parse created by: %w", err)
	}
	return &domain.TaskComment{
		ID:        id,
		TaskID:    taskID,
		CreatedBy: createdBy,
		Body:      r.Body,
		CreatedAt: r.CreatedAt,
	}, nil
}
