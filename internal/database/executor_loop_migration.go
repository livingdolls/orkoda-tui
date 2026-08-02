package database

func init() {
	foundationStatements = append(foundationStatements,
		`CREATE TABLE IF NOT EXISTS executor_iterations (
			id TEXT PRIMARY KEY,
			execution_id TEXT NOT NULL,
			sequence INTEGER NOT NULL CHECK (sequence > 0),
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('RUNNING','COMPLETED','FAILED','CANCELLED')),
			action_type TEXT NOT NULL CHECK (action_type IN ('tool','finish')),
			tool TEXT,
			action_summary_json TEXT NOT NULL DEFAULT '{}',
			result_summary_json TEXT NOT NULL DEFAULT '{}',
			usage_json TEXT NOT NULL DEFAULT '{}',
			error_code TEXT,
			error_message TEXT,
			started_at INTEGER NOT NULL,
			completed_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (execution_id) REFERENCES executions(id) ON DELETE CASCADE,
			UNIQUE (execution_id, sequence)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_executor_iterations_execution_sequence
			ON executor_iterations(execution_id, sequence)`,
	)
}
