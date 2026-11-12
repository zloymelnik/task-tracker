package domain

import (
	"time"

	"github.com/google/uuid"
)

type TaskStatus string

const (
	Todo       TaskStatus = "todo"
	InProgress TaskStatus = "in_progress"
	Done       TaskStatus = "done"
)

type Task struct {
	ID          uuid.UUID
	TeamID      uuid.UUID
	CreatedBy   uuid.UUID
	AssigneeID  *uuid.UUID
	Title       string
	Description string
	Status      TaskStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ClosedAt    *time.Time
	Version     uint32
}

type TaskFilter struct {
	TeamID     *uuid.UUID
	Status     *TaskStatus
	AssigneeID *uuid.UUID
	Offset     int
	Limit      int
}
