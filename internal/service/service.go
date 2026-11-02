package service

import (
	"log/slog"

	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

type Service struct {
	Auth *AuthService
	Team *TeamService
}

func NewService(cfg *config.Config, repository *mysql.Repository, log *slog.Logger) *Service {
	return &Service{
		Auth: NewAuthService(cfg.Auth, repository.User, log),
		Team: NewTeamService(repository.Transactions, repository.Team, repository.TeamMember, repository.User, log),
	}
}
