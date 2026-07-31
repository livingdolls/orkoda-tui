package jobqueue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var ErrJobNotRunning = errors.New("job is not running")

type Queue struct {
	db *sql.DB
}

type Job struct {
	ID          string
	Type        string
	PayloadJSON string
	Status      string
	Attempts    int
	MaxAttempts int
	RunAfter    time.Time
	LockedBy    string
	LockedAt    *time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func New(db *sql.DB) *Queue {
	return &Queue{db: db}
}

func (q *Queue) Enqueue(ctx context.Context, jobType, payloadJSON string, maxAttempts int, runAfter time.Time) (Job, error) {
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

	_, err := q.db.ExecContext(ctx, `
		INSERT INTO jobs (
			id, type, payload_json, status, attempts, max_attempts,
			run_after, created_at, updated_at
		) VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?)
	`, job.ID, job.Type, job.PayloadJSON, job.Status, job.MaxAttempts,
		job.RunAfter.UnixMilli(), job.CreatedAt.UnixMilli(), job.UpdatedAt.UnixMilli())
	if err != nil {
		return Job{}, fmt.Errorf("enqueue job: %w", err)
	}
	return job, nil
}

func (q *Queue) Claim(ctx context.Context, workerID string, now time.Time) (*Job, error) {
	if workerID == "" {
		return nil, fmt.Errorf("worker ID is required")
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
			ORDER BY run_after, created_at
			LIMIT 1
		)
		RETURNING id, type, payload_json, status, attempts, max_attempts,
			run_after, locked_by, locked_at, last_error, created_at, updated_at
	`, workerID, now.UTC().UnixMilli(), now.UTC().UnixMilli(), now.UTC().UnixMilli())

	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim job: %w", err)
	}
	return &job, nil
}

func (q *Queue) Complete(ctx context.Context, id string, now time.Time) error {
	result, err := q.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'COMPLETED', locked_by = NULL, locked_at = NULL, updated_at = ?
		WHERE id = ? AND status = 'RUNNING'
	`, now.UTC().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	return requireOneRunning(result)
}

func (q *Queue) Fail(ctx context.Context, id, failure string, retryAt time.Time, now time.Time) (string, error) {
	var status string
	err := q.db.QueryRowContext(ctx, `
		UPDATE jobs
		SET status = CASE WHEN attempts >= max_attempts THEN 'DEAD' ELSE 'QUEUED' END,
			run_after = CASE WHEN attempts >= max_attempts THEN run_after ELSE ? END,
			last_error = ?,
			locked_by = NULL,
			locked_at = NULL,
			updated_at = ?
		WHERE id = ? AND status = 'RUNNING'
		RETURNING status
	`, retryAt.UTC().UnixMilli(), failure, now.UTC().UnixMilli(), id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrJobNotRunning
	}
	if err != nil {
		return "", fmt.Errorf("fail job: %w", err)
	}
	return status, nil
}

func (q *Queue) RecoverStale(ctx context.Context, lockedBefore time.Time, now time.Time) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, `
		UPDATE jobs
		SET status = 'QUEUED', locked_by = NULL, locked_at = NULL, updated_at = ?
		WHERE status = 'RUNNING' AND locked_at < ?
		RETURNING id
	`, now.UTC().UnixMilli(), lockedBefore.UTC().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("recover stale jobs: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan recovered stale job: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recovered stale jobs: %w", err)
	}
	return ids, nil
}

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var job Job
	var runAfter, createdAt, updatedAt int64
	var lockedAt sql.NullInt64
	var lockedBy, lastError sql.NullString

	err := row.Scan(
		&job.ID, &job.Type, &job.PayloadJSON, &job.Status,
		&job.Attempts, &job.MaxAttempts, &runAfter, &lockedBy,
		&lockedAt, &lastError, &createdAt, &updatedAt,
	)
	if err != nil {
		return Job{}, err
	}

	job.RunAfter = time.UnixMilli(runAfter).UTC()
	job.CreatedAt = time.UnixMilli(createdAt).UTC()
	job.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	job.LockedBy = lockedBy.String
	job.LastError = lastError.String
	if lockedAt.Valid {
		value := time.UnixMilli(lockedAt.Int64).UTC()
		job.LockedAt = &value
	}
	return job, nil
}

func requireOneRunning(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows: %w", err)
	}
	if count != 1 {
		return ErrJobNotRunning
	}
	return nil
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate job ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}
