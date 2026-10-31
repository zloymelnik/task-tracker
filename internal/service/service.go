package service

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/domain"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

type Authorization interface {
	Register(ctx context.Context, input RegisterInput) (*domain.User, error)
	Login(ctx context.Context, input LoginInput) (string, error)
	ParseToken(tokenString string) (uuid.UUID, error)
}

type Service struct {
	Auth Authorization
}

func NewService(cfg *config.Config, repository *mysql.Repository, log *slog.Logger) *Service {
	return &Service{
		Auth: NewAuth(cfg.Auth, repository.User, log),
	}
}
