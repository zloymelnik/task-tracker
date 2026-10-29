package app

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zloymelnik/task-tracker/internal/handler"
)

func NewRouter(h *handler.Handler, log *slog.Logger) http.Handler {
	//TODO: добавить нормальные middleware
	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		h.Health(w, r, log)
	})
	return r
}
