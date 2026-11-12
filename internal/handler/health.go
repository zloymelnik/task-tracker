package handler

import (
	"log/slog"
	"net/http"
)

type HealthHandler struct {
	log *slog.Logger
}

func NewHealthHandler(log *slog.Logger) *HealthHandler {
	return &HealthHandler{
		log: log,
	}
}

// Health returns service health status.
// @Summary Статус сервиса
// @Description Возвращает OK, если HTTP-сервер обрабатывает запросы. Подключения к MySQL и Redis не проверяет.
// @Tags health
// @Produce text/plain
// @Success 200 {string} string "OK"
// @Router /health [get]
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("OK")); err != nil {
		h.log.Error("Failed to write health response", "error", err)
	}
}
