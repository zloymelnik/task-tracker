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
	a, err := app.NewApp(config, logger)
	if err != nil {
		logger.Error("Error init app", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("Start service: init app complete")

	//Runs app
	serverErr := make(chan error, 1)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("Server starting", "address", config.Server.Address())
		if err := a.Run(); !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	exitCode := 0
	select {
	case sig := <-quit:
		logger.Info("Signal received, shutting down", "signal", sig)
	case err := <-serverErr:
		logger.Error("Server error", "err", err)
		exitCode = 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.Server.ShutdownTimeout)
	if err := a.Shutdown(ctx); err != nil {
		cancel()
		logger.Error("Graceful shutdown failed", "err", err)
		exitCode = 1
	}
	cancel()

	logger.Info("Server stopped")
	signal.Stop(quit)
	os.Exit(exitCode)
}
