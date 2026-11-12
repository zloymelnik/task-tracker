package handler

import (
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type CreateTeamRequest struct {
	Name string `json:"name" binding:"required" minLength:"1" maxLength:"255" example:"Моя команда"`
}

type CreateTeamResponse struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
	CreatedBy uuid.UUID `json:"created_by" format:"uuid"`
}

// ToInput converts the HTTP request into service input without changing its values.
func (r CreateTeamRequest) ToInput() service.CreateTeamInput {
	return service.CreateTeamInput{
		Name: r.Name,
	}
}

// NewCreateTeamResponse selects the team fields exposed by the API.
func NewCreateTeamResponse(team domain.Team) CreateTeamResponse {
	return CreateTeamResponse{
		ID:        team.ID,
		Name:      team.Name,
		CreatedAt: team.CreatedAt,
		CreatedBy: team.CreatedBy,
	}
}

type ListTeamsResponse struct {
	Items  []CreateTeamResponse `json:"items"`
	Total  int                  `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}

// NewCreateTeamResponse selects the team fields exposed by the API.
func NewCreateTeamResponseForList(teams []domain.Team, total, limit, offset int) ListTeamsResponse {
	ts := make([]CreateTeamResponse, 0, len(teams))
	for _, t := range teams {
		ts = append(ts, NewCreateTeamResponse(t))
	}
	return ListTeamsResponse{
		Items:  ts,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}
}

type InviteUserRequest struct {
	Email string `json:"email" binding:"required" format:"email" example:"member@example.com"`
	Role  string `json:"role" binding:"required" enums:"admin,member" example:"member"`
}

type InviteUserResponse struct {
	TeamID   uuid.UUID `json:"team_id" format:"uuid"`
	UserID   uuid.UUID `json:"user_id" format:"uuid"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at" format:"date-time"`
}

// ToInput converts the HTTP request into service input without changing its values.
func (r InviteUserRequest) ToInput() service.AddTeamMemberInput {
	return service.AddTeamMemberInput{
		Email: r.Email,
		Role:  domain.TeamRole(r.Role),
	}
}

// NewInviteUserResponse selects the team member fields exposed by the API.
func NewInviteUserResponse(teamMember domain.TeamMember) InviteUserResponse {
	return InviteUserResponse{
		TeamID:   teamMember.TeamID,
		UserID:   teamMember.UserID,
		Role:     string(teamMember.Role),
		JoinedAt: teamMember.JoinedAt,
	}
}

type UpdateRoleRequest struct {
	Role string `json:"role" binding:"required" enums:"admin,member" example:"admin"`
}
