package approval

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound         = errors.New("approval decision not found")
	ErrInvalid          = errors.New("invalid approval decision")
	ErrSnapshotConflict = errors.New("approval decision conflicts with persisted snapshot")
)

type Kind string

type Status string

const (
	KindApprove         Kind = "APPROVE"
	KindRequestRevision Kind = "REQUEST_REVISION"
	KindReject          Kind = "REJECT"

	StatusPending Status = "PENDING"
	StatusApplied Status = "APPLIED"
)

type Decision struct {
	ID                    string     `json:"id"`
	WorkflowJobID         string     `json:"workflow_job_id"`
	ReviewRunID           string     `json:"review_run_id"`
	ExecutionID           string     `json:"execution_id"`
	ExecutionVersion      int        `json:"execution_version"`
	CheckpointID          string     `json:"checkpoint_id"`
	BaseCommitSHA         string     `json:"base_commit_sha"`
	PatchChecksum         string     `json:"patch_checksum"`
	Kind                  Kind       `json:"decision"`
	Status                Status     `json:"status"`
	Note                  string     `json:"note"`
	RevisionInstructions  string     `json:"revision_instructions,omitempty"`
	ReviewOverride        bool       `json:"review_override"`
	ReviewerVerdict       string     `json:"reviewer_verdict"`
	CheckStatus           string     `json:"check_status"`
	FailedSteps           int        `json:"failed_steps"`
	WorkflowVersionBefore int        `json:"workflow_version_before"`
	WorkflowVersionAfter  int        `json:"workflow_version_after,omitempty"`
	RevisionCountBefore   int        `json:"revision_count_before"`
	CreatedAt             time.Time  `json:"created_at"`
	AppliedAt             *time.Time `json:"applied_at,omitempty"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type CreateInput struct {
	WorkflowJobID         string
	ReviewRunID           string
	ExecutionID           string
	ExecutionVersion      int
	CheckpointID          string
	BaseCommitSHA         string
	PatchChecksum         string
	Kind                  Kind
	Note                  string
	RevisionInstructions  string
	ReviewOverride        bool
	ReviewerVerdict       string
	CheckStatus           string
	FailedSteps           int
	WorkflowVersionBefore int
	RevisionCountBefore   int
}

type Store interface {
	CreateOrGet(context.Context, CreateInput) (Decision, bool, error)
	Get(context.Context, string) (Decision, error)
	GetByVersion(context.Context, string, int) (Decision, error)
	ListWorkflow(context.Context, string) ([]Decision, error)
	MarkApplied(context.Context, string, int) (Decision, error)
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

func (r *Repository) CreateOrGet(ctx context.Context, input CreateInput) (Decision, bool, error) {
	input = normalizeInput(input)
	if err := validateInput(input); err != nil {
		return Decision{}, false, err
	}
	if existing, err := r.GetByVersion(ctx, input.WorkflowJobID, input.ExecutionVersion); err == nil {
		if !sameDecision(existing, input) {
			return Decision{}, false, ErrSnapshotConflict
		}
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Decision{}, false, err
	}

	now := r.now().UTC()
	item := Decision{
		ID:                    newID(),
		WorkflowJobID:         input.WorkflowJobID,
		ReviewRunID:           input.ReviewRunID,
		ExecutionID:           input.ExecutionID,
		ExecutionVersion:      input.ExecutionVersion,
		CheckpointID:          input.CheckpointID,
		BaseCommitSHA:         input.BaseCommitSHA,
		PatchChecksum:         input.PatchChecksum,
		Kind:                  input.Kind,
		Status:                StatusPending,
		Note:                  input.Note,
		RevisionInstructions:  input.RevisionInstructions,
		ReviewOverride:        input.ReviewOverride,
		ReviewerVerdict:       input.ReviewerVerdict,
		CheckStatus:           input.CheckStatus,
		FailedSteps:           input.FailedSteps,
		WorkflowVersionBefore: input.WorkflowVersionBefore,
		RevisionCountBefore:   input.RevisionCountBefore,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Decision{}, false, fmt.Errorf("begin approval decision: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO approval_decisions (
			id, workflow_job_id, review_run_id, execution_id, execution_version,
			checkpoint_id, base_commit_sha, patch_checksum, decision, status,
			note, review_override, reviewer_verdict, workflow_version_before,
			revision_count_before, check_status, failed_steps, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.WorkflowJobID, item.ReviewRunID, item.ExecutionID,
		item.ExecutionVersion, item.CheckpointID, item.BaseCommitSHA, item.PatchChecksum,
		item.Kind, item.Note, boolInt(item.ReviewOverride), item.ReviewerVerdict,
		item.WorkflowVersionBefore, item.RevisionCountBefore, item.CheckStatus, item.FailedSteps,
		now.UnixMilli(), now.UnixMilli())
	if err != nil {
		if existing, getErr := r.GetByVersion(ctx, input.WorkflowJobID, input.ExecutionVersion); getErr == nil {
			if !sameDecision(existing, input) {
				return Decision{}, false, ErrSnapshotConflict
			}
			return existing, false, nil
		}
		return Decision{}, false, fmt.Errorf("insert approval decision: %w", err)
	}
	if item.Kind == KindRequestRevision {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO revision_requests (id, approval_decision_id, instructions, created_at)
			VALUES (?, ?, ?, ?)
		`, newID(), item.ID, item.RevisionInstructions, now.UnixMilli()); err != nil {
			return Decision{}, false, fmt.Errorf("insert revision request: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Decision{}, false, fmt.Errorf("commit approval decision: %w", err)
	}
	return item, true, nil
}

