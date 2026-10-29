package logger

import (
	"errors"
	"log/slog"
	"os"

	"github.com/zloymelnik/task-tracker/internal/config"
)

// GetLogger returns a logger tuned for the given environment:
//   - "prod"  — JSON format, INFO level
//   - "dev"   — JSON format, DEBUG level
//   - "local" — text format, DEBUG level (default)
func GetLogger(env string) (*slog.Logger, error) {
	var logg *slog.Logger
	switch env {
	case config.LocalEnv:
		logg = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	case config.DevEnv:
		logg = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	case config.ProdEnv:
		logg = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	default:
		return logg, errors.New("wrong log initialization")
	}
	return logg, nil
}
