package handler

import (
	"log/slog"
	"net/http"

	"github.com/zloymelnik/task-tracker/internal/service"
)

type HealthEndpoint interface {
	Health(w http.ResponseWriter, r *http.Request)
}

type AuthorizationHandler interface {
	Register(w http.ResponseWriter, r *http.Request)
	Login(w http.ResponseWriter, r *http.Request)
}

type Handler struct {
	Health HealthEndpoint
	Auth   AuthorizationHandler
}

func New(service *service.Service, log *slog.Logger) *Handler {
	return &Handler{
		Health: NewHealthHandler(log),
		Auth:   NewAuthHandler(service.Auth, log),
	}
}
