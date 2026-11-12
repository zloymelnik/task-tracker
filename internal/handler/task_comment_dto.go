package handler

import (
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type CreateTaskCommentRequest struct {
	// Body must contain non-whitespace text and be at most 65535 bytes.
	Body string `json:"body" binding:"required" minLength:"1" example:"Отчёт готов к проверке"`
}

func (r CreateTaskCommentRequest) ToInput() service.CreateTaskCommentInput {
	return service.CreateTaskCommentInput{
		Body: r.Body,
	}
}

type TaskCommentResponse struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	TaskID    uuid.UUID `json:"task_id" format:"uuid"`
	CreatedBy uuid.UUID `json:"created_by" format:"uuid"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
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
