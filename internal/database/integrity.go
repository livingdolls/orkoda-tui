package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// CheckIntegrity runs SQLite's structural and foreign-key diagnostics. It is
// intentionally explicit so startup and the migration command fail closed on
// a damaged local database instead of letting later workflow writes obscure
// the original corruption.
func CheckIntegrity(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("run SQLite integrity check: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(result), "ok") {
		return fmt.Errorf("SQLite integrity check failed: %s", strings.TrimSpace(result))
	}
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("run SQLite foreign-key check: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, parent string
		var rowID, foreignKeyID int64
		if err := rows.Scan(&table, &rowID, &parent, &foreignKeyID); err != nil {
			return fmt.Errorf("scan SQLite foreign-key violation: %w", err)
		}
		return fmt.Errorf(
			"SQLite foreign-key violation: table=%s rowid=%d parent=%s fk=%d",
			table, rowID, parent, foreignKeyID,
		)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read SQLite foreign-key check: %w", err)
	}
	return nil
}
