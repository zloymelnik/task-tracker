package app

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/zloymelnik/task-tracker/internal/handler"
)

func NewRouter(h *handler.Handler, parser handler.TokenParser, log *slog.Logger) http.Handler {
	//TODO: добавить нормальные middleware
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(handler.Logger(log))
	r.Use(handler.Recovery(log))
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		h.Health.Health(w, r)
	})
	r.Route("/auth", func(r chi.Router) {
		r.Post("/register", h.Auth.Register)
		r.Post("/login", h.Auth.Login)
	})
	r.Group(func(r chi.Router) {
		r.Use(handler.AuthMiddleware(parser, log))
		// Register protected team and task routes here.
	})
	return r
}
