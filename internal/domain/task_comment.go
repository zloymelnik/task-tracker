package domain

import (
	"time"

	"github.com/google/uuid"
)

type TaskComment struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	CreatedBy uuid.UUID
	Body      string
	CreatedAt time.Time
}

type TaskCommentFilter struct {
	TaskID *uuid.UUID
	Limit  int
	Offset int
}
