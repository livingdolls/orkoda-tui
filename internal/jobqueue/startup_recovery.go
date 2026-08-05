package jobqueue

import (
	"context"
	"fmt"
	"time"
)

// RecoverInterrupted immediately requeues jobs left RUNNING by a previous
// daemon process. The process-wide instance lock guarantees that no other
// scheduler is alive when startup recovery runs.
func (q *Queue) RecoverInterrupted(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, `
		UPDATE jobs
		SET status = 'QUEUED', locked_by = NULL, locked_at = NULL, updated_at = ?
		WHERE status = 'RUNNING'
		RETURNING id
	`, now.UTC().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("recover interrupted jobs: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan interrupted job: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate interrupted jobs: %w", err)
	}
	return ids, nil
}
