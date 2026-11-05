package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/service"
)

type TeamService interface {
	Create(ctx context.Context, creatorID uuid.UUID, input service.CreateTeamInput) (*domain.Team, error)
	List(ctx context.Context, actorID uuid.UUID, filter domain.TeamFilter) ([]domain.Team, int, error)
	AddMember(ctx context.Context, actorID uuid.UUID, teamID uuid.UUID, input service.AddTeamMemberInput) (*domain.TeamMember, error)
	UpdateRole(ctx context.Context, actorID, teamID, userID uuid.UUID, role domain.TeamRole) error
}

type TeamHandler struct {
	s   TeamService
	log *slog.Logger
}

func NewTeamHandler(s TeamService, log *slog.Logger) *TeamHandler {
	return &TeamHandler{
		s:   s,
		log: log,
	}
}

func (h *TeamHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
			h.log.Error("Failed to write create team response", "error", err)
		}
		return
	}
	var rawTeam CreateTeamRequest
	if err := decodeJSON(w, r, &rawTeam); err != nil {
		respondError(w, err, h.log)
		return
	}
	teamInput := rawTeam.ToInput()
	h.log.Info("Create team", "name", teamInput.Name)
	team, err := h.s.Create(r.Context(), userID, teamInput)
	if err != nil {
		respondError(w, err, h.log)
		return
	}
	if err := writeJSON(w, http.StatusCreated, NewCreateTeamResponse(*team)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write create team response", "error", err)
		}
		return
	}
	h.log.Info("Team created successfully", "name", team.Name)
}

func (h *TeamHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
			h.log.Error("Failed to write team list response", "error", err)
		}
		return
	}
	query := r.URL.Query()
	filter := domain.TeamFilter{
		Limit:  20,
		Offset: 0,
	}
	if query.Has("limit") {
		limit, err := strconv.Atoi(query.Get("limit"))
		if err != nil {
			respondError(w, domain.ErrInvalidInput, h.log)
			return
		}
		filter.Limit = limit
	}
	if query.Has("offset") {
		offset, err := strconv.Atoi(query.Get("offset"))
		if err != nil {
			respondError(w, domain.ErrInvalidInput, h.log)
			return
		}
		filter.Offset = offset
	}
	h.log.Info("Gets team list", "filter", filter, "user_id", userID)
	teams, total, err := h.s.List(r.Context(), userID, filter)
	if err != nil {
		respondError(w, err, h.log)
		return
	}
	if err := writeJSON(w, http.StatusOK, NewCreateTeamResponseForList(teams, total, filter.Limit, filter.Offset)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write team list response", "error", err)
		}
		return
	}
	h.log.Info("Team list got successfully", "user_id", userID, "filter", filter, "total", total)
}

func (h *TeamHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
			h.log.Error("Failed to write invite team response", "error", err)
		}
		return
	}
	teamID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		if err := writeErrorResponse(w, http.StatusBadRequest, "Failed team_id"); err != nil {
			h.log.Error("Failed to write invite team response", "error", err)
		}
		return
	}

	var rawCandidate InviteUserRequest
	if err := decodeJSON(w, r, &rawCandidate); err != nil {
		respondError(w, err, h.log)
		return
	}

	candidate, err := h.s.AddMember(r.Context(), userID, teamID, rawCandidate.ToInput())
	if err != nil {
		respondError(w, err, h.log)
		return
	}

	if err := writeJSON(w, http.StatusCreated, NewInviteUserResponse(*candidate)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write invite team response", "error", err)
		}
		return
	}
	h.log.Info("User added to team successfully", "user_id", candidate.UserID, "team_id", candidate.TeamID, "role", candidate.Role)
}

func (h *TeamHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	actorID, ok := UserIDFromContext(r.Context())
	if !ok {
		if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
			h.log.Error("Failed to write change role response", "error", err)
		}
		return
	}
	teamID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		if err := writeErrorResponse(w, http.StatusBadRequest, "Failed team_id"); err != nil {
			h.log.Error("Failed to write change role response", "error", err)
		}
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		if err := writeErrorResponse(w, http.StatusBadRequest, "Failed user_id"); err != nil {
			h.log.Error("Failed to write change role team response", "error", err)
		}
		return
	}
	var input UpdateRoleRequest
	if err := decodeJSON(w, r, &input); err != nil {
		respondError(w, err, h.log)
		return
	}
	err = h.s.UpdateRole(
		r.Context(), actorID, teamID, userID,
		domain.TeamRole(input.Role),
	)
	if err != nil {
		respondError(w, err, h.log)
		return
	}
	h.log.Info("Change role team sucessfully", "actor_id", actorID, "team_id", teamID, "user_id", userID, "role", input.Role)
	w.WriteHeader(http.StatusNoContent)
}
