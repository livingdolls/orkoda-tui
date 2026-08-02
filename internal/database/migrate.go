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
	`CREATE TABLE IF NOT EXISTS agent_settings (
		project_id TEXT PRIMARY KEY,
		version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS agent_configs (
		project_id TEXT NOT NULL,
		role TEXT NOT NULL CHECK (role IN ('PLANNER', 'EXECUTOR', 'REVIEWER')),
		provider TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		temperature REAL NOT NULL CHECK (temperature >= 0 AND temperature <= 2),
		max_output_tokens INTEGER NOT NULL CHECK (max_output_tokens >= 256 AND max_output_tokens <= 65536),
		enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
		system_instruction TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (project_id, role),
		FOREIGN KEY (project_id) REFERENCES agent_settings(project_id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_configs_project_role
		ON agent_configs(project_id, role)`,
	`CREATE TABLE IF NOT EXISTS tool_policies (
		project_id TEXT NOT NULL,
		role TEXT NOT NULL CHECK (role IN ('PLANNER', 'EXECUTOR', 'REVIEWER')),
		allowed_tools_json TEXT NOT NULL DEFAULT '[]',
		allowed_command_profiles_json TEXT NOT NULL DEFAULT '[]',
		network_access TEXT NOT NULL DEFAULT 'DISABLED'
			CHECK (network_access IN ('DISABLED', 'LOOPBACK', 'OUTBOUND')),
		filesystem_access TEXT NOT NULL DEFAULT 'READ_ONLY'
			CHECK (filesystem_access IN ('READ_ONLY', 'WORKSPACE_WRITE')),
		command_timeout_ms INTEGER NOT NULL CHECK (command_timeout_ms >= 1000 AND command_timeout_ms <= 600000),
		max_command_output_bytes INTEGER NOT NULL CHECK (max_command_output_bytes >= 1024 AND max_command_output_bytes <= 33554432),
		max_file_bytes INTEGER NOT NULL CHECK (max_file_bytes >= 1024 AND max_file_bytes <= 33554432),
		max_patch_bytes INTEGER NOT NULL CHECK (max_patch_bytes >= 1024 AND max_patch_bytes <= 33554432),
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (project_id, role),
		FOREIGN KEY (project_id) REFERENCES agent_settings(project_id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_tool_policies_project_role
		ON tool_policies(project_id, role)`,
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
	`CREATE TABLE IF NOT EXISTS repository_summaries (
		id TEXT PRIMARY KEY,
		repository_id TEXT NOT NULL,
		head_sha TEXT NOT NULL,
		summary_json TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE,
		UNIQUE (repository_id, head_sha)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_repository_summaries_current
		ON repository_summaries(repository_id, created_at DESC)`,
	`CREATE TABLE IF NOT EXISTS planning_contexts (
		id TEXT PRIMARY KEY,
		plan_id TEXT NOT NULL,
		plan_version_id TEXT NOT NULL,
		repository_summary_id TEXT NOT NULL,
		normalized_plan_json TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
		FOREIGN KEY (plan_version_id) REFERENCES plan_versions(id) ON DELETE CASCADE,
		FOREIGN KEY (repository_summary_id) REFERENCES repository_summaries(id) ON DELETE CASCADE,
		UNIQUE (plan_version_id, repository_summary_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_planning_contexts_plan_created
		ON planning_contexts(plan_id, created_at DESC)`,
	`CREATE TABLE IF NOT EXISTS planning_runs (
		id TEXT PRIMARY KEY,
		plan_id TEXT NOT NULL,
		plan_version_id TEXT NOT NULL,
		planning_context_id TEXT NOT NULL,
		parent_run_id TEXT,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		status TEXT NOT NULL CHECK (status IN ('RUNNING', 'NEEDS_INPUT', 'COMPLETED', 'FAILED', 'CANCELLED', 'SUPERSEDED')),
		response_json TEXT,
		usage_json TEXT NOT NULL DEFAULT '{}',
		error_code TEXT,
		error_message TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
		FOREIGN KEY (plan_version_id) REFERENCES plan_versions(id) ON DELETE CASCADE,
		FOREIGN KEY (planning_context_id) REFERENCES planning_contexts(id) ON DELETE CASCADE,
		FOREIGN KEY (parent_run_id) REFERENCES planning_runs(id) ON DELETE SET NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_planning_runs_plan_created
		ON planning_runs(plan_id, created_at DESC)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_planning_runs_active_plan
		ON planning_runs(plan_id)
		WHERE status IN ('RUNNING', 'NEEDS_INPUT')`,
	`CREATE TABLE IF NOT EXISTS planning_questions (
		id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		position INTEGER NOT NULL CHECK (position >= 0),
		question TEXT NOT NULL,
		answer TEXT,
		status TEXT NOT NULL CHECK (status IN ('OPEN', 'ANSWERED')),
		created_at INTEGER NOT NULL,
		answered_at INTEGER,
		FOREIGN KEY (run_id) REFERENCES planning_runs(id) ON DELETE CASCADE,
		UNIQUE (run_id, position)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_planning_questions_run_position
		ON planning_questions(run_id, position)`,
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
