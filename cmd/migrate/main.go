package main

import (
	"log/slog"
	"os"

	"github.com/livingdolls/orkoda-tui/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	if cfg.DatabaseURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	slog.Info("migration runner is ready; migrations will be added in Epic 4")
}
