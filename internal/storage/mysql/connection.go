package mysql

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/avast/retry-go"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/config"
)

func NewConnection(ctx context.Context, cfg *config.DB, log *slog.Logger) (*sqlx.DB, error) {
	c := mysqldriver.Config{
		User:                 cfg.User,
		Passwd:               cfg.Password,
		Net:                  "tcp",
		Addr:                 net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		DBName:               cfg.Name,
		AllowNativePasswords: true,
		ParseTime:            true,
		Loc:                  time.UTC,
	}

	log.Debug("MySQL init: config settings are", "addr", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), "user", cfg.User, "name", cfg.Name)
	db, err := sqlx.Open("mysql", c.FormatDSN())
	if err != nil {
		return db, fmt.Errorf("MySQL init error: %w", err)
	}

	db.SetMaxOpenConns(cfg.Pool.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Pool.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.Pool.ConnMaxLifetime)

	err = retry.Do(
		func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			pingCtx, cancel := context.WithTimeout(ctx, cfg.MaxRequestTime)
			defer cancel()

			return db.PingContext(pingCtx)
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
			log.Warn("MySQL ping failed",
				"attempt", n+1,
				"max_attempts", cfg.Retry.MaxAttempts,
				"error", err,
			)
		}),
	)
	if err != nil {
		_ = db.Close()

		if ctx.Err() != nil {
			err = ctx.Err()
		}

		return nil, fmt.Errorf("MySQL connection failed: %w", err)
	}

	log.Info("MySQL connection established")
	return db, nil
}
