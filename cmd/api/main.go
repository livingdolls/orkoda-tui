package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/livingdolls/orkoda-tui/internal/config"
	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/httpapi"
)

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

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(shutdownCtx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(shutdownCtx, db); err != nil {
		return err
	}
	slog.Info("sqlite ready", "path", cfg.DatabasePath)

	server := &http.Server{
		Addr:              cfg.APIAddress(),
		Handler:           httpapi.NewRouter(cfg.Environment),
		ReadHeaderTimeout: 5 * cfg.ShutdownTimeout / 10,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("daemon listening", "address", cfg.APIAddress(), "environment", cfg.Environment)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdownCtx.Done():
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return server.Shutdown(ctx)
}
