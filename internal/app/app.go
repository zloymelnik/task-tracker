package app

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/handler"
)

type App struct {
	server *http.Server
	log    *slog.Logger
	cansel context.CancelFunc
}

func NewApp(cfg *config.Config, log *slog.Logger) (*App, error) {
	//TODO: добавить инициализацию БД, редис -> сервисы -> хандлеры

	_, cansel := context.WithCancel(context.Background())
	h := handler.New()
	r := NewRouter(h, log)

	a := &App{
		server: &http.Server{
			Addr:         cfg.Server.Address(),
			Handler:      r,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
		},
		log:    log,
		cansel: cansel,
	}
	return a, nil
}

func (a *App) Run() error {
	return a.server.ListenAndServe()
}

func (a *App) Shutdown(ctx context.Context) error {
	//TODO: добавить закртие соеднений
	a.cansel()
	return a.server.Shutdown(ctx)
}
