package workspace

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestRepositoryEnsuresWorkspaceAndProtectsLeaseOwnership(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	seedWorkspaceWorkflow(t, db)

	repository, err := NewRepository(db, filepath.Join(t.TempDir(), "workspaces"))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	fixedNow := time.Date(2026, 8, 2, 6, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return fixedNow }

	item, source, err := repository.EnsureForWorkflow(ctx, "workflow-1")
	if err != nil {
		t.Fatalf("EnsureForWorkflow() error = %v", err)
	}
	if item.Status != StatusRequested || source.LocalPath != "/tmp/source-repository" {
		t.Fatalf("workspace = %#v source = %#v", item, source)
	}
	second, _, err := repository.EnsureForWorkflow(ctx, "workflow-1")
	if err != nil || second.ID != item.ID {
		t.Fatalf("second EnsureForWorkflow() = %#v, %v", second, err)
	}

	lease, err := repository.Acquire(ctx, item.ID, "worker-a", time.Minute)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if lease.Workspace.Status != StatusPreparing || lease.Token == "" {
		t.Fatalf("lease = %#v", lease)
	}
	if _, err := repository.Acquire(ctx, item.ID, "worker-b", time.Minute); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("second Acquire() error = %v", err)
	}
	if err := repository.Release(ctx, item.ID, "wrong-token"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("Release(wrong token) error = %v", err)
	}

	ready, err := repository.MarkReady(ctx, item.ID, lease.Token, "abc123", false)
	if err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	if ready.Status != StatusReady || ready.HeadSHA != "abc123" || ready.Dirty {
		t.Fatalf("ready workspace = %#v", ready)
	}
	if err := repository.Release(ctx, item.ID, lease.Token); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func TestRepositoryAllowsExpiredLeaseTakeover(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	seedWorkspaceWorkflow(t, db)

	repository, _ := NewRepository(db, filepath.Join(t.TempDir(), "workspaces"))
	current := time.Date(2026, 8, 2, 6, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return current }
	item, _, err := repository.EnsureForWorkflow(ctx, "workflow-1")
	if err != nil {
		t.Fatal(err)
	}
	first, err := repository.Acquire(ctx, item.ID, "worker-a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	current = current.Add(2 * time.Second)
	second, err := repository.Acquire(ctx, item.ID, "worker-b", time.Minute)
	if err != nil {
		t.Fatalf("expired lease takeover error = %v", err)
	}
	if second.Token == first.Token || second.Workspace.LeaseOwner != "worker-b" {
		t.Fatalf("second lease = %#v", second)
	}
	if err := repository.Release(ctx, item.ID, first.Token); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale release error = %v", err)
	}
}

func TestWorkspaceCascadesWhenWorkflowIsDeleted(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	seedWorkspaceWorkflow(t, db)
	repository, _ := NewRepository(db, filepath.Join(t.TempDir(), "workspaces"))
	if _, _, err := repository.EnsureForWorkflow(ctx, "workflow-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM workflow_jobs WHERE id = 'workflow-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetByWorkflow(ctx, "workflow-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByWorkflow() error = %v", err)
	}
}

func seedWorkspaceWorkflow(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().UnixMilli()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`, []any{"project-1", "Project", now, now}},
		{`INSERT INTO repositories (id, project_id, local_path, current_branch, head_sha, remote_url, dirty, created_at, updated_at) VALUES (?, ?, ?, ?, ?, '', 0, ?, ?)`, []any{"repository-1", "project-1", "/tmp/source-repository", "main", "abc123", now, now}},
		{`INSERT INTO plans (id, project_id, title, status, current_version, created_at, updated_at) VALUES (?, ?, ?, 'READY', 1, ?, ?)`, []any{"plan-1", "project-1", "Plan", now, now}},
		{`INSERT INTO plan_versions (id, plan_id, version, requirement, acceptance_criteria_json, constraints_json, created_at) VALUES (?, ?, 1, 'Requirement', '[]', '[]', ?)`, []any{"plan-version-1", "plan-1", now}},
		{`INSERT INTO workflow_jobs (id, project_id, plan_id, plan_version_id, repository_id, base_branch, base_commit_sha, status, version, max_revisions, max_stage_attempts, max_tool_calls, wall_clock_seconds, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'main', 'abc123', 'WORKSPACE_PREPARING', 2, 3, 3, 120, 3600, ?, ?)`, []any{"workflow-1", "project-1", "plan-1", "plan-version-1", "repository-1", now, now}},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed database: %v", err)
		}
	}
}
