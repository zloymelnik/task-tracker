package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
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

func (t *TaskRepository) List(ctx context.Context, filter domain.TaskFilter) ([]domain.Task, int, error) {
	t.log.Debug("MySQL: Task: Gets tasks", "filter", filter)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)

	var conditions strings.Builder
	var args []interface{}
	if filter.TeamID != nil {
		if *filter.TeamID == uuid.Nil {
			return nil, 0, fmt.Errorf("MySQL: Task: %w: team_id must not be zero", domain.ErrInvalidInput)
		}
		conditions.WriteString(" AND team_id = ?")
		args = append(args, uuidToBytes(*filter.TeamID))
	}
	if filter.Status != nil {
		conditions.WriteString(" AND status = ?")
		args = append(args, filter.Status)
	}
	if filter.AssigneeID != nil {
		if *filter.AssigneeID == uuid.Nil {
			return nil, 0, fmt.Errorf("MySQL: Task: %w: assignee_id must not be zero", domain.ErrInvalidInput)
		}
		conditions.WriteString(" AND assignee_id = ?")
		args = append(args, uuidToBytes(*filter.AssigneeID))
	}
	from := "FROM tasks WHERE 1 = 1" + conditions.String()

	var total int
	err := executor.GetContext(ctx, &total, "SELECT COUNT(*) "+from, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("MySQL: Task: Failed to get total tasks: %w", err)
	}

	var rawTasks []rawTask
	args = append(args, filter.Limit, filter.Offset)
	err = executor.SelectContext(
		ctx,
		&rawTasks,
		"SELECT id, team_id, created_by, assignee_id, title, description, status, created_at, updated_at, closed_at, version "+from+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?",
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("MySQL: Task: Failed to get ltasks: %w", err)
	}
	tasks := make([]domain.Task, 0, len(rawTasks))
	for _, r := range rawTasks {
		t, err := rawToTask(&r)
		if err != nil {
			return nil, 0, fmt.Errorf("MySQL: Task: failed to parse task row: %w", err)
		}
		tasks = append(tasks, *t)
	}
	return tasks, total, nil
}

type rawTask struct {
	ID          []byte     `db:"id"`
	TeamID      []byte     `db:"team_id"`
	CreatedBy   []byte     `db:"created_by"`
	AssigneeID  []byte     `db:"assignee_id"`
	Title       string     `db:"title"`
	Description string     `db:"description"`
	Status      string     `db:"status"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
	ClosedAt    *time.Time `db:"closed_at"`
	Version     uint32     `db:"version"`
}

func rawToTask(r *rawTask) (*domain.Task, error) {
	id, err := bytesToUUID(r.ID)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Task: Wrong parse task id: %w", err)
	}
	teamID, err := bytesToUUID(r.TeamID)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Task: Wrong parse team id: %w", err)
	}
	createdBy, err := bytesToUUID(r.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Task: Wrong parse created id: %w", err)
	}
	var assigneeID *uuid.UUID
	if r.AssigneeID != nil {
		parsed, err := bytesToUUID(r.AssigneeID)
		if err != nil {
			return nil, fmt.Errorf("parse assignee id: %w", err)
		}
		assigneeID = &parsed
	}
	return &domain.Task{
		ID:          id,
		TeamID:      teamID,
		CreatedBy:   createdBy,
		AssigneeID:  assigneeID,
		Title:       r.Title,
		Description: r.Description,
		Status:      domain.TaskStatus(r.Status),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		ClosedAt:    r.ClosedAt,
		Version:     r.Version,
	}, nil
}

func (t *TaskRepository) GetByID(ctx context.Context, taskID uuid.UUID) (*domain.Task, error) {
	t.log.Debug("MySQL: Tasks: Gets task by id", "id", taskID)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)

	var raw rawTask
	err := executor.GetContext(ctx, &raw,
		`SELECT id, team_id, created_by, assignee_id, title, description, status, created_at, updated_at, closed_at, version
		FROM tasks
		WHERE id = ?`,
		uuidToBytes(taskID),
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrTaskNotFound
		}
		return nil, fmt.Errorf("MySQL: Task: Failed to get task row: %w", err)
	}
	task, err := rawToTask(&raw)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Task: Failed to parse task row: %w", err)
	}
	t.log.Debug("MySQL: task: Gets task data for id successfully", "task", task)
	return task, nil
}

func (t *TaskRepository) Update(ctx context.Context, task *domain.Task, expectedVersion uint32) error {
	if expectedVersion < 1 || expectedVersion > math.MaxUint32-1 {
		return fmt.Errorf("MySQL: Task: Failed to update task by id: %w", domain.ErrInvalidInput)
	}
	t.log.Debug("MySQL: Tasks: Updates task by id", "task", task, "expectedVersion", expectedVersion)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)

	var assigneeID any
	if task.AssigneeID != nil {
		assigneeID = uuidToBytes(*task.AssigneeID)
	}
	res, err := executor.ExecContext(
		ctx,
		`UPDATE tasks
		SET title = ?,
			description = ?,
			assignee_id = ?,
			status = ?,
			updated_at = ?,
			closed_at = ?,
			version = version + 1
		WHERE id = ? AND version = ?`,
		task.Title,
		task.Description,
		assigneeID,
		task.Status,
		task.UpdatedAt,
		task.ClosedAt,
		uuidToBytes(task.ID),
		expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("MySQL: Task: Failed to update task by id: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("MySQL: Task: Failed to update task by id: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("MySQL: Tasks: Failed to update task by id: %w", domain.ErrTaskVersionConflict)
	}
	task.Version = expectedVersion + 1
	return nil
}
