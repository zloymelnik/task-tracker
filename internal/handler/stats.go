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

// Get returns team statistics.
// @Summary Статистика команды
// @Description Возвращает отчёт только по указанной команде. Доступ имеют её owner и admin. Показывает кол-во задач по всем статусам, среднее время закрытия задачи, топ 3 пользователя по закрытым задачам в течении последних 30 дней и общее кол-во комментариев к задачам команды.
// @Tags statistics
// @Produce json
// @Security BearerAuth
// @Param team_id path string true "UUID команды" Format(uuid)
// @Success 200 {object} TeamStatsResponse
// @Failure 400 {object} errorResponse
// @Failure 401 {object} errorResponse
// @Failure 403 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Failure 429 {object} errorResponse "Превышен лимит запросов"
// @Header 429 {integer} Retry-After "Время ожидания перед повторной попыткой в секундах, минимум 1"
// @Router /teams/{team_id}/stats [get]
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
