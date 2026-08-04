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

// Restore replaces databasePath with a validated backup copy. The caller must
// stop all database users before invoking Restore. The destination is updated
// through a temporary file and atomic rename so a failed copy cannot leave a
// half-written SQLite database.
func Restore(ctx context.Context, databasePath, backupPath string) error {
	databasePath = filepath.Clean(strings.TrimSpace(databasePath))
	backupPath = filepath.Clean(strings.TrimSpace(backupPath))
	if databasePath == "" || databasePath == "." || backupPath == "" || backupPath == "." {
		return fmt.Errorf("database and backup paths are required")
	}
	if databasePath == backupPath {
		return fmt.Errorf("database and backup paths must differ")
	}
	if err := validateRegularDatabaseFile(backupPath); err != nil {
		return fmt.Errorf("validate database backup: %w", err)
	}
	if info, err := os.Lstat(databasePath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("database path must be a regular non-symlink file")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect database restore destination: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return fmt.Errorf("create database restore directory: %w", err)
	}
	source, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("open database backup: %w", err)
	}
	defer source.Close()
	temporary, err := os.CreateTemp(filepath.Dir(databasePath), ".orkoda-db-restore-*")
	if err != nil {
		return fmt.Errorf("create temporary restore database: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("restrict restored database permissions: %w", err)
	}
	if _, err := io.Copy(temporary, source); err != nil {
		temporary.Close()
		return fmt.Errorf("copy database restore: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync restored database: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close restored database: %w", err)
	}
	if err := os.Rename(temporaryPath, databasePath); err != nil {
		return fmt.Errorf("publish restored database: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		path := databasePath + suffix
		if info, statErr := os.Lstat(path); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to remove symlinked SQLite sidecar %s", path)
			}
			if removeErr := os.Remove(path); removeErr != nil {
				return fmt.Errorf("remove SQLite sidecar %s: %w", path, removeErr)
			}
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("inspect SQLite sidecar %s: %w", path, statErr)
		}
	}
	return nil
}

func validateRegularDatabaseFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("path must be a regular non-symlink file")
	}
	return nil
}
