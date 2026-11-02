package mysql

import (
	"context"
	"log/slog"

	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type User interface {
	Save(ctx context.Context, user *domain.User) error
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}

type Repository struct {
	User
}

func NewRepository(cfg config.DB, db *sqlx.DB, log *slog.Logger) *Repository {
	return &Repository{
		User: NewUserRepository(db, log, cfg.MaxRequestTime),
	}
}
