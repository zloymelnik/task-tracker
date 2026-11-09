package redis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/avast/retry-go"
	goredis "github.com/redis/go-redis/v9"
	"github.com/zloymelnik/task-tracker/internal/config"
)

func NewConnection(ctx context.Context, cfg *config.Redis, log *slog.Logger) (*goredis.Client, error) {
	log.Debug("Redis init: config settings are", "addr", cfg.Addr, "db", cfg.DB)
	client := goredis.NewClient(&goredis.Options{
		Addr:                  cfg.Addr,
		Password:              cfg.Password,
		DB:                    cfg.DB,
		PoolSize:              cfg.PoolSize,
		DialTimeout:           cfg.MaxRequestTime,
		ReadTimeout:           cfg.MaxRequestTime,
		WriteTimeout:          cfg.MaxRequestTime,
		PoolTimeout:           cfg.MaxRequestTime,
		ContextTimeoutEnabled: true,
		MaxRetries:            -1,
	})

	err := retry.Do(
		func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			pingCtx, cancel := context.WithTimeout(ctx, cfg.MaxRequestTime)
			defer cancel()
			return client.Ping(pingCtx).Err()
		},
		retry.Attempts(uint(cfg.Retry.MaxAttempts)),
		retry.Delay(cfg.Retry.Delay),
		retry.DelayType(retry.FixedDelay),
		retry.Context(ctx),
		retry.LastErrorOnly(true),
		retry.RetryIf(func(err error) bool {
			return ctx.Err() == nil
		}),
		retry.OnRetry(func(n uint, err error) {
			log.Warn("Redis ping failed",
				"attempt", n+1,
				"max_attempts", cfg.Retry.MaxAttempts,
				"error", err,
			)
		}),
	)
	if err != nil {
		e := client.Close()

		if ctx.Err() != nil {
			err = ctx.Err()
		}

		return nil, fmt.Errorf("redis connection failed: %w", errors.Join(err, e))
	}

	log.Info("Redis connection established")
	return client, nil
}
