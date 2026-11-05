package handler

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type CreateTaskRequest struct {
	TeamID      uuid.UUID  `json:"team_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	AssigneeID  *uuid.UUID `json:"assignee_id"`
}

func (r CreateTaskRequest) ToInput() service.CreateTaskInput {
	return service.CreateTaskInput{
		TeamID:      r.TeamID,
		Title:       r.Title,
		Description: r.Description,
		AssigneeID:  r.AssigneeID,
	}
}

type CreateTaskResponse struct {
	ID          uuid.UUID         `json:"id"`
	TeamID      uuid.UUID         `json:"team_id"`
	CreatedBy   uuid.UUID         `json:"created_by"`
	AssigneeID  *uuid.UUID        `json:"assignee_id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      domain.TaskStatus `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	ClosedAt    *time.Time        `json:"closed_at"`
	Version     uint32            `json:"version"`
}

func NewCreateTaskResponse(task domain.Task) CreateTaskResponse {
	return CreateTaskResponse{
		ID:          task.ID,
		TeamID:      task.TeamID,
		CreatedBy:   task.CreatedBy,
		AssigneeID:  task.AssigneeID,
		Title:       task.Title,
		Description: task.Description,
		Status:      task.Status,
		CreatedAt:   task.CreatedAt,
		UpdatedAt:   task.UpdatedAt,
		ClosedAt:    task.ClosedAt,
		Version:     task.Version,
	}
}

type ListTasksResponse struct {
	Items  []CreateTaskResponse `json:"items"`
	Total  int                  `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}

func NewCreateTaskListResponse(tasks []domain.Task, total, limit, offset int) ListTasksResponse {
	items := make([]CreateTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, NewCreateTaskResponse(task))
	}
	return ListTasksResponse{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}
}

type UpdateTaskRequest struct {
	Version     uint32             `json:"version"`
	Title       *string            `json:"title"`
	Description *string            `json:"description"`
	Status      *domain.TaskStatus `json:"status"`
	AssigneeID  json.RawMessage    `json:"assignee_id"`
}

func (r UpdateTaskRequest) ToInput() (service.UpdateTaskInput, error) {
	input := service.UpdateTaskInput{
		Version:     r.Version,
		Title:       r.Title,
		Description: r.Description,
		Status:      r.Status,
	}
	if len(r.AssigneeID) > 0 {
		input.AssigneeSet = true
		if err := json.Unmarshal(r.AssigneeID, &input.AssigneeID); err != nil {
			return service.UpdateTaskInput{}, fmt.Errorf("%w: invalid assignee_id", domain.ErrInvalidInput)
		}
	}
	return input, nil
}
