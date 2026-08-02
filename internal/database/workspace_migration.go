package database

func init() {
	foundationStatements = append(foundationStatements,
		`CREATE TABLE IF NOT EXISTS workspaces (
			id TEXT PRIMARY KEY,
			workflow_job_id TEXT NOT NULL UNIQUE,
			project_id TEXT NOT NULL,
			repository_id TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			base_commit_sha TEXT NOT NULL,
			head_sha TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK (status IN (
				'REQUESTED', 'PREPARING', 'READY', 'WRITE_LOCKED',
				'ARCHIVED', 'FAILED'
			)),
			dirty INTEGER NOT NULL DEFAULT 0 CHECK (dirty IN (0, 1)),
			lease_owner TEXT,
			lease_token TEXT,
			lease_expires_at INTEGER,
			failure_message TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (workflow_job_id) REFERENCES workflow_jobs(id) ON DELETE CASCADE,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE RESTRICT,
			CHECK (
				(lease_owner IS NULL AND lease_token IS NULL AND lease_expires_at IS NULL)
				OR
				(lease_owner IS NOT NULL AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)
			)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_workspaces_project_updated
			ON workspaces(project_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_workspaces_lease_expiry
			ON workspaces(lease_expires_at)
			WHERE lease_expires_at IS NOT NULL`,
	)
}
