from pathlib import Path

Path("internal/workspace/startup_recovery_test.go").write_text(r'''package workspace

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestRecoverDaemonLeasesPreservesManualLease(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "workspace-recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		CREATE TABLE workspaces (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			lease_owner TEXT,
			lease_token TEXT,
			lease_expires_at INTEGER,
			updated_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err = db.ExecContext(ctx, `
		INSERT INTO workspaces (
			id, status, lease_owner, lease_token, lease_expires_at, updated_at
		) VALUES
			('daemon', 'WRITE_LOCKED', 'local-daemon-123', 'daemon-token', ?, ?),
			('manual', 'WRITE_LOCKED', 'tui-client', 'manual-token', ?, ?)
	`, now.Add(time.Hour).UnixMilli(), now.UnixMilli(),
		now.Add(time.Hour).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewRepository(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := repository.RecoverDaemonLeases(ctx, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0] != "daemon" {
		t.Fatalf("recovered = %v", recovered)
	}
	var daemonStatus, daemonOwner, manualStatus, manualOwner string
	if err := db.QueryRow(`SELECT status, COALESCE(lease_owner, '') FROM workspaces WHERE id = 'daemon'`).Scan(&daemonStatus, &daemonOwner); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status, COALESCE(lease_owner, '') FROM workspaces WHERE id = 'manual'`).Scan(&manualStatus, &manualOwner); err != nil {
		t.Fatal(err)
	}
	if daemonStatus != "READY" || daemonOwner != "" || manualStatus != "WRITE_LOCKED" || manualOwner != "tui-client" {
		t.Fatalf("daemon=%s/%q manual=%s/%q", daemonStatus, daemonOwner, manualStatus, manualOwner)
	}
}
''')

print("startup recovery test schema isolated")
