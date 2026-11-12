package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jmoiron/sqlx"
	goredis "github.com/redis/go-redis/v9"
	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
	"github.com/zloymelnik/task-tracker/internal/storage/mysql"
	redisstorage "github.com/zloymelnik/task-tracker/internal/storage/redis"
)

type App struct {
	server *http.Server
	log    *slog.Logger
	db     *sqlx.DB
	redis  *goredis.Client
}

func NewApp(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	db, err := mysql.NewConnection(ctx, &cfg.DB, log)
	if err != nil {
		return nil, fmt.Errorf("init mysql: %w", err)
	}
	redisClient, err := redisstorage.NewConnection(ctx, &cfg.Redis, log)
	if err != nil {
		dbError := db.Close()
		return nil, fmt.Errorf("init redis: %w", errors.Join(err, dbError))
	}
	repo := mysql.NewRepository(cfg.DB, db, log)
	cache := redisstorage.NewCache(redisClient, cfg.Redis.MaxRequestTime, log)
	services := service.NewService(cfg, repo, cache, log)
	handlers := handler.New(services, log)
	router := NewRouter(handlers, cfg.Docs.Enabled, services.Auth, log)

	a := &App{
		server: &http.Server{
			Addr:         cfg.Server.Address(),
			Handler:      router,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
		},
		log:   log,
		db:    db,
		redis: redisClient,
	}
	return a, nil
}

func (a *App) Run() error {
	return a.server.ListenAndServe()
}

func (a *App) Shutdown(ctx context.Context) error {
	serverErr := a.server.Shutdown(ctx)
	dbErr := a.db.Close()
	redisError := a.redis.Close()
	return errors.Join(serverErr, dbErr, redisError)
}
