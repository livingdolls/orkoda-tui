package database

func init() {
	foundationStatements = append(foundationStatements,
		`CREATE TABLE IF NOT EXISTS check_runs (
			id TEXT PRIMARY KEY,
			workflow_job_id TEXT NOT NULL,
			execution_id TEXT NOT NULL,
			execution_version INTEGER NOT NULL CHECK (execution_version > 0),
			workspace_id TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('PENDING','RUNNING','PASSED','FAILED','CANCELLED')),
			total_steps INTEGER NOT NULL DEFAULT 0 CHECK (total_steps >= 0),
			passed_steps INTEGER NOT NULL DEFAULT 0 CHECK (passed_steps >= 0),
			failed_steps INTEGER NOT NULL DEFAULT 0 CHECK (failed_steps >= 0),
			started_at INTEGER,
			completed_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (workflow_job_id) REFERENCES workflow_jobs(id) ON DELETE CASCADE,
			FOREIGN KEY (execution_id) REFERENCES executions(id) ON DELETE CASCADE,
			FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
			UNIQUE (workflow_job_id, execution_version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_check_runs_workflow_created
			ON check_runs(workflow_job_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS check_steps (
			id TEXT PRIMARY KEY,
			check_run_id TEXT NOT NULL,
			sequence INTEGER NOT NULL CHECK (sequence > 0),
			profile TEXT NOT NULL,
			command_json TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('PENDING','RUNNING','PASSED','FAILED','CANCELLED')),
			exit_code INTEGER,
			duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
			output_text TEXT NOT NULL DEFAULT '',
			output_truncated INTEGER NOT NULL DEFAULT 0 CHECK (output_truncated IN (0,1)),
			artifact_key TEXT NOT NULL DEFAULT '',
			error_message TEXT,
			started_at INTEGER,
			completed_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (check_run_id) REFERENCES check_runs(id) ON DELETE CASCADE,
			UNIQUE (check_run_id, sequence),
			UNIQUE (check_run_id, profile)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_check_steps_run_sequence
			ON check_steps(check_run_id, sequence)`,
	)
}
