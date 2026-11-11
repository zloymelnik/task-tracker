package handler

import (
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type TaskStatusCountsResponse struct {
	Todo       int `json:"todo"`
	InProgress int `json:"in_progress"`
	Done       int `json:"done"`
}

type AssigneeStatsResponse struct {
	UserID      uuid.UUID `json:"user_id"`
	Name        string    `json:"name"`
	ClosedTasks int       `json:"closed_tasks"`
}

type TeamStatsResponse struct {
	TeamID              uuid.UUID                `json:"team_id"`
	TasksByStatus       TaskStatusCountsResponse `json:"tasks_by_status"`
	TopAssignees        []AssigneeStatsResponse  `json:"top_assignees"`
	AverageCloseSeconds *float64                 `json:"average_close_seconds"`
	CommentsCount       int                      `json:"comments_count"`
}

func NewTeamStatsResponse(stats domain.TeamStats) TeamStatsResponse {
	tasksByStatus := TaskStatusCountsResponse{
		Todo:       stats.TasksByStatus.Todo,
		InProgress: stats.TasksByStatus.InProgress,
		Done:       stats.TasksByStatus.Done,
	}
	topAssignees := make([]AssigneeStatsResponse, 0, len(stats.TopAssignees))
	for _, r := range stats.TopAssignees {
		topAssignees = append(topAssignees, AssigneeStatsResponse{
			UserID:      r.UserID,
			Name:        r.Name,
			ClosedTasks: r.ClosedTasks,
		})
	}
	return TeamStatsResponse{
		TeamID:              stats.TeamID,
		TasksByStatus:       tasksByStatus,
		TopAssignees:        topAssignees,
		AverageCloseSeconds: stats.AverageCloseSeconds,
		CommentsCount:       stats.CommentsCount,
	}
}
