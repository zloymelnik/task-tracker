package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
)

type App struct {
	server *http.Server
	log    *slog.Logger
	db     *sqlx.DB
}

func NewApp(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	db, err := mysql.NewConnection(ctx, &cfg.DB, log)
	if err != nil {
		return nil, fmt.Errorf("init mysql: %w", err)
	}
	repo := mysql.NewRepository(cfg.DB, db, log)
	services := service.NewService(cfg, repo, log)
	handlers := handler.New(services, log)
	router := NewRouter(handlers, services.Auth, log)

	a := &App{
		server: &http.Server{
			Addr:         cfg.Server.Address(),
			Handler:      router,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
		},
		log: log,
		db:  db,
	}
	return a, nil
}

func (a *App) Run() error {
	return a.server.ListenAndServe()
}

func (a *App) Shutdown(ctx context.Context) error {
	serverErr := a.server.Shutdown(ctx)
	dbErr := a.db.Close()
	return errors.Join(serverErr, dbErr)
}
