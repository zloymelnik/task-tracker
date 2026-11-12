package domain

import "github.com/google/uuid"

type TaskStatusCounts struct {
	Todo       int
	InProgress int
	Done       int
}

type AssigneeStats struct {
	UserID      uuid.UUID
	Name        string
	ClosedTasks int
}

type TeamStats struct {
	TeamID              uuid.UUID
	TasksByStatus       TaskStatusCounts
	TopAssignees        []AssigneeStats
	AverageCloseSeconds *float64
	CommentsCount       int
}