func (r *Repository) Get(ctx context.Context, id string) (Decision, error) {
	return scanDecision(r.db.QueryRowContext(ctx, `
		SELECT `+decisionColumns+` FROM approval_decisions d
		LEFT JOIN revision_requests rr ON rr.approval_decision_id = d.id
		WHERE d.id = ?
	`, strings.TrimSpace(id)))
}

func (r *Repository) GetByVersion(ctx context.Context, workflowID string, executionVersion int) (Decision, error) {
	return scanDecision(r.db.QueryRowContext(ctx, `
		SELECT `+decisionColumns+` FROM approval_decisions d
		LEFT JOIN revision_requests rr ON rr.approval_decision_id = d.id
		WHERE d.workflow_job_id = ? AND d.execution_version = ?
	`, strings.TrimSpace(workflowID), executionVersion))
}

func (r *Repository) ListWorkflow(ctx context.Context, workflowID string) ([]Decision, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+decisionColumns+` FROM approval_decisions d
		LEFT JOIN revision_requests rr ON rr.approval_decision_id = d.id
		WHERE d.workflow_job_id = ? ORDER BY d.execution_version DESC
	`, strings.TrimSpace(workflowID))
	if err != nil {
		return nil, fmt.Errorf("list approval decisions: %w", err)
	}
	defer rows.Close()
	items := make([]Decision, 0)
	for rows.Next() {
		item, scanErr := scanDecision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) MarkApplied(ctx context.Context, id string, workflowVersion int) (Decision, error) {
	if workflowVersion < 1 {
		return Decision{}, ErrInvalid
	}
	now := r.now().UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE approval_decisions
		SET status = 'APPLIED', workflow_version_after = ?,
			applied_at = COALESCE(applied_at, ?), updated_at = ?
		WHERE id = ? AND status IN ('PENDING','APPLIED')
	`, workflowVersion, now.UnixMilli(), now.UnixMilli(), strings.TrimSpace(id))
	if err != nil {
		return Decision{}, fmt.Errorf("mark approval decision applied: %w", err)
	}
	if count, err := result.RowsAffected(); err != nil {
		return Decision{}, err
	} else if count != 1 {
		return Decision{}, ErrNotFound
	}
	return r.Get(ctx, id)
}

const decisionColumns = `
	d.id, d.workflow_job_id, d.review_run_id, d.execution_id, d.execution_version,
	d.checkpoint_id, d.base_commit_sha, d.patch_checksum, d.decision, d.status,
	d.note, COALESCE(rr.instructions, ''), d.review_override, d.reviewer_verdict,
	d.workflow_version_before, COALESCE(d.workflow_version_after, 0),
	d.revision_count_before, COALESCE(d.check_status, ''), COALESCE(d.failed_steps, 0),
	d.created_at, d.applied_at, d.updated_at`

func scanDecision(scanner interface{ Scan(...any) error }) (Decision, error) {
	var item Decision
	var override int
	var createdAt, updatedAt int64
	var appliedAt sql.NullInt64
	err := scanner.Scan(
		&item.ID, &item.WorkflowJobID, &item.ReviewRunID, &item.ExecutionID,
		&item.ExecutionVersion, &item.CheckpointID, &item.BaseCommitSHA,
		&item.PatchChecksum, &item.Kind, &item.Status, &item.Note,
		&item.RevisionInstructions, &override, &item.ReviewerVerdict,
		&item.WorkflowVersionBefore, &item.WorkflowVersionAfter,
		&item.RevisionCountBefore, &item.CheckStatus, &item.FailedSteps,
		&createdAt, &appliedAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Decision{}, ErrNotFound
	}
	if err != nil {
		return Decision{}, fmt.Errorf("scan approval decision: %w", err)
	}
	item.ReviewOverride = override == 1
	item.CreatedAt = time.UnixMilli(createdAt).UTC()
	item.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	if appliedAt.Valid {
		value := time.UnixMilli(appliedAt.Int64).UTC()
		item.AppliedAt = &value
	}
	return item, nil
}

func normalizeInput(input CreateInput) CreateInput {
	input.WorkflowJobID = strings.TrimSpace(input.WorkflowJobID)
	input.ReviewRunID = strings.TrimSpace(input.ReviewRunID)
	input.ExecutionID = strings.TrimSpace(input.ExecutionID)
	input.CheckpointID = strings.TrimSpace(input.CheckpointID)
	input.BaseCommitSHA = strings.TrimSpace(input.BaseCommitSHA)
	input.PatchChecksum = strings.TrimSpace(input.PatchChecksum)
	input.Kind = Kind(strings.ToUpper(strings.TrimSpace(string(input.Kind))))
	input.Note = strings.TrimSpace(input.Note)
	input.RevisionInstructions = strings.TrimSpace(input.RevisionInstructions)
	input.ReviewerVerdict = strings.ToUpper(strings.TrimSpace(input.ReviewerVerdict))
	input.CheckStatus = strings.ToUpper(strings.TrimSpace(input.CheckStatus))
	return input
}

func validateInput(input CreateInput) error {
	if input.WorkflowJobID == "" || input.ReviewRunID == "" || input.ExecutionID == "" ||
		input.CheckpointID == "" || input.BaseCommitSHA == "" || input.PatchChecksum == "" ||
		input.ExecutionVersion < 1 || input.WorkflowVersionBefore < 1 || input.RevisionCountBefore < 0 {
		return fmt.Errorf("%w: snapshot fields are required", ErrInvalid)
	}
	if input.Kind != KindApprove && input.Kind != KindRequestRevision && input.Kind != KindReject {
		return fmt.Errorf("%w: decision is invalid", ErrInvalid)
	}
	if input.ReviewerVerdict != "APPROVE" && input.ReviewerVerdict != "REQUEST_REVISION" {
		return fmt.Errorf("%w: reviewer verdict is invalid", ErrInvalid)
	}
	if len(input.Note) > 4000 || len(input.RevisionInstructions) > 8000 {
		return fmt.Errorf("%w: decision text exceeds the allowed size", ErrInvalid)
	}
	if input.Kind == KindRequestRevision && input.RevisionInstructions == "" {
		return fmt.Errorf("%w: revision instructions are required", ErrInvalid)
	}
	if input.Kind != KindRequestRevision && input.RevisionInstructions != "" {
		return fmt.Errorf("%w: revision instructions are only valid for REQUEST_REVISION", ErrInvalid)
	}
	return nil
}

func sameDecision(item Decision, input CreateInput) bool {
	checkSnapshotMatches := input.CheckStatus == "" || item.CheckStatus == "" ||
		(item.CheckStatus == input.CheckStatus && item.FailedSteps == input.FailedSteps)
	return item.WorkflowJobID == input.WorkflowJobID &&
		item.ReviewRunID == input.ReviewRunID &&
		item.ExecutionID == input.ExecutionID &&
		item.ExecutionVersion == input.ExecutionVersion &&
		item.CheckpointID == input.CheckpointID &&
		item.BaseCommitSHA == input.BaseCommitSHA &&
		item.PatchChecksum == input.PatchChecksum &&
		item.Kind == input.Kind &&
		item.Note == input.Note &&
		item.RevisionInstructions == input.RevisionInstructions &&
		item.ReviewOverride == input.ReviewOverride &&
		item.ReviewerVerdict == input.ReviewerVerdict &&
		checkSnapshotMatches &&
		item.WorkflowVersionBefore == input.WorkflowVersionBefore &&
		item.RevisionCountBefore == input.RevisionCountBefore
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate approval decision ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}
