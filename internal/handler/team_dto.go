package handler

import (
	"time"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type CreateTeamRequest struct {
	Name string `json:"name"`
}

type CreateTeamResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy uuid.UUID `json:"created_by"`
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

// NewCreateTeamResponse selects the team fields exposed by the API.
func NewCreateTeamResponseForList(teams []domain.Team) []CreateTeamResponse {
	ts := make([]CreateTeamResponse, 0, len(teams))
	for _, t := range teams {
		ts = append(ts, NewCreateTeamResponse(t))
	}
	return ts
}

type InviteUserRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type InviteUserResponse struct {
	TeamID   uuid.UUID `json:"team_id"`
	UserID   uuid.UUID `json:"user_id"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
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
	Role string `json:"role"`
}
