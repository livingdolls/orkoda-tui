package jobqueue

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// EnqueueTx stores a durable dispatch in the caller's transaction. It allows a
// domain state transition and its queue message to commit atomically.
func (q *Queue) EnqueueTx(
	ctx context.Context,
	tx *sql.Tx,
	jobType, payloadJSON string,
	maxAttempts int,
	runAfter time.Time,
) (Job, error) {
	if tx == nil {
		return Job{}, fmt.Errorf("transaction is required")
	}
	return enqueueWith(ctx, tx, jobType, payloadJSON, maxAttempts, runAfter)
}

// ClaimTypes claims only jobs handled by the current scheduler. Durable jobs
// for capabilities that are not registered remain queued instead of being
// consumed and marked dead as unknown work.
func (q *Queue) ClaimTypes(
	ctx context.Context,
	workerID string,
	now time.Time,
	jobTypes []string,
) (*Job, error) {
	if workerID == "" {
		return nil, fmt.Errorf("worker ID is required")
	}

	jobTypes = normalizeJobTypes(jobTypes)
	if len(jobTypes) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(jobTypes)), ",")
	arguments := make([]any, 0, 4+len(jobTypes))
	arguments = append(arguments, workerID, now.UTC().UnixMilli(), now.UTC().UnixMilli(), now.UTC().UnixMilli())
	for _, jobType := range jobTypes {
		arguments = append(arguments, jobType)
	}

	row := q.db.QueryRowContext(ctx, `
		UPDATE jobs
		SET status = 'RUNNING',
			attempts = attempts + 1,
			locked_by = ?,
			locked_at = ?,
			updated_at = ?
		WHERE id = (
			SELECT id
			FROM jobs
			WHERE status = 'QUEUED' AND run_after <= ?
				AND type IN (`+placeholders+`)
			ORDER BY run_after, created_at
			LIMIT 1
		)
		RETURNING id, type, payload_json, status, attempts, max_attempts,
			run_after, locked_by, locked_at, last_error, created_at, updated_at
	`, arguments...)

	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim supported job: %w", err)
	}
	return &job, nil
}

func enqueueWith(
	ctx context.Context,
	executor interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	},
	jobType, payloadJSON string,
	maxAttempts int,
	runAfter time.Time,
) (Job, error) {
	jobType = strings.TrimSpace(jobType)
	if jobType == "" {
		return Job{}, fmt.Errorf("job type is required")
	}
	if maxAttempts < 1 {
		return Job{}, fmt.Errorf("max attempts must be greater than zero")
	}

	now := time.Now().UTC()
	job := Job{
		ID:          newID(),
		Type:        jobType,
		PayloadJSON: payloadJSON,
		Status:      "QUEUED",
		MaxAttempts: maxAttempts,
		RunAfter:    runAfter.UTC(),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := executor.ExecContext(ctx, `
		INSERT INTO jobs (
			id, type, payload_json, status, attempts, max_attempts,
			run_after, created_at, updated_at
		) VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?)
	`, job.ID, job.Type, job.PayloadJSON, job.Status, job.MaxAttempts,
		job.RunAfter.UnixMilli(), job.CreatedAt.UnixMilli(), job.UpdatedAt.UnixMilli()); err != nil {
		return Job{}, fmt.Errorf("enqueue job: %w", err)
	}
	return job, nil
}

func normalizeJobTypes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
