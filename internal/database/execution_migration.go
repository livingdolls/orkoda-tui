package database

func init() {
	foundationStatements = append(foundationStatements,
		`CREATE TABLE IF NOT EXISTS executions (
			id TEXT PRIMARY KEY,
			workflow_job_id TEXT NOT NULL,
			workflow_version INTEGER NOT NULL CHECK (workflow_version > 0),
			execution_version INTEGER NOT NULL CHECK (execution_version > 0),
			plan_version_id TEXT NOT NULL,
			workspace_id TEXT NOT NULL,
			base_commit_sha TEXT NOT NULL,
			agent_settings_version INTEGER NOT NULL CHECK (agent_settings_version > 0),
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('PENDING','RUNNING','COMPLETED','FAILED','CANCELLED')),
			tool_calls INTEGER NOT NULL DEFAULT 0 CHECK (tool_calls >= 0),
			failure_code TEXT,
			failure_message TEXT,
			started_at INTEGER,
			completed_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (workflow_job_id) REFERENCES workflow_jobs(id) ON DELETE CASCADE,
			FOREIGN KEY (plan_version_id) REFERENCES plan_versions(id) ON DELETE RESTRICT,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
			UNIQUE (workflow_job_id, execution_version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_executions_workflow_created
			ON executions(workflow_job_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS tool_runs (
			id TEXT PRIMARY KEY,
			execution_id TEXT NOT NULL,
			sequence INTEGER NOT NULL CHECK (sequence > 0),
			tool TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('PENDING','RUNNING','COMPLETED','FAILED','CANCELLED')),
			input_summary_json TEXT NOT NULL DEFAULT '{}',
			output_summary_json TEXT NOT NULL DEFAULT '{}',
			error_code TEXT,
			error_message TEXT,
			started_at INTEGER,
			completed_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (execution_id) REFERENCES executions(id) ON DELETE CASCADE,
			UNIQUE (execution_id, sequence)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tool_runs_execution_sequence
			ON tool_runs(execution_id, sequence)`,
		`CREATE TABLE IF NOT EXISTS patch_checkpoints (
			id TEXT PRIMARY KEY,
			execution_id TEXT NOT NULL,
			sequence INTEGER NOT NULL CHECK (sequence > 0),
			base_commit_sha TEXT NOT NULL,
			workspace_head_sha TEXT NOT NULL,
			patch_checksum TEXT NOT NULL,
			patch_bytes INTEGER NOT NULL CHECK (patch_bytes >= 0),
			changed_files_json TEXT NOT NULL DEFAULT '[]',
			patch_text TEXT NOT NULL,
			artifact_key TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			FOREIGN KEY (execution_id) REFERENCES executions(id) ON DELETE CASCADE,
			UNIQUE (execution_id, sequence),
			UNIQUE (execution_id, patch_checksum)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_patch_checkpoints_execution_sequence
			ON patch_checkpoints(execution_id, sequence DESC)`,
	)
}
