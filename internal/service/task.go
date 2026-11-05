package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type TaskRepository interface {
	Save(ctx context.Context, task *domain.Task) error
	List(ctx context.Context, filter domain.TaskFilter) ([]domain.Task, int, error)
	GetByID(ctx context.Context, taskID uuid.UUID) (*domain.Task, error)
	Update(ctx context.Context, task *domain.Task, expectedVersion uint32) error
}

type TaskHistoryRepository interface {
	Save(ctx context.Context, history *domain.TaskHistory) error
	List(ctx context.Context, filter domain.TaskHistoryFilter) ([]domain.TaskHistory, int, error)
}

type TaskCommentRepository interface {
	Save(ctx context.Context, comment *domain.TaskComment) error
	List(ctx context.Context, filter domain.TaskCommentFilter) ([]domain.TaskComment, int, error)
}

type TeamMemberRepo interface {
	GetByTeamAndUserIDForUpdate(ctx context.Context, teamID uuid.UUID, userID uuid.UUID) (*domain.TeamMember, error)
	GetByTeamAndUserID(ctx context.Context, teamID, userID uuid.UUID) (*domain.TeamMember, error)
}

type TaskService struct {
	tx  TransactionManager
	tr  TaskRepository
	thr TaskHistoryRepository
	tcr TaskCommentRepository
	tmr TeamMemberRepo
	log *slog.Logger
}

