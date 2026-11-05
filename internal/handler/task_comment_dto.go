package handler

import (
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type CreateTaskCommentRequest struct {
	Body string `json:"body"`
}

func (r CreateTaskCommentRequest) ToInput() service.CreateTaskCommentInput {
	return service.CreateTaskCommentInput{
		Body: r.Body,
	}
}

type TaskCommentResponse struct {
	ID        uuid.UUID `json:"id"`
	TaskID    uuid.UUID `json:"task_id"`
	CreatedBy uuid.UUID `json:"created_by"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type ListTaskCommentsResponse struct {
	Items  []TaskCommentResponse `json:"items"`
	Total  int                   `json:"total"`
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
}

func NewCreateTaskCommentResponse(comment domain.TaskComment) TaskCommentResponse {
	return TaskCommentResponse{
		ID:        comment.ID,
		TaskID:    comment.TaskID,
		CreatedBy: comment.CreatedBy,
		Body:      comment.Body,
		CreatedAt: comment.CreatedAt,
	}
}

func NewCreateListTaskCommentsResponse(comments []domain.TaskComment, total, limit, offset int) ListTaskCommentsResponse {
	items := make([]TaskCommentResponse, 0, len(comments))
	for _, comment := range comments {
		items = append(items, NewCreateTaskCommentResponse(comment))
	}
	return ListTaskCommentsResponse{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}
}
