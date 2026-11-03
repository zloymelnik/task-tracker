package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type TaskRepository interface {
	Save(ctx context.Context, task *domain.Task) error
}

type TaskHistoryRepository interface {
	Save(ctx context.Context, history *domain.TaskHistory) error
}

type TeamMemberRepo interface {
	GetByTeamAndUserIDForUpdate(ctx context.Context, teamID uuid.UUID, userID uuid.UUID) (*domain.TeamMember, error)
}

type TaskService struct {
	tx  TransactionManager
	tr  TaskRepository
	thr TaskHistoryRepository
	tmr TeamMemberRepo
	log *slog.Logger
}

func NewTaskService(tx TransactionManager, tr TaskRepository, thr TaskHistoryRepository, tmr TeamMemberRepo, log *slog.Logger) *TaskService {
	return &TaskService{
		tx:  tx,
		tr:  tr,
		thr: thr,
		tmr: tmr,
		log: log,
	}
}

type CreateTaskInput struct {
	TeamID      uuid.UUID
	Title       string
	Description string
	AssigneeID  *uuid.UUID
}

func (s *TaskService) Create(
	ctx context.Context,
	actorID uuid.UUID,
	input CreateTaskInput,
) (*domain.Task, error) {
	if actorID == uuid.Nil {
		return nil, fmt.Errorf("create task: %w: actor_id must not be nil", domain.ErrInvalidInput)
	}
	if input.TeamID == uuid.Nil {
		return nil, fmt.Errorf("create task: %w: team_id must not be nil", domain.ErrInvalidInput)
	}
	title, err := nameNormalize(input.Title)
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}
	if err := validateDescription(input.Description); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}
	if input.AssigneeID != nil && *input.AssigneeID == uuid.Nil {
		return nil, fmt.Errorf("create task: %w: assigne_id must be nil or real user_id", domain.ErrInvalidInput)
	}
	s.log.Debug("Task: create a new task", "task", title)
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ID:          uuid.New(),
		TeamID:      input.TeamID,
		CreatedBy:   actorID,
		AssigneeID:  input.AssigneeID,
		Title:       title,
		Description: input.Description,
		Status:      domain.Todo,
		CreatedAt:   now,
		UpdatedAt:   now,
		ClosedAt:    nil,
		Version:     1,
	}
	taskChanges := domain.TaskChanges{
		"title":       {Old: nil, New: title},
		"description": {Old: nil, New: input.Description},
		"status":      {Old: nil, New: task.Status},
		"assignee_id": {Old: nil, New: task.AssigneeID},
	}
	taskHistory := &domain.TaskHistory{
		ID:        uuid.New(),
		TaskID:    task.ID,
		ChangedBy: actorID,
		Changes:   taskChanges,
		CreatedAt: now,
	}
	err = s.tx.Do(ctx, func(txCtx context.Context) error {
		actor, err := s.tmr.GetByTeamAndUserIDForUpdate(txCtx, input.TeamID, actorID)
		if err != nil {
			if errors.Is(err, domain.ErrTeamMemberNotFound) {
				return domain.ErrForbidden
			}
			return fmt.Errorf("get actor membership: %w", err)
		}
		if actor.Role != domain.OwnerRole &&
			actor.Role != domain.AdminRole &&
			actor.Role != domain.MemberRole {
			return domain.ErrForbidden
		}

		if input.AssigneeID != nil && actorID != *input.AssigneeID {
			_, err := s.tmr.GetByTeamAndUserIDForUpdate(txCtx, input.TeamID, *input.AssigneeID)
			if err != nil {
				if errors.Is(err, domain.ErrTeamMemberNotFound) {
					return fmt.Errorf("wrong assignee_id: %w", domain.ErrInvalidInput)
				}
				return fmt.Errorf("get executor membership: %w", err)
			}
		}

		if err := s.tr.Save(txCtx, task); err != nil {
			return fmt.Errorf("create task: %w", err)
		}
		if err := s.thr.Save(txCtx, taskHistory); err != nil {
			return fmt.Errorf("create task history: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}
	s.log.Debug("Task: create a new task successfully", "task_id", task.ID, "task", title)
	return task, nil
}

func validateDescription(descr string) error {
	if len(descr) > 65535 {
		return fmt.Errorf("%w: description must contain at most 65535 bytes", domain.ErrInvalidInput)
	}
	return nil
}
