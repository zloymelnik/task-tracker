package handler

import (
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type TaskHistoryResponse struct {
	ID        uuid.UUID          `json:"id" format:"uuid"`
	TaskID    uuid.UUID          `json:"task_id" format:"uuid"`
	ChangedBy uuid.UUID          `json:"changed_by" format:"uuid"`
	Changes   domain.TaskChanges `json:"changes"`
	CreatedAt time.Time          `json:"created_at" format:"date-time"`
}

type ListTaskHistoryResponse struct {
	Items  []TaskHistoryResponse `json:"items"`
	Total  int                   `json:"total"`
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
}

func NewCreateHistoryResponse(h domain.TaskHistory) TaskHistoryResponse {
	return TaskHistoryResponse{
		ID:        h.ID,
		TaskID:    h.TaskID,
		ChangedBy: h.ChangedBy,
		Changes:   h.Changes,
		CreatedAt: h.CreatedAt,
	}
}

func NewCreateListTaskHistoryResponse(history []domain.TaskHistory, total, limit, offset int) ListTaskHistoryResponse {
	items := make([]TaskHistoryResponse, 0, len(history))
	for _, h := range history {
		items = append(items, NewCreateHistoryResponse(h))
	}
	return ListTaskHistoryResponse{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}
}
