package logger

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/zloymelnik/task-tracker/internal/config"
)

// GetLogger returns a logger tuned for the given environment:
//   - "prod"  — JSON format, INFO level
//   - "dev"   — JSON format, DEBUG level
//   - "local" — text format, DEBUG level, stdout and tmp/app.log
func GetLogger(env string) (*slog.Logger, error) {
	var logg *slog.Logger
	switch env {
	case config.LocalEnv:
		if err := os.MkdirAll("tmp", 0700); err != nil {
			return nil, fmt.Errorf("create local log directory: %w", err)
		}
		// The application keeps this append-only log open for its process lifetime.
		file, err := os.OpenFile("tmp/app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, fmt.Errorf("open local application log: %w", err)
		}
		logg = slog.New(slog.NewTextHandler(io.MultiWriter(os.Stdout, file), &slog.HandlerOptions{Level: slog.LevelDebug}))
	case config.DevEnv:
		logg = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	case config.ProdEnv:
		logg = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	default:
		return logg, errors.New("wrong log initialization")
	}
	return logg, nil
}
