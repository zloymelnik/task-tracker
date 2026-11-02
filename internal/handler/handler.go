package handler

import (
	"log/slog"

	"github.com/zloymelnik/task-tracker/internal/service"
)

type Handler struct {
	Health *HealthHandler
	Auth   *AuthHandler
	Team   *TeamHandler
}

func New(service *service.Service, log *slog.Logger) *Handler {
	return &Handler{
		Health: NewHealthHandler(log),
		Auth:   NewAuthHandler(service.Auth, log),
		Team:   NewTeamHandler(service.Team, log),
	}
}
