package database

func init() {
	foundationStatements = append(foundationStatements,
		`CREATE TABLE IF NOT EXISTS approval_decisions (
			id TEXT PRIMARY KEY,
			workflow_job_id TEXT NOT NULL,
			review_run_id TEXT NOT NULL,
			execution_id TEXT NOT NULL,
			execution_version INTEGER NOT NULL CHECK (execution_version > 0),
			checkpoint_id TEXT NOT NULL,
			base_commit_sha TEXT NOT NULL,
			patch_checksum TEXT NOT NULL,
			decision TEXT NOT NULL CHECK (decision IN ('APPROVE','REQUEST_REVISION','REJECT')),
			status TEXT NOT NULL CHECK (status IN ('PENDING','APPLIED')),
			note TEXT NOT NULL DEFAULT '',
			review_override INTEGER NOT NULL DEFAULT 0 CHECK (review_override IN (0,1)),
			reviewer_verdict TEXT NOT NULL CHECK (reviewer_verdict IN ('APPROVE','REQUEST_REVISION')),
			check_status TEXT NOT NULL DEFAULT '',
			failed_steps INTEGER NOT NULL DEFAULT 0 CHECK (failed_steps >= 0),
			workflow_version_before INTEGER NOT NULL CHECK (workflow_version_before > 0),
			workflow_version_after INTEGER,
			revision_count_before INTEGER NOT NULL CHECK (revision_count_before >= 0),
			created_at INTEGER NOT NULL,
			applied_at INTEGER,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (workflow_job_id) REFERENCES workflow_jobs(id) ON DELETE CASCADE,
			FOREIGN KEY (review_run_id) REFERENCES review_runs(id) ON DELETE RESTRICT,
			FOREIGN KEY (execution_id) REFERENCES executions(id) ON DELETE RESTRICT,
			FOREIGN KEY (checkpoint_id) REFERENCES patch_checkpoints(id) ON DELETE RESTRICT,
			UNIQUE (workflow_job_id, execution_version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_approval_decisions_workflow_created
			ON approval_decisions(workflow_job_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS revision_requests (
			id TEXT PRIMARY KEY,
			approval_decision_id TEXT NOT NULL UNIQUE,
			instructions TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			FOREIGN KEY (approval_decision_id) REFERENCES approval_decisions(id) ON DELETE CASCADE
		)`,
	)
}
