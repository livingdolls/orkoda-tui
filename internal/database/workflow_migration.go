package database

func init() {
	foundationStatements = append(foundationStatements,
		`CREATE TABLE IF NOT EXISTS workflow_jobs (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			plan_id TEXT NOT NULL,
			plan_version_id TEXT NOT NULL,
			repository_id TEXT NOT NULL,
			base_branch TEXT NOT NULL,
			base_commit_sha TEXT NOT NULL,
			agent_settings_version INTEGER NOT NULL DEFAULT 0 CHECK (agent_settings_version >= 0),
			executor_provider TEXT NOT NULL DEFAULT '',
			executor_model TEXT NOT NULL DEFAULT '',
			reviewer_provider TEXT NOT NULL DEFAULT '',
			reviewer_model TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK (status IN (
				'READY', 'WORKSPACE_PREPARING', 'QUEUED', 'EXECUTING',
				'CHECKING', 'REVIEWING', 'WAITING_FOR_APPROVAL',
				'REVISION_REQUIRED', 'APPROVED', 'PUBLISHING',
				'COMPLETED', 'FAILED', 'REJECTED', 'CANCELLED'
			)),
			version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
			current_dispatch_id TEXT,
			retry_status TEXT,
			execution_version INTEGER NOT NULL DEFAULT 0 CHECK (execution_version >= 0),
			revision_count INTEGER NOT NULL DEFAULT 0 CHECK (revision_count >= 0),
			max_revisions INTEGER NOT NULL DEFAULT 3 CHECK (max_revisions >= 0),
			max_stage_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_stage_attempts > 0),
			max_executor_turns INTEGER NOT NULL DEFAULT 32 CHECK (max_executor_turns > 0),
			max_tool_calls INTEGER NOT NULL DEFAULT 24 CHECK (max_tool_calls > 0),
			max_consecutive_tool_errors INTEGER NOT NULL DEFAULT 3 CHECK (max_consecutive_tool_errors > 0),
			max_no_progress_turns INTEGER NOT NULL DEFAULT 4 CHECK (max_no_progress_turns > 0),
			wall_clock_seconds INTEGER NOT NULL DEFAULT 3600 CHECK (wall_clock_seconds > 0),
			cancellation_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancellation_requested IN (0, 1)),
			failure_code TEXT,
			failure_message TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			completed_at INTEGER,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
			FOREIGN KEY (plan_version_id) REFERENCES plan_versions(id) ON DELETE RESTRICT,
			FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE RESTRICT,
			FOREIGN KEY (current_dispatch_id) REFERENCES jobs(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_jobs_project_updated
			ON workflow_jobs(project_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_jobs_status_updated
			ON workflow_jobs(status, updated_at ASC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_workflow_jobs_active_plan
			ON workflow_jobs(plan_version_id, repository_id)
			WHERE status NOT IN ('COMPLETED', 'REJECTED', 'CANCELLED')`,
		`CREATE TABLE IF NOT EXISTS workflow_job_transitions (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			workflow_job_id TEXT NOT NULL,
			from_status TEXT,
			action TEXT NOT NULL,
			to_status TEXT NOT NULL,
			workflow_version INTEGER NOT NULL CHECK (workflow_version > 0),
			dispatch_job_id TEXT,
			details_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL,
			FOREIGN KEY (workflow_job_id) REFERENCES workflow_jobs(id) ON DELETE CASCADE,
			FOREIGN KEY (dispatch_job_id) REFERENCES jobs(id) ON DELETE SET NULL,
			UNIQUE (workflow_job_id, workflow_version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_transitions_job_sequence
			ON workflow_job_transitions(workflow_job_id, sequence)`,
	)
}
