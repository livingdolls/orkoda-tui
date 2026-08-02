package checks

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("check run not found")
	ErrInvalid  = errors.New("invalid check run")
)

type Status string

const (
	StatusPending   Status = "PENDING"
	StatusRunning   Status = "RUNNING"
	StatusPassed    Status = "PASSED"
	StatusFailed    Status = "FAILED"
	StatusCancelled Status = "CANCELLED"
)

type Run struct {
	ID               string     `json:"id"`
	WorkflowJobID    string     `json:"workflow_job_id"`
	ExecutionID      string     `json:"execution_id"`
	ExecutionVersion int        `json:"execution_version"`
	WorkspaceID      string     `json:"workspace_id"`
	Status           Status     `json:"status"`
	TotalSteps       int        `json:"total_steps"`
	PassedSteps      int        `json:"passed_steps"`
	FailedSteps      int        `json:"failed_steps"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Step struct {
	ID              string          `json:"id"`
	CheckRunID      string          `json:"check_run_id"`
	Sequence        int             `json:"sequence"`
	Profile         string          `json:"profile"`
	CommandJSON     json.RawMessage `json:"command"`
	Status          Status          `json:"status"`
	ExitCode        *int            `json:"exit_code,omitempty"`
	DurationMS      int64           `json:"duration_ms"`
	OutputText      string          `json:"output_text"`
	OutputTruncated bool            `json:"output_truncated"`
	ErrorMessage    string          `json:"error_message,omitempty"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
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

func (r *Repository) CreateOrGet(
	ctx context.Context,
	workflowID string,
	executionID string,
	workspaceID string,
	executionVersion int,
) (Run, bool, error) {
	workflowID = strings.TrimSpace(workflowID)
	executionID = strings.TrimSpace(executionID)
	workspaceID = strings.TrimSpace(workspaceID)
	if workflowID == "" || executionID == "" || workspaceID == "" || executionVersion < 1 {
		return Run{}, false, ErrInvalid
	}
	if item, err := r.GetByVersion(ctx, workflowID, executionVersion); err == nil {
		return item, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Run{}, false, err
	}

	now := r.now().UTC()
	item := Run{
		ID: newID(), WorkflowJobID: workflowID, ExecutionID: executionID,
		ExecutionVersion: executionVersion, WorkspaceID: workspaceID,
		Status: StatusPending, CreatedAt: now, UpdatedAt: now,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO check_runs (
			id, workflow_job_id, execution_id, execution_version,
			workspace_id, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 'PENDING', ?, ?)
	`, item.ID, workflowID, executionID, executionVersion, workspaceID,
		now.UnixMilli(), now.UnixMilli())
	if err != nil {
		if existing, getErr := r.GetByVersion(ctx, workflowID, executionVersion); getErr == nil {
			return existing, false, nil
		}
		return Run{}, false, err
	}
	return item, true, nil
}

func (r *Repository) Get(ctx context.Context, id string) (Run, error) {
	return scanRun(r.db.QueryRowContext(
		ctx, `SELECT `+runColumns+` FROM check_runs WHERE id=?`, strings.TrimSpace(id),
	))
}

func (r *Repository) GetByVersion(ctx context.Context, workflowID string, version int) (Run, error) {
	return scanRun(r.db.QueryRowContext(
		ctx,
		`SELECT `+runColumns+` FROM check_runs WHERE workflow_job_id=? AND execution_version=?`,
		strings.TrimSpace(workflowID), version,
	))
}

func (r *Repository) ListWorkflow(ctx context.Context, workflowID string) ([]Run, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT `+runColumns+` FROM check_runs WHERE workflow_job_id=? ORDER BY execution_version DESC`,
		strings.TrimSpace(workflowID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Run, 0)
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Start(ctx context.Context, id string, profiles []Profile) (Run, error) {
	now := r.now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE check_runs
		SET status='RUNNING', total_steps=?, started_at=COALESCE(started_at,?),
			completed_at=NULL, updated_at=?
		WHERE id=? AND status IN ('PENDING','RUNNING')
	`, len(profiles), now.UnixMilli(), now.UnixMilli(), id); err != nil {
		return Run{}, err
	}
	for index, profile := range profiles {
		commandJSON, err := json.Marshal(profile.Command)
		if err != nil {
			return Run{}, err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO check_steps (
				id, check_run_id, sequence, profile, command_json,
				status, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, 'PENDING', ?, ?)
		`, newID(), id, index+1, profile.Name, string(commandJSON),
			now.UnixMilli(), now.UnixMilli())
		if err != nil {
			return Run{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Run{}, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) StartStep(ctx context.Context, runID, profile string) (Step, error) {
	now := r.now().UTC()
	row := r.db.QueryRowContext(ctx, `
		UPDATE check_steps
		SET status='RUNNING', exit_code=NULL, duration_ms=0,
			output_text='', output_truncated=0, error_message=NULL,
			started_at=?, completed_at=NULL, updated_at=?
		WHERE check_run_id=? AND profile=?
			AND status IN ('PENDING','RUNNING','FAILED','CANCELLED')
		RETURNING `+stepColumns,
		now.UnixMilli(), now.UnixMilli(), runID, profile,
	)
	return scanStep(row)
}

func (r *Repository) CompleteStep(ctx context.Context, stepID string, result Result) error {
	now := r.now().UTC()
	status := StatusPassed
	if !result.Passed {
		status = StatusFailed
	}
	message := bound(result.ErrorMessage, 1024)
	_, err := r.db.ExecContext(ctx, `
		UPDATE check_steps
		SET status=?, exit_code=?, duration_ms=?, output_text=?,
			output_truncated=?, error_message=?, completed_at=?, updated_at=?
		WHERE id=?
	`, status, result.ExitCode, result.Duration.Milliseconds(),
		bound(result.Output, result.OutputLimit), boolInt(result.Truncated), nullable(message),
		now.UnixMilli(), now.UnixMilli(), stepID)
	return err
}

func (r *Repository) Finish(ctx context.Context, runID string) (Run, error) {
	now := r.now().UTC()
	row := r.db.QueryRowContext(ctx, `
		UPDATE check_runs
		SET status=CASE
				WHEN EXISTS(
					SELECT 1 FROM check_steps
					WHERE check_run_id=? AND status!='PASSED'
				) THEN 'FAILED'
				ELSE 'PASSED'
			END,
			passed_steps=(SELECT COUNT(*) FROM check_steps WHERE check_run_id=? AND status='PASSED'),
			failed_steps=(SELECT COUNT(*) FROM check_steps WHERE check_run_id=? AND status!='PASSED'),
			completed_at=?, updated_at=?
		WHERE id=?
		RETURNING `+runColumns,
		runID, runID, runID, now.UnixMilli(), now.UnixMilli(), runID,
	)
	return scanRun(row)
}

func (r *Repository) ListSteps(ctx context.Context, runID string) ([]Step, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT `+stepColumns+` FROM check_steps WHERE check_run_id=? ORDER BY sequence`,
		strings.TrimSpace(runID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Step, 0)
	for rows.Next() {
		item, err := scanStep(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const runColumns = `id,workflow_job_id,execution_id,execution_version,workspace_id,status,total_steps,passed_steps,failed_steps,started_at,completed_at,created_at,updated_at`
const stepColumns = `id,check_run_id,sequence,profile,command_json,status,exit_code,duration_ms,output_text,output_truncated,error_message,started_at,completed_at,created_at,updated_at`

func scanRun(row interface{ Scan(...any) error }) (Run, error) {
	var item Run
	var started, completed sql.NullInt64
	var created, updated int64
	if err := row.Scan(
		&item.ID, &item.WorkflowJobID, &item.ExecutionID, &item.ExecutionVersion,
		&item.WorkspaceID, &item.Status, &item.TotalSteps, &item.PassedSteps,
		&item.FailedSteps, &started, &completed, &created, &updated,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, err
	}
	item.CreatedAt = time.UnixMilli(created).UTC()
	item.UpdatedAt = time.UnixMilli(updated).UTC()
	if started.Valid {
		value := time.UnixMilli(started.Int64).UTC()
		item.StartedAt = &value
	}
	if completed.Valid {
		value := time.UnixMilli(completed.Int64).UTC()
		item.CompletedAt = &value
	}
	return item, nil
}

func scanStep(row interface{ Scan(...any) error }) (Step, error) {
	var item Step
	var command string
	var exit sql.NullInt64
	var message sql.NullString
	var started, completed sql.NullInt64
	var created, updated int64
	var truncated int
	if err := row.Scan(
		&item.ID, &item.CheckRunID, &item.Sequence, &item.Profile, &command,
		&item.Status, &exit, &item.DurationMS, &item.OutputText, &truncated,
		&message, &started, &completed, &created, &updated,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Step{}, ErrNotFound
		}
		return Step{}, err
	}
	item.CommandJSON = json.RawMessage(command)
	if exit.Valid {
		value := int(exit.Int64)
		item.ExitCode = &value
	}
	item.OutputTruncated = truncated == 1
	item.ErrorMessage = message.String
	item.CreatedAt = time.UnixMilli(created).UTC()
	item.UpdatedAt = time.UnixMilli(updated).UTC()
	if started.Valid {
		value := time.UnixMilli(started.Int64).UTC()
		item.StartedAt = &value
	}
	if completed.Valid {
		value := time.UnixMilli(completed.Int64).UTC()
		item.CompletedAt = &value
	}
	return item, nil
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func bound(value string, limit int) string {
	if limit > 0 && len(value) > limit {
		return value[:limit]
	}
	return value
}
