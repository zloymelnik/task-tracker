package service

import (
	"context"
	"log/slog"

	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

type Service struct {
	Auth  *AuthService
	Team  *TeamService
	Task  *TaskService
	Stats *StatsService
}

type TransactionManager interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}

func NewService(cfg *config.Config, repository *mysql.Repository, cache Cache, log *slog.Logger) *Service {
	return &Service{
		Auth: NewAuthService(cfg.Auth, repository.User, log),
		Team: NewTeamService(repository.Transactions, repository.Team, repository.TeamMember, repository.User, log),
		Task: NewTaskService(
			repository.Transactions,
			repository.Task,
			repository.TaskHistory,
			repository.TaskComment,
			repository.TeamMember,
			cache,
			NewTaskCacheOptions(cfg.Redis),
			log,
		),
		Stats: NewStatsService(repository.TeamStats, repository.TeamMember, log),
	}
}
