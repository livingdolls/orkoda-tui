package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/livingdolls/orkoda-tui/internal/config"
	"github.com/livingdolls/orkoda-tui/internal/database"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx := context.Background()
	if err := database.Backup(ctx, cfg.DatabasePath); err != nil {
		return err
	}
	db, err := database.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := database.Migrate(ctx, db); err != nil {
		return err
	}
	if err := database.CheckIntegrity(ctx, db); err != nil {
		return err
	}
	slog.Info("sqlite migrations completed", "path", cfg.DatabasePath)
	return nil
}
