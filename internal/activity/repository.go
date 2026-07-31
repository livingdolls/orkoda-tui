package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

const (
	DefaultPageSize = 100
	MaxPageSize     = 500
)

type Event struct {
	Sequence    int64
	JobID       string
	Type        string
	PayloadJSON json.RawMessage
	CreatedAt   time.Time
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Append(ctx context.Context, jobID, eventType string, payloadJSON json.RawMessage, createdAt time.Time) (Event, error) {
	if r == nil || r.db == nil {
		return Event{}, fmt.Errorf("activity database is required")
	}
	if eventType == "" {
		return Event{}, fmt.Errorf("activity event type is required")
	}
	if len(payloadJSON) == 0 {
		payloadJSON = json.RawMessage(`{}`)
	}
	if !json.Valid(payloadJSON) {
		return Event{}, fmt.Errorf("activity payload must be valid JSON")
	}

	event := Event{
		JobID:       jobID,
		Type:        eventType,
		PayloadJSON: append(json.RawMessage(nil), payloadJSON...),
		CreatedAt:   createdAt.UTC(),
	}

	var nullableJobID any
	if jobID != "" {
		nullableJobID = jobID
	}

	err := r.db.QueryRowContext(ctx, `
		INSERT INTO activity_events (job_id, type, payload_json, created_at)
		VALUES (?, ?, ?, ?)
		RETURNING sequence
	`, nullableJobID, event.Type, string(event.PayloadJSON), event.CreatedAt.UnixMilli()).Scan(&event.Sequence)
	if err != nil {
		return Event{}, fmt.Errorf("append activity event: %w", err)
	}
	return event, nil
}

func (r *Repository) ListAfter(ctx context.Context, afterSequence int64, limit int) ([]Event, error) {
	return r.list(ctx, "", afterSequence, limit)
}

func (r *Repository) ListJobAfter(ctx context.Context, jobID string, afterSequence int64, limit int) ([]Event, error) {
	if jobID == "" {
		return nil, fmt.Errorf("job ID is required")
	}
	return r.list(ctx, jobID, afterSequence, limit)
}

func (r *Repository) list(ctx context.Context, jobID string, afterSequence int64, limit int) ([]Event, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("activity database is required")
	}
	if afterSequence < 0 {
		return nil, fmt.Errorf("after sequence must not be negative")
	}
	if limit < 1 || limit > MaxPageSize+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", MaxPageSize+1)
	}

	query := `
		SELECT sequence, job_id, type, payload_json, created_at
		FROM activity_events
		WHERE sequence > ?
	`
	args := []any{afterSequence}
	if jobID != "" {
		query += " AND job_id = ?"
		args = append(args, jobID)
	}
	query += " ORDER BY sequence ASC LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list activity events: %w", err)
	}
	defer rows.Close()

	events := make([]Event, 0, limit)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan activity event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activity events: %w", err)
	}
	return events, nil
}

func scanEvent(row interface{ Scan(...any) error }) (Event, error) {
	var event Event
	var jobID sql.NullString
	var payload string
	var createdAt int64

	if err := row.Scan(&event.Sequence, &jobID, &event.Type, &payload, &createdAt); err != nil {
		return Event{}, err
	}
	event.JobID = jobID.String
	event.PayloadJSON = json.RawMessage(payload)
	event.CreatedAt = time.UnixMilli(createdAt).UTC()
	return event, nil
}
