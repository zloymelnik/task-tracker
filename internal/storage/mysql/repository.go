package mysql

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type User interface {
	Save(ctx context.Context, user *domain.User) error
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}

type Team interface {
	Save(ctx context.Context, team *domain.Team) error
	ListByUserID(ctx context.Context, userID uuid.UUID) ([]domain.Team, error)
}

type TeamMember interface {
	Save(ctx context.Context, team *domain.TeamMember) error
}

type TxManager struct {
	db *sqlx.DB
}

type Repository struct {
	User         *UserRepository
	Team         *TeamRepository
	TeamMember   *TeamMemberRepository
	Transactions *TxManager
}

type Executor interface {
	ExecContext(
		ctx context.Context,
		query string,
		args ...any,
	) (sql.Result, error)
	SelectContext(
		ctx context.Context,
		dest any,
		query string,
		args ...any,
	) error
	GetContext(
		ctx context.Context,
		dest interface{},
		query string,
		args ...interface{},
	) error
}

func NewRepository(cfg config.DB, db *sqlx.DB, log *slog.Logger) *Repository {
	return &Repository{
		User:         NewUserRepository(db, log, cfg.MaxRequestTime),
		Team:         NewTeamRepository(db, log, cfg.MaxRequestTime),
		TeamMember:   NewTeamMemberRepository(db, log, cfg.MaxRequestTime),
		Transactions: NewTxManager(db),
	}
}