func NewTaskService(tx TransactionManager, tr TaskRepository, thr TaskHistoryRepository, tcr TaskCommentRepository, tmr TeamMemberRepo, log *slog.Logger) *TaskService {
	return &TaskService{
		tx:  tx,
		tr:  tr,
		thr: thr,
		tcr: tcr,
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
		if err := validateRole(actor.Role); err != nil {
			return fmt.Errorf("get actor membership: %w", err)
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

func (s *TaskService) List(
	ctx context.Context,
	actorID uuid.UUID,
	filter domain.TaskFilter,
) ([]domain.Task, int, error) {
	if actorID == uuid.Nil {
		return nil, 0, fmt.Errorf("get task list: %w: actor_id must not be nil", domain.ErrInvalidInput)
	}
	if filter.TeamID == nil || *filter.TeamID == uuid.Nil {
		return nil, 0, fmt.Errorf("get task list: %w: team_id must not be nil", domain.ErrInvalidInput)
	}
	if filter.Status != nil {
		if *filter.Status != domain.Todo && *filter.Status != domain.InProgress && *filter.Status != domain.Done {
			return nil, 0, fmt.Errorf("get task list: %w: wrong status", domain.ErrInvalidInput)
		}
	}
	if filter.AssigneeID != nil {
		if *filter.AssigneeID == uuid.Nil {
			return nil, 0, fmt.Errorf("get task list: %w: wrong assignee_id", domain.ErrInvalidInput)
		}
	}
	if err := validateLimit(filter.Limit); err != nil {
		return nil, 0, fmt.Errorf("get task list: %w", err)
	}
	if err := validateOffset(filter.Offset); err != nil {
		return nil, 0, fmt.Errorf("get task list: %w", err)
	}
	actor, err := s.tmr.GetByTeamAndUserID(ctx, *filter.TeamID, actorID)
	if err != nil {
		if errors.Is(err, domain.ErrTeamMemberNotFound) {
			return nil, 0, domain.ErrForbidden
		}
		return nil, 0, fmt.Errorf("get actor membership: %w", err)
	}
	if err := validateRole(actor.Role); err != nil {
		return nil, 0, domain.ErrForbidden
	}
	tasks, total, err := s.tr.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("get task list: %w: request error", err)
	}
	s.log.Debug("Task: get list of task sucessfully", "filter", filter, "total", total)
	return tasks, total, nil
}

type UpdateTaskInput struct {
	Version     uint32
	Title       *string
	Description *string
	Status      *domain.TaskStatus

	AssigneeID  *uuid.UUID
	AssigneeSet bool
}

func (s *TaskService) Update(ctx context.Context, actorID, taskID uuid.UUID, input UpdateTaskInput) (*domain.Task, error) {
	if actorID == uuid.Nil {
		return nil, fmt.Errorf("update task: %w: actor_id must not be nil", domain.ErrInvalidInput)
	}
	if taskID == uuid.Nil {
		return nil, fmt.Errorf("update task: %w: task_id must not be nil", domain.ErrInvalidInput)
	}
	if input.Version < 1 || input.Version > math.MaxUint32-1 {
		return nil, fmt.Errorf("update task: %w: version must contain at most 4294967294", domain.ErrInvalidInput)
	}

	s.log.Debug("Task: update task", "task_id", taskID, "actor_id", actorID, "task", input)
	var updatedTask *domain.Task
	err := s.tx.Do(ctx, func(txCtx context.Context) error {
		task, err := s.tr.GetByID(txCtx, taskID)
		if err != nil {
			return fmt.Errorf("update task: %w", err)
		}
		actor, err := s.tmr.GetByTeamAndUserIDForUpdate(txCtx, task.TeamID, actorID)
		if err != nil {
			if errors.Is(err, domain.ErrTeamMemberNotFound) {
				return domain.ErrForbidden
			}
			return fmt.Errorf("update task: get actor membership: %w", err)
		}
		if err := validateRole(actor.Role); err != nil {
			return fmt.Errorf("update task: %w", err)
		}

		if input.Version != task.Version {
			return fmt.Errorf("update task: %w", domain.ErrTaskVersionConflict)
		}
		if err := checkUpdateFieldPermissions(actorID, *task, input); err != nil {
			return fmt.Errorf("update task: %w", err)
		}
		title := task.Title
		if input.Title != nil {
			title, err = nameNormalize(*input.Title)
			if err != nil {
				return fmt.Errorf("update task: %w", err)
			}
		}
		description := task.Description
		if input.Description != nil {
			if err := validateDescription(*input.Description); err != nil {
				return fmt.Errorf("update task: %w", err)
			}
			description = *input.Description
		}
		status := task.Status
		if input.Status != nil {
			if err := validateStatus(*input.Status); err != nil {
				return fmt.Errorf("update task: %w", err)
			}
			status = *input.Status
		}
		assigneeID := task.AssigneeID
		if input.AssigneeSet {
			if input.AssigneeID != nil {
				if *input.AssigneeID == uuid.Nil {
					return fmt.Errorf("update task: %w: zero assignee_id", domain.ErrInvalidInput)
				}
				_, err := s.tmr.GetByTeamAndUserIDForUpdate(txCtx, task.TeamID, *input.AssigneeID)
				if err != nil {
					if errors.Is(err, domain.ErrTeamMemberNotFound) {
						return fmt.Errorf("update task: wrong assignee_id: %w", domain.ErrInvalidInput)
					}
					return fmt.Errorf("update task: get executor membership: %w", err)
				}
			}
			assigneeID = input.AssigneeID
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		closedAt := task.ClosedAt
		if status == domain.Done && task.Status != domain.Done {
			closedAt = &now
		} else if status != domain.Done {
			closedAt = nil
		}

		updatedTask = &domain.Task{
			ID:          taskID,
			TeamID:      task.TeamID,
			CreatedBy:   task.CreatedBy,
			AssigneeID:  assigneeID,
			Title:       title,
			Description: description,
			Status:      status,
			CreatedAt:   task.CreatedAt,
			UpdatedAt:   now,
			ClosedAt:    closedAt,
			Version:     task.Version,
		}

		taskChanges := domain.TaskChanges{}
		if updatedTask.Title != task.Title {
			taskChanges["title"] = domain.FieldChange{
				Old: task.Title,
				New: updatedTask.Title,
			}
		}
		if updatedTask.Description != task.Description {
			taskChanges["description"] = domain.FieldChange{
				Old: task.Description,
				New: updatedTask.Description,
			}
		}
		if updatedTask.Status != task.Status {
			taskChanges["status"] = domain.FieldChange{
				Old: task.Status,
				New: updatedTask.Status,
			}
		}
		oldID, newID := task.AssigneeID, updatedTask.AssigneeID
		assigneeChanged := (oldID == nil) != (newID == nil) ||
			(oldID != nil && newID != nil && *oldID != *newID)

		if assigneeChanged {
			taskChanges["assignee_id"] = domain.FieldChange{
				Old: oldID,
				New: newID,
			}
		}

		if len(taskChanges) == 0 {
			updatedTask = task
			return nil
		}
		if err := s.tr.Update(txCtx, updatedTask, input.Version); err != nil {
			return fmt.Errorf("update task: %w", err)
		}

		history := &domain.TaskHistory{
			ID:        uuid.New(),
			TaskID:    task.ID,
			ChangedBy: actorID,
			Changes:   taskChanges,
			CreatedAt: now,
		}
		if err := s.thr.Save(txCtx, history); err != nil {
			return fmt.Errorf("update task: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("update task: %w", err)
	}
	return updatedTask, nil
}

func validateRole(role domain.TeamRole) error {
	if role != domain.OwnerRole &&
		role != domain.AdminRole &&
		role != domain.MemberRole {
		return domain.ErrForbidden
	}
	return nil
}

func checkUpdateFieldPermissions(actorID uuid.UUID, task domain.Task, input UpdateTaskInput) error {
	isAuthor := actorID == task.CreatedBy
	isAssignee := task.AssigneeID != nil && actorID == *task.AssigneeID
	if !isAuthor {
		changesOtherFields := input.Title != nil ||
			input.Description != nil ||
			input.AssigneeSet

		if !isAssignee || changesOtherFields {
			return domain.ErrForbidden
		}
	}
	return nil
}

func validateStatus(status domain.TaskStatus) error {
	if status != domain.Todo &&
		status != domain.InProgress &&
		status != domain.Done {
		return domain.ErrInvalidInput
	}
	return nil
}

func (s *TaskService) History(
	ctx context.Context,
	actorID uuid.UUID,
	filter domain.TaskHistoryFilter,
) ([]domain.TaskHistory, int, error) {
	if actorID == uuid.Nil {
		return nil, 0, fmt.Errorf("get history: %w: actor_id must not be nil", domain.ErrInvalidInput)
	}
	if err := validateLimit(filter.Limit); err != nil {
		return nil, 0, fmt.Errorf("get history: %w", err)
	}
	if err := validateOffset(filter.Offset); err != nil {
		return nil, 0, fmt.Errorf("get history: %w", err)
	}
	if filter.TaskID == nil || *filter.TaskID == uuid.Nil {
		return nil, 0, fmt.Errorf("get history: %w: task_id must not be nil", domain.ErrInvalidInput)
	}
	task, err := s.tr.GetByID(ctx, *filter.TaskID)
	if err != nil {
		return nil, 0, fmt.Errorf("get history: %w", err)
	}
	actor, err := s.tmr.GetByTeamAndUserID(ctx, task.TeamID, actorID)
	if err != nil {
		if errors.Is(err, domain.ErrTeamMemberNotFound) {
			return nil, 0, fmt.Errorf("get history: %w", domain.ErrForbidden)
		}
		return nil, 0, fmt.Errorf("get history: %w", err)
	}
	if err := validateRole(actor.Role); err != nil {
		return nil, 0, fmt.Errorf("get history: %w", err)
	}
	history, total, err := s.thr.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("get history: %w: request error", err)
	}
	s.log.Debug("Task History: get list of history sucessfully", "filter", filter, "total", total)
	return history, total, nil
}

type CreateTaskCommentInput struct {
	Body string
}

func (s *TaskService) AddComment(
	ctx context.Context,
	actorID, taskID uuid.UUID,
	input CreateTaskCommentInput,
) (*domain.TaskComment, error) {
	if actorID == uuid.Nil {
		return nil, fmt.Errorf("create comment: %w: actor_id must not be nil", domain.ErrInvalidInput)
	}
	if taskID == uuid.Nil {
		return nil, fmt.Errorf("create comment: %w: task_id must not be nil", domain.ErrInvalidInput)
	}
	if strings.TrimSpace(input.Body) == "" {
		return nil, fmt.Errorf("create comment: %w: body must not be empty", domain.ErrInvalidInput)
	}
	if err := validateDescription(input.Body); err != nil {
		return nil, fmt.Errorf("create comment: %w", err)
	}
	s.log.Debug("Task: create a new comment", "actorID", actorID, "taskID", taskID, "body", input)
	var comment *domain.TaskComment
	err := s.tx.Do(ctx, func(txCtx context.Context) error {
		task, err := s.tr.GetByID(txCtx, taskID)
		if err != nil {
			return fmt.Errorf("create task comment: %w", err)
		}
		actor, err := s.tmr.GetByTeamAndUserIDForUpdate(txCtx, task.TeamID, actorID)
		if err != nil {
			if errors.Is(err, domain.ErrTeamMemberNotFound) {
				return domain.ErrForbidden
			}
			return fmt.Errorf("create task comment: get actor membership: %w", err)
		}
		if err := validateRole(actor.Role); err != nil {
			return fmt.Errorf("create task comment: %w", err)
		}
		comment = &domain.TaskComment{
			ID:        uuid.New(),
			TaskID:    taskID,
			CreatedBy: actorID,
			CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
			Body:      input.Body,
		}
		if err := s.tcr.Save(txCtx, comment); err != nil {
			return fmt.Errorf("create task comment: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create task comment: %w", err)
	}
	s.log.Debug("Task: create a new task comment successfully", "comment", comment)
	return comment, nil
}

func (s *TaskService) Comments(
	ctx context.Context,
	actorID uuid.UUID,
	filter domain.TaskCommentFilter,
) ([]domain.TaskComment, int, error) {
	if actorID == uuid.Nil {
		return nil, 0, fmt.Errorf("get comments: %w: actor_id must not be nil", domain.ErrInvalidInput)
	}
	if err := validateLimit(filter.Limit); err != nil {
		return nil, 0, fmt.Errorf("get comments: %w", err)
	}
	if err := validateOffset(filter.Offset); err != nil {
		return nil, 0, fmt.Errorf("get comments: %w", err)
	}
	if filter.TaskID == nil || *filter.TaskID == uuid.Nil {
		return nil, 0, fmt.Errorf("get comments: %w: task_id must not be nil", domain.ErrInvalidInput)
	}
	task, err := s.tr.GetByID(ctx, *filter.TaskID)
	if err != nil {
		return nil, 0, fmt.Errorf("get comments: %w", err)
	}
	actor, err := s.tmr.GetByTeamAndUserID(ctx, task.TeamID, actorID)
	if err != nil {
		if errors.Is(err, domain.ErrTeamMemberNotFound) {
			return nil, 0, fmt.Errorf("get comments: %w", domain.ErrForbidden)
		}
		return nil, 0, fmt.Errorf("get comments: %w", err)
	}
	if err := validateRole(actor.Role); err != nil {
		return nil, 0, fmt.Errorf("get comments: %w", err)
	}
	comments, total, err := s.tcr.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("get comments: %w: request error", err)
	}
	s.log.Debug("Task Comment: get list of comments sucessfully", "filter", filter, "total", total)
	return comments, total, nil
}
