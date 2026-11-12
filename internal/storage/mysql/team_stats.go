package mysql

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type TeamStatsRepository struct {
	db      *sqlx.DB
	log     *slog.Logger
	timeout time.Duration
}

func NewTeamStatsRepository(db *sqlx.DB, log *slog.Logger, timeout time.Duration) *TeamStatsRepository {
	return &TeamStatsRepository{
		db:      db,
		log:     log,
		timeout: timeout,
	}
}

type rawTeamStats struct {
	Todo                int      `db:"todo"`
	InProgress          int      `db:"in_progress"`
	Done                int      `db:"done"`
	AverageCloseSeconds *float64 `db:"average_close_seconds"`
	ClosedTasks         *int     `db:"closed_tasks"`
	CommentsCount       int      `db:"comments_count"`
	AssigneeID          []byte   `db:"assignee_id"`
	AssigneeName        *string  `db:"assignee_name"`
}

func (t *TeamStatsRepository) Get(
	ctx context.Context,
	teamID uuid.UUID,
	since time.Time,
	qtyToppers int,
) (*domain.TeamStats, error) {
	if qtyToppers < 1 {
		return nil, fmt.Errorf("qtyToppers must be greater than zero")
	}
	t.log.Debug("MySQL: Team Stats: Get metrics", "team_id", teamID, "since", since, "qty", qtyToppers)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)
	var raw []rawTeamStats

	sql := `
		WITH team_tasks AS (
			SELECT id, status, assignee_id, created_at, closed_at
			FROM tasks
			WHERE team_id = ?
		),
		task_summary AS (
			SELECT 
				COUNT(CASE WHEN status = 'todo' THEN 1 END) AS todo,
				COUNT(CASE WHEN status = 'in_progress' THEN 1 END) AS in_progress,
				COUNT(CASE WHEN status = 'done' THEN 1 END) AS done,
				AVG(CASE WHEN closed_at IS NOT NULL AND status = 'done' THEN TIMESTAMPDIFF(MICROSECOND, created_at, closed_at) / 1000000.0 END) AS average_close_seconds
			FROM team_tasks
		),
		top_assignees AS (
			SELECT COUNT(*) AS closed_tasks, assignee_id
			FROM team_tasks
			WHERE closed_at IS NOT NULL AND status = 'done' AND assignee_id IS NOT NULL AND closed_at >= ?
			GROUP BY assignee_id
			ORDER BY closed_tasks DESC, assignee_id ASC
			LIMIT ?
		),
		comment_summary AS (
			SELECT COUNT(cs.id) AS comments_count
			FROM team_tasks
			LEFT JOIN task_comments AS cs ON team_tasks.id = cs.task_id
		)
		SELECT
			ts.todo,
			ts.in_progress,
			ts.done,
			ts.average_close_seconds,
			cs.comments_count,
			ta.assignee_id,
			u.name AS assignee_name,
			ta.closed_tasks
		FROM task_summary AS ts
		CROSS JOIN comment_summary AS cs
		LEFT JOIN top_assignees AS ta ON TRUE
		LEFT JOIN users AS u ON u.id = ta.assignee_id
		ORDER BY ta.closed_tasks DESC, ta.assignee_id ASC
	`
	err := executor.SelectContext(
		ctx,
		&raw,
		sql,
		uuidToBytes(teamID),
		since.UTC(),
		qtyToppers,
	)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Team stats: Failed to get metrics: %w", err)
	}
	toppers := make([]domain.AssigneeStats, 0, qtyToppers)
	var taskStats domain.TaskStatusCounts
	var averageCloseSeconds *float64
	commentsCount := 0
	isTaskStatsFilled := false
	for _, r := range raw {
		if !isTaskStatsFilled {
			isTaskStatsFilled = true
			taskStats.Todo = r.Todo
			taskStats.InProgress = r.InProgress
			taskStats.Done = r.Done

			averageCloseSeconds = r.AverageCloseSeconds
			commentsCount = r.CommentsCount
		}
		if r.AssigneeID == nil {
			continue
		}
		if r.AssigneeName == nil || r.ClosedTasks == nil {
			return nil, fmt.Errorf("MySQL: Team stats: incomplete assignee metrics")
		}
		assigneeID, err := bytesToUUID(r.AssigneeID)
		if err != nil {
			return nil, fmt.Errorf("MySQL: Team stats: Failed to parse AssigneeID: %w", err)
		}
		toppers = append(toppers, domain.AssigneeStats{
			UserID:      assigneeID,
			Name:        *r.AssigneeName,
			ClosedTasks: *r.ClosedTasks,
		})
	}
	t.log.Debug("MySQL: Team Stats: Get metrics successfully", "team_id", teamID, "since", since, "qty", qtyToppers)
	return &domain.TeamStats{
		TeamID:              teamID,
		TasksByStatus:       taskStats,
		TopAssignees:        toppers,
		AverageCloseSeconds: averageCloseSeconds,
		CommentsCount:       commentsCount,
	}, nil
}
