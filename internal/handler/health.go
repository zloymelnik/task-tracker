package handler

import (
	"log/slog"
	"net/http"
)

func (h *Handler) Health(w http.ResponseWriter, r *http.Request, log *slog.Logger) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("OK")); err != nil {
		log.Error("Failed to write health response", "error", err)
	}
}
