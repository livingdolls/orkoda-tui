package database

import (
	"context"
	"database/sql"
	"fmt"
)

var foundationStatements = []string{
	`CREATE TABLE IF NOT EXISTS projects (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS repositories (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		local_path TEXT NOT NULL UNIQUE,
		current_branch TEXT NOT NULL DEFAULT '',
		head_sha TEXT NOT NULL,
		remote_url TEXT NOT NULL DEFAULT '',
		dirty INTEGER NOT NULL DEFAULT 0 CHECK (dirty IN (0, 1)),
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_repositories_project
		ON repositories(project_id, created_at)`,
	`CREATE TABLE IF NOT EXISTS plans (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		title TEXT NOT NULL,
		status TEXT NOT NULL CHECK (status IN ('DRAFT', 'READY', 'PLANNING', 'NEEDS_INPUT', 'APPROVED', 'ARCHIVED')),
		current_version INTEGER NOT NULL DEFAULT 1 CHECK (current_version > 0),
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_plans_project_updated
		ON plans(project_id, updated_at DESC)`,
	`CREATE TABLE IF NOT EXISTS plan_versions (
		id TEXT PRIMARY KEY,
		plan_id TEXT NOT NULL,
		version INTEGER NOT NULL CHECK (version > 0),
		requirement TEXT NOT NULL,
		acceptance_criteria_json TEXT NOT NULL DEFAULT '[]',
		constraints_json TEXT NOT NULL DEFAULT '[]',
		created_at INTEGER NOT NULL,
		FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
		UNIQUE (plan_id, version)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_plan_versions_plan_version
		ON plan_versions(plan_id, version DESC)`,
	`CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		status TEXT NOT NULL CHECK (status IN ('QUEUED', 'RUNNING', 'COMPLETED', 'DEAD')),
		attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
		max_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
		run_after INTEGER NOT NULL,
		locked_by TEXT,
		locked_at INTEGER,
		last_error TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_jobs_claim
		ON jobs(status, run_after, created_at)`,
	`CREATE TABLE IF NOT EXISTS activity_events (
		sequence INTEGER PRIMARY KEY AUTOINCREMENT,
		job_id TEXT,
		type TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_activity_events_job_sequence
		ON activity_events(job_id, sequence)`,
}

// Migrate applies idempotent foundation migrations required by the local daemon.
func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()

	for index, statement := range foundationStatements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply migration statement %d: %w", index+1, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}
