package database

func init() {
	foundationStatements = append(foundationStatements,
		`CREATE TABLE IF NOT EXISTS review_runs (
			id TEXT PRIMARY KEY,
			workflow_job_id TEXT NOT NULL,
			execution_id TEXT NOT NULL,
			execution_version INTEGER NOT NULL CHECK (execution_version > 0),
			check_run_id TEXT NOT NULL,
			checkpoint_id TEXT NOT NULL,
			agent_settings_version INTEGER NOT NULL CHECK (agent_settings_version > 0),
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('PENDING','RUNNING','COMPLETED','FAILED','CANCELLED')),
			verdict TEXT CHECK (verdict IS NULL OR verdict IN ('APPROVE','REQUEST_REVISION')),
			summary TEXT NOT NULL DEFAULT '',
			total_issues INTEGER NOT NULL DEFAULT 0 CHECK (total_issues >= 0),
			blocking_issues INTEGER NOT NULL DEFAULT 0 CHECK (blocking_issues >= 0),
			usage_json TEXT NOT NULL DEFAULT '{}',
			failure_code TEXT,
			failure_message TEXT,
			started_at INTEGER,
			completed_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (workflow_job_id) REFERENCES workflow_jobs(id) ON DELETE CASCADE,
			FOREIGN KEY (execution_id) REFERENCES executions(id) ON DELETE CASCADE,
			FOREIGN KEY (check_run_id) REFERENCES check_runs(id) ON DELETE RESTRICT,
			FOREIGN KEY (checkpoint_id) REFERENCES patch_checkpoints(id) ON DELETE RESTRICT,
			UNIQUE (workflow_job_id, execution_version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_review_runs_workflow_created
			ON review_runs(workflow_job_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS review_issues (
			id TEXT PRIMARY KEY,
			review_run_id TEXT NOT NULL,
			position INTEGER NOT NULL CHECK (position >= 0),
			issue_key TEXT NOT NULL,
			severity TEXT NOT NULL CHECK (severity IN ('CRITICAL','HIGH','MEDIUM','LOW')),
			category TEXT NOT NULL CHECK (category IN ('CORRECTNESS','SECURITY','RELIABILITY','PERFORMANCE','MAINTAINABILITY','TESTING','REQUIREMENT')),
			blocking INTEGER NOT NULL CHECK (blocking IN (0,1)),
			title TEXT NOT NULL,
			description TEXT NOT NULL,
			file_path TEXT,
			line_start INTEGER NOT NULL DEFAULT 0 CHECK (line_start >= 0),
			line_end INTEGER NOT NULL DEFAULT 0 CHECK (line_end >= 0),
			criteria_refs_json TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL,
			FOREIGN KEY (review_run_id) REFERENCES review_runs(id) ON DELETE CASCADE,
			UNIQUE (review_run_id, position),
			UNIQUE (review_run_id, issue_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_review_issues_run_position
			ON review_issues(review_run_id, position)`,
	)
}
