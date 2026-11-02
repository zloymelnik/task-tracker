package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/zloymelnik/task-tracker/internal/app"
	"github.com/zloymelnik/task-tracker/internal/config"
	"github.com/zloymelnik/task-tracker/internal/logger"
)

func main() {
	//Inits config
	configPath := flag.String("config-path", "configs/config.yaml", "Please set in the config-path flag the path to config file")
	flag.Parse()
	config, err := config.LoadConfig(*configPath)
	if err != nil {
		panic(err.Error())
	}

	//Inits logger
	logger, err := logger.GetLogger(config.Env)
	if err != nil {
		log.Fatalf("Error init logger: %s", err.Error())
	}
	logger.Info("Start service: init logger complete")

	//Inints app
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	a, err := app.NewApp(ctx, config, logger)
	if err != nil {
		logger.Error("Error init app", "error", err)
		os.Exit(1)
	}
	logger.Info("Start service: init app complete")

	//Runs app
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("Server starting", "address", config.Server.Address())
		if err := a.Run(); !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	exitCode := 0
	select {
	case <-ctx.Done():
		logger.Info("Shutdown requested", "reason", ctx.Err())
	case err := <-serverErr:
		logger.Error("Server error", "err", err)
		exitCode = 1
	}
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.Server.ShutdownTimeout)
	if err := a.Shutdown(shutdownCtx); err != nil {
		cancel()
		logger.Error("Graceful shutdown failed", "err", err)
		exitCode = 1
	}
	cancel()

	logger.Info("Server stopped")
	os.Exit(exitCode)
}
