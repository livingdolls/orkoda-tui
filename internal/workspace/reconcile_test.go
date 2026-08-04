package workspace

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestReconcileOrphansQuarantinesUnknownDirectoriesAndMarksMissing(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	repository, err := NewRepository(db, root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects(id, name, created_at, updated_at) VALUES ('p', 'p', ?, ?)`, []any{now, now}},
		{`INSERT INTO repositories(id, project_id, local_path, current_branch, head_sha, created_at, updated_at) VALUES ('r', 'p', ?, 'main', 'abc', ?, ?)`, []any{filepath.Join(root, "source"), now, now}},
		{`INSERT INTO plans(id, project_id, title, status, current_version, created_at, updated_at) VALUES ('plan', 'p', 'plan', 'READY', 1, ?, ?)`, []any{now, now}},
		{`INSERT INTO plan_versions(id, plan_id, version, requirement, created_at) VALUES ('version', 'plan', 1, 'plan', ?)`, []any{now}},
		{`INSERT INTO workflow_jobs(id, project_id, plan_id, plan_version_id, repository_id, base_branch, base_commit_sha, status, created_at, updated_at) VALUES ('job', 'p', 'plan', 'version', 'r', 'main', 'abc', 'READY', ?, ?)`, []any{now, now}},
	} {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	missingPath := filepath.Join(root, "job")
	if _, err := db.ExecContext(ctx, `INSERT INTO workspaces(id, workflow_job_id, project_id, repository_id, path, base_commit_sha, status, created_at, updated_at) VALUES ('w', 'job', 'p', 'r', ?, 'abc', 'READY', ?, ?)`, missingPath, now, now); err != nil {
		t.Fatal(err)
	}
	orphanPath := filepath.Join(root, "orphan-directory")
	if err := os.MkdirAll(orphanPath, 0o700); err != nil {
		t.Fatal(err)
	}
	report, err := repository.ReconcileOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Orphaned) != 1 || len(report.Missing) != 1 {
		t.Fatalf("report = %#v", report)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("orphan path still exists: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".orphans"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("quarantine entries = %v, err = %v", entries, err)
	}
	var status, message string
	if err := db.QueryRowContext(ctx, `SELECT status, failure_message FROM workspaces WHERE id = 'w'`).Scan(&status, &message); err != nil {
		t.Fatal(err)
	}
	if status != string(StatusFailed) || message == "" {
		t.Fatalf("missing workspace status/message = %q/%q", status, message)
	}
}

func TestArchiveAndCleanupPreserveRecoverableWorkspaceData(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	repository, err := NewRepository(db, root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return now }
	seedWorkspaceWorkflow(t, db)
	workspacePath := filepath.Join(root, "workflow-1")
	if err := os.MkdirAll(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.EnsureForWorkflow(ctx, "workflow-1"); err != nil {
		t.Fatal(err)
	}
	archived, err := repository.Archive(ctx, "")
	if err == nil || archived.ID != "" {
		t.Fatalf("Archive(empty) = %#v, %v", archived, err)
	}
	item, err := repository.Archive(ctx, mustWorkspaceID(t, db, "workflow-1"))
	if err != nil || item.Status != StatusArchived {
		t.Fatalf("Archive() = %#v, %v", item, err)
	}
	old := now.Add(-48 * time.Hour).UnixMilli()
	if _, err := db.ExecContext(ctx, `UPDATE workspaces SET updated_at = ? WHERE id = ?`, old, item.ID); err != nil {
		t.Fatal(err)
	}
	orphanPath := filepath.Join(root, ".orphans", "old-orphan")
	if err := os.MkdirAll(orphanPath, 0o700); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(orphanPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	report, err := repository.Cleanup(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Archived) != 1 || len(report.RemovedOrphans) != 1 {
		t.Fatalf("cleanup report = %#v", report)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("workspace was not moved to archive: %v", err)
	}
	archiveEntries, err := os.ReadDir(filepath.Join(root, ".archive"))
	if err != nil || len(archiveEntries) != 1 {
		t.Fatalf("archive entries = %v, err = %v", archiveEntries, err)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("old orphan was not removed: %v", err)
	}
}

func mustWorkspaceID(t *testing.T, db *sql.DB, workflowID string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(context.Background(), `SELECT id FROM workspaces WHERE workflow_job_id = ?`, workflowID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
