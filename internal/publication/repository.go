package publication

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("publication not found")

type Record struct {
	ID                 string     `json:"id"`
	WorkflowJobID      string     `json:"workflow_job_id"`
	ExecutionVersion   int        `json:"execution_version"`
	ApprovalDecisionID string     `json:"approval_decision_id"`
	BaseCommitSHA      string     `json:"base_commit_sha"`
	PatchChecksum      string     `json:"patch_checksum"`
	PublishedCommitSHA string     `json:"published_commit_sha"`
	Status             string     `json:"status"`
	ErrorMessage       string     `json:"error_message,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
}

type Store interface {
	GetByWorkflow(context.Context, string) (Record, error)
	Complete(context.Context, Record) (Record, error)
}

type Repository struct {
	db  *sql.DB
	now func() time.Time
}

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	return &Repository{db: db, now: time.Now}, nil
}

func (r *Repository) GetByWorkflow(ctx context.Context, workflowID string) (Record, error) {
	return scan(r.db.QueryRowContext(ctx, `
		SELECT id, workflow_job_id, execution_version, approval_decision_id,
			base_commit_sha, patch_checksum, published_commit_sha, status,
			COALESCE(error_message, ''), created_at, completed_at
		FROM publications WHERE workflow_job_id = ?
	`, strings.TrimSpace(workflowID)))
}

func (r *Repository) Complete(ctx context.Context, item Record) (Record, error) {
	item.WorkflowJobID = strings.TrimSpace(item.WorkflowJobID)
	item.ApprovalDecisionID = strings.TrimSpace(item.ApprovalDecisionID)
	item.BaseCommitSHA = strings.TrimSpace(item.BaseCommitSHA)
	item.PatchChecksum = strings.TrimSpace(item.PatchChecksum)
	item.PublishedCommitSHA = strings.TrimSpace(item.PublishedCommitSHA)
	if item.WorkflowJobID == "" || item.ApprovalDecisionID == "" || item.BaseCommitSHA == "" ||
		item.PatchChecksum == "" || item.PublishedCommitSHA == "" || item.ExecutionVersion < 1 {
		return Record{}, fmt.Errorf("invalid publication record")
	}
	now := r.now().UTC()
	if item.ID == "" {
		item.ID = newID()
	}
	item.Status = "COMPLETED"
	item.CreatedAt = now
	item.CompletedAt = &now
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO publications (
			id, workflow_job_id, execution_version, approval_decision_id,
			base_commit_sha, patch_checksum, published_commit_sha, status,
			created_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'COMPLETED', ?, ?)
		ON CONFLICT(workflow_job_id) DO UPDATE SET
			execution_version = excluded.execution_version,
			approval_decision_id = excluded.approval_decision_id,
			base_commit_sha = excluded.base_commit_sha,
			patch_checksum = excluded.patch_checksum,
			published_commit_sha = excluded.published_commit_sha,
			status = excluded.status,
			error_message = NULL,
			completed_at = excluded.completed_at
	`, item.ID, item.WorkflowJobID, item.ExecutionVersion, item.ApprovalDecisionID,
		item.BaseCommitSHA, item.PatchChecksum, item.PublishedCommitSHA,
		now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return Record{}, fmt.Errorf("save publication: %w", err)
	}
	return r.GetByWorkflow(ctx, item.WorkflowJobID)
}

func scan(row interface{ Scan(...any) error }) (Record, error) {
	var item Record
	var created int64
	var completed sql.NullInt64
	if err := row.Scan(
		&item.ID, &item.WorkflowJobID, &item.ExecutionVersion, &item.ApprovalDecisionID,
		&item.BaseCommitSHA, &item.PatchChecksum, &item.PublishedCommitSHA,
		&item.Status, &item.ErrorMessage, &created, &completed,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Record{}, ErrNotFound
		}
		return Record{}, err
	}
	item.CreatedAt = time.UnixMilli(created).UTC()
	if completed.Valid {
		value := time.UnixMilli(completed.Int64).UTC()
		item.CompletedAt = &value
	}
	return item, nil
}

func newID() string {
	return fmt.Sprintf("publication-%d", time.Now().UnixNano())
}
