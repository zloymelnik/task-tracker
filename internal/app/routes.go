package app

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/zloymelnik/task-tracker/internal/handler"
)

func NewRouter(h *handler.Handler, parser handler.TokenParser, log *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(handler.Logger(log))
		r.Use(handler.Recovery(log))
		r.Get("/health", h.Health.Health)
		r.Post("/register", h.Auth.Register)
		r.Post("/login", h.Auth.Login)
		r.Group(func(r chi.Router) {
			r.Use(handler.AuthMiddleware(parser, log))
			r.Post("/teams", h.Team.Create)
			r.Get("/teams", h.Team.List)
			r.Post("/teams/{id}/invite", h.Team.AddMember)
			r.Patch("/teams/{id}/members/{user_id}/role", h.Team.UpdateRole)

			r.Post("/tasks", h.Task.Create)
			r.Get("/tasks", h.Task.List)
			r.Put("/tasks/{id}", h.Task.Update)
			r.Get("/tasks/{id}/history", h.Task.History)
			r.Post("/tasks/{id}/comments", h.Task.AddComment)
			r.Get("/tasks/{id}/comments", h.Task.Comments)

			r.Get("/teams/{team_id}/stats", h.Stats.Get)
		})
	})

	return r
}
