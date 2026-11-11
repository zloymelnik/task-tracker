package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type StatsService interface {
	Get(ctx context.Context, actorID, teamID uuid.UUID) (*domain.TeamStats, error)
}

type StatsHandler struct {
	s   StatsService
	log *slog.Logger
}

func NewStatsHandler(s StatsService, log *slog.Logger) *StatsHandler {
	return &StatsHandler{
		s:   s,
		log: log,
	}
}

func (h *StatsHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
			h.log.Error("Failed to write team stats response", "error", err)
		}
		return
	}
	teamID, err := uuid.Parse(chi.URLParam(r, "team_id"))
	if err != nil {
		respondError(w, domain.ErrInvalidInput, h.log)
		return
	}
	h.log.Info("Gets team stats", "team_id", teamID)
	stats, err := h.s.Get(r.Context(), userID, teamID)
	if err != nil {
		respondError(w, err, h.log)
		return
	}
	if err := writeJSON(w, http.StatusOK, NewTeamStatsResponse(*stats)); err != nil {
		if errors.Is(err, errEncodeJSON) {
			respondError(w, err, h.log)
		} else {
			h.log.Error("Failed to write team stats response", "error", err)
		}
		return
	}
	h.log.Info("Gets team stats", "team_id", teamID, "stats", stats)
}
