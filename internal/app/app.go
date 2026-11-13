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
	"github.com/zloymelnik/task-tracker/internal/ratelimit"
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

	apiLimiter, err := ratelimit.NewLimiter(
		cfg.RateLimit.Interval,
		cfg.RateLimit.Burst,
		cfg.RateLimit.MaxEntries,
	)
	if err != nil {
		dbErr := db.Close()
		redisError := redisClient.Close()
		return nil, errors.Join(fmt.Errorf("init rate limiter: %w", err), dbErr, redisError)
	}
	loginLimiter, err := ratelimit.NewLimiter(
		cfg.Auth.LoginLimit.Interval,
		cfg.Auth.LoginLimit.Burst,
		cfg.Auth.LoginLimit.MaxEntries,
	)
	if err != nil {
		dbErr := db.Close()
		redisError := redisClient.Close()
		return nil, errors.Join(fmt.Errorf("init login rate limiter: %w", err), dbErr, redisError)
	}
	registerLimiter, err := ratelimit.NewLimiter(
		cfg.Auth.RegisterLimit.Interval,
		cfg.Auth.RegisterLimit.Burst,
		cfg.Auth.RegisterLimit.MaxEntries,
	)
	if err != nil {
		dbErr := db.Close()
		redisError := redisClient.Close()
		return nil, errors.Join(fmt.Errorf("init registr rate limiter: %w", err), dbErr, redisError)
	}
	router := NewRouter(
		handlers,
		cfg.Docs.Enabled,
		services.Auth,
		apiLimiter,
		loginLimiter,
		registerLimiter,
		log,
	)

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
