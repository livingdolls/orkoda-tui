package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/livingdolls/orkoda-tui/internal/config"
	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/httpapi"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/scheduler"
)

type componentResult struct {
	name string
	err  error
}

func main() {
	if err := run(); err != nil {
		slog.Error("daemon stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	runtimeCtx, stopRuntime := context.WithCancel(signalCtx)
	defer stopRuntime()

	db, err := database.Open(runtimeCtx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(runtimeCtx, db); err != nil {
		return err
	}
	logger.Info("sqlite ready", "path", cfg.DatabasePath)

	queue := jobqueue.New(db)
	queueScheduler, err := scheduler.New(
		queue,
		scheduler.DefaultConfig(fmt.Sprintf("local-daemon-%d", os.Getpid())),
		map[string]scheduler.Handler{
			"system.noop": func(_ context.Context, job jobqueue.Job) error {
				logger.Info("noop job handled", "job_id", job.ID)
				return nil
			},
		},
		logger,
	)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.APIAddress(),
		Handler:           httpapi.NewRouter(cfg.Environment),
		ReadHeaderTimeout: 5 * cfg.ShutdownTimeout / 10,
	}

	results := make(chan componentResult, 2)
	go func() {
		logger.Info("daemon listening", "address", cfg.APIAddress(), "environment", cfg.Environment)
		results <- componentResult{name: "http", err: server.ListenAndServe()}
	}()
	go func() {
		logger.Info("queue scheduler started")
		results <- componentResult{name: "scheduler", err: queueScheduler.Run(runtimeCtx)}
	}()

	completed := 0
	var runErr error
	select {
	case <-signalCtx.Done():
		logger.Info("shutdown requested")
	case result := <-results:
		completed++
		runErr = componentError(result)
	}

	stopRuntime()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = fmt.Errorf("shutdown HTTP server: %w", err)
	}

	for completed < 2 {
		select {
		case result := <-results:
			completed++
			if err := componentError(result); err != nil && runErr == nil {
				runErr = err
			}
		case <-shutdownCtx.Done():
			if runErr == nil {
				runErr = fmt.Errorf("coordinated shutdown: %w", shutdownCtx.Err())
			}
			return runErr
		}
	}

	logger.Info("daemon shutdown complete")
	return runErr
}

func componentError(result componentResult) error {
	if result.err == nil {
		return nil
	}
	if result.name == "http" && errors.Is(result.err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("%s component stopped: %w", result.name, result.err)
}
