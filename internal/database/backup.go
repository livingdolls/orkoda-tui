package database

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Backup creates a recoverable copy before startup migrations. It is a no-op
// for a new database and refuses symlinked database paths.
func Backup(ctx context.Context, databasePath string) error {
	databasePath = filepath.Clean(strings.TrimSpace(databasePath))
	if databasePath == "" || databasePath == "." {
		return fmt.Errorf("database path is required")
	}
	info, err := os.Lstat(databasePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect database before backup: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("database path must be a regular non-symlink file")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	checkpointDB, err := Open(ctx, databasePath)
	if err != nil {
		return fmt.Errorf("open database for WAL checkpoint: %w", err)
	}
	if _, err := checkpointDB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		checkpointDB.Close()
		return fmt.Errorf("checkpoint database before backup: %w", err)
	}
	if err := checkpointDB.Close(); err != nil {
		return fmt.Errorf("close database after WAL checkpoint: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return fmt.Errorf("create database backup directory: %w", err)
	}
	source, err := os.Open(databasePath)
	if err != nil {
		return fmt.Errorf("open database for backup: %w", err)
	}
	defer source.Close()
	temporary, err := os.CreateTemp(filepath.Dir(databasePath), ".orkoda-db-backup-*")
	if err != nil {
		return fmt.Errorf("create temporary database backup: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, source); err != nil {
		temporary.Close()
		return fmt.Errorf("copy database backup: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync database backup: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close database backup: %w", err)
	}
	backupPath := databasePath + ".bak"
	if backupInfo, statErr := os.Lstat(backupPath); statErr == nil && backupInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to overwrite symlinked database backup")
	}
	if err := os.Rename(temporaryPath, backupPath); err != nil {
		return fmt.Errorf("publish database backup: %w", err)
	}
	return nil
}
