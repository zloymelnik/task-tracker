package mysql

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/config"
)

type TxManager struct {
	db *sqlx.DB
}

type Repository struct {
	User         *UserRepository
	Team         *TeamRepository
	TeamMember   *TeamMemberRepository
	Transactions *TxManager
	Task         *TaskRepository
	TaskHistory  *TaskHistoryRepository
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
		Task:         NewTaskRepository(db, log, cfg.MaxRequestTime),
		TaskHistory:  NewTaskHistoryRepository(db, log, cfg.MaxRequestTime),
	}
}
