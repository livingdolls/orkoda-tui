package reviewer

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

	"github.com/livingdolls/orkoda-tui/internal/llm"
)

var (
	ErrNotFound         = errors.New("review run not found")
	ErrInvalid          = errors.New("invalid review run")
	ErrSnapshotConflict = errors.New("review snapshot conflicts with persisted run")
)

type Status string

type Verdict string

type Severity string

type Category string

const (
	StatusPending   Status = "PENDING"
	StatusRunning   Status = "RUNNING"
	StatusCompleted Status = "COMPLETED"
	StatusFailed    Status = "FAILED"
	StatusCancelled Status = "CANCELLED"

	VerdictApprove         Verdict = "APPROVE"
	VerdictRequestRevision Verdict = "REQUEST_REVISION"

	SeverityCritical Severity = "CRITICAL"
	SeverityHigh     Severity = "HIGH"
	SeverityMedium   Severity = "MEDIUM"
	SeverityLow      Severity = "LOW"

	CategoryCorrectness     Category = "CORRECTNESS"
	CategorySecurity        Category = "SECURITY"
	CategoryReliability     Category = "RELIABILITY"
	CategoryPerformance     Category = "PERFORMANCE"
	CategoryMaintainability Category = "MAINTAINABILITY"
	CategoryTesting         Category = "TESTING"
	CategoryRequirement     Category = "REQUIREMENT"
)

type Run struct {
	ID                   string     `json:"id"`
	WorkflowJobID        string     `json:"workflow_job_id"`
	ExecutionID          string     `json:"execution_id"`
	ExecutionVersion     int        `json:"execution_version"`
	CheckRunID           string     `json:"check_run_id"`
	CheckpointID         string     `json:"checkpoint_id"`
	AgentSettingsVersion int        `json:"agent_settings_version"`
	Provider             string     `json:"provider"`
	Model                string     `json:"model"`
	Status               Status     `json:"status"`
	Verdict              Verdict    `json:"verdict,omitempty"`
	Summary              string     `json:"summary"`
	TotalIssues          int        `json:"total_issues"`
	BlockingIssues       int        `json:"blocking_issues"`
	Usage                llm.Usage  `json:"usage"`
	FailureCode          string     `json:"failure_code,omitempty"`
	FailureMessage       string     `json:"failure_message,omitempty"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type Issue struct {
	ID           string    `json:"id"`
	ReviewRunID  string    `json:"review_run_id"`
	Position     int       `json:"position"`
	Key          string    `json:"key"`
	Severity     Severity  `json:"severity"`
	Category     Category  `json:"category"`
	Blocking     bool      `json:"blocking"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	FilePath     string    `json:"file_path,omitempty"`
	LineStart    int       `json:"line_start,omitempty"`
	LineEnd      int       `json:"line_end,omitempty"`
	CriteriaRefs []string  `json:"criteria_refs"`
	CreatedAt    time.Time `json:"created_at"`
}

type Result struct {
	Verdict Verdict `json:"verdict"`
	Summary string  `json:"summary"`
	Issues  []Issue `json:"issues"`
}

type CreateInput struct {
	WorkflowJobID        string
	ExecutionID          string
	ExecutionVersion     int
	CheckRunID           string
	CheckpointID         string
	AgentSettingsVersion int
	Provider             string
	Model                string
}

type Store interface {
	CreateOrGet(context.Context, CreateInput) (Run, bool, error)
	Get(context.Context, string) (Run, error)
	GetByVersion(context.Context, string, int) (Run, error)
	ListWorkflow(context.Context, string) ([]Run, error)
	ListIssues(context.Context, string) ([]Issue, error)
	Start(context.Context, string) (Run, error)
	Complete(context.Context, string, Result, llm.Usage) (Run, error)
	Fail(context.Context, string, string, string, bool) error
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

func (r *Repository) CreateOrGet(ctx context.Context, input CreateInput) (Run, bool, error) {
	input = normalizeCreateInput(input)
	if err := validateCreateInput(input); err != nil {
		return Run{}, false, err
	}
	if existing, err := r.GetByVersion(ctx, input.WorkflowJobID, input.ExecutionVersion); err == nil {
		if !sameSnapshot(existing, input) {
			return Run{}, false, ErrSnapshotConflict
		}
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Run{}, false, err
	}

	now := r.now().UTC()
	item := Run{
		ID:                   newID(),
		WorkflowJobID:        input.WorkflowJobID,
		ExecutionID:          input.ExecutionID,
		ExecutionVersion:     input.ExecutionVersion,
		CheckRunID:           input.CheckRunID,
		CheckpointID:         input.CheckpointID,
		AgentSettingsVersion: input.AgentSettingsVersion,
		Provider:             input.Provider,
		Model:                input.Model,
		Status:               StatusPending,
		Summary:              "",
		Usage:                llm.Usage{},
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO review_runs (
			id, workflow_job_id, execution_id, execution_version,
			check_run_id, checkpoint_id, agent_settings_version,
			provider, model, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', ?, ?)
	`, item.ID, item.WorkflowJobID, item.ExecutionID, item.ExecutionVersion,
		item.CheckRunID, item.CheckpointID, item.AgentSettingsVersion,
		item.Provider, item.Model, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		if existing, getErr := r.GetByVersion(ctx, input.WorkflowJobID, input.ExecutionVersion); getErr == nil {
			if !sameSnapshot(existing, input) {
				return Run{}, false, ErrSnapshotConflict
			}
			return existing, false, nil
		}
		return Run{}, false, fmt.Errorf("insert review run: %w", err)
	}
	return item, true, nil
}

func (r *Repository) Get(ctx context.Context, id string) (Run, error) {
	return scanRun(r.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM review_runs WHERE id = ?`, strings.TrimSpace(id)))
}

func (r *Repository) GetByVersion(ctx context.Context, workflowID string, executionVersion int) (Run, error) {
	return scanRun(r.db.QueryRowContext(ctx, `
		SELECT `+runColumns+` FROM review_runs
		WHERE workflow_job_id = ? AND execution_version = ?
	`, strings.TrimSpace(workflowID), executionVersion))
}

func (r *Repository) ListWorkflow(ctx context.Context, workflowID string) ([]Run, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+runColumns+` FROM review_runs
		WHERE workflow_job_id = ? ORDER BY execution_version DESC
	`, strings.TrimSpace(workflowID))
	if err != nil {
		return nil, fmt.Errorf("list review runs: %w", err)
	}
	defer rows.Close()
	items := make([]Run, 0)
	for rows.Next() {
		item, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListIssues(ctx context.Context, reviewRunID string) ([]Issue, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, review_run_id, position, issue_key, severity, category,
			blocking, title, description, COALESCE(file_path, ''), line_start,
			line_end, criteria_refs_json, created_at
		FROM review_issues WHERE review_run_id = ? ORDER BY position
	`, strings.TrimSpace(reviewRunID))
	if err != nil {
		return nil, fmt.Errorf("list review issues: %w", err)
	}
	defer rows.Close()
	issues := make([]Issue, 0)
	for rows.Next() {
		var issue Issue
		var blocking int
		var criteriaJSON string
		var createdAt int64
		if err := rows.Scan(
			&issue.ID, &issue.ReviewRunID, &issue.Position, &issue.Key,
			&issue.Severity, &issue.Category, &blocking, &issue.Title,
			&issue.Description, &issue.FilePath, &issue.LineStart, &issue.LineEnd,
			&criteriaJSON, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan review issue: %w", err)
		}
		issue.Blocking = blocking == 1
		issue.CreatedAt = time.UnixMilli(createdAt).UTC()
		if err := json.Unmarshal([]byte(criteriaJSON), &issue.CriteriaRefs); err != nil {
			return nil, fmt.Errorf("decode review issue criteria: %w", err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

func (r *Repository) Start(ctx context.Context, id string) (Run, error) {
	now := r.now().UTC()
	row := r.db.QueryRowContext(ctx, `
		UPDATE review_runs
		SET status = 'RUNNING', started_at = COALESCE(started_at, ?),
			completed_at = NULL, failure_code = NULL, failure_message = NULL,
			updated_at = ?
		WHERE id = ? AND status IN ('PENDING','RUNNING','FAILED','CANCELLED')
		RETURNING `+runColumns,
		now.UnixMilli(), now.UnixMilli(), strings.TrimSpace(id),
	)
	return scanRun(row)
}

func (r *Repository) Complete(ctx context.Context, id string, result Result, usage llm.Usage) (Run, error) {
	resultJSON, err := json.Marshal(usage)
	if err != nil {
		return Run{}, fmt.Errorf("marshal review usage: %w", err)
	}
	blocking := 0
	for _, issue := range result.Issues {
		if issue.Blocking {
			blocking++
		}
	}
	now := r.now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, fmt.Errorf("begin review completion: %w", err)
	}
	defer tx.Rollback()
	update, err := tx.ExecContext(ctx, `
		UPDATE review_runs
		SET status = 'COMPLETED', verdict = ?, summary = ?, total_issues = ?,
			blocking_issues = ?, usage_json = ?, failure_code = NULL,
			failure_message = NULL, completed_at = ?, updated_at = ?
		WHERE id = ? AND status = 'RUNNING'
	`, result.Verdict, result.Summary, len(result.Issues), blocking, string(resultJSON),
		now.UnixMilli(), now.UnixMilli(), strings.TrimSpace(id))
	if err != nil {
		return Run{}, fmt.Errorf("complete review run: %w", err)
	}
	if changed, err := update.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return Run{}, err
		}
		return Run{}, ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM review_issues WHERE review_run_id = ?`, id); err != nil {
		return Run{}, fmt.Errorf("replace review issues: %w", err)
	}
	for position, issue := range result.Issues {
		criteriaJSON, err := json.Marshal(issue.CriteriaRefs)
		if err != nil {
			return Run{}, fmt.Errorf("marshal review issue criteria: %w", err)
		}
		var filePath any
		if strings.TrimSpace(issue.FilePath) != "" {
			filePath = issue.FilePath
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO review_issues (
				id, review_run_id, position, issue_key, severity, category,
				blocking, title, description, file_path, line_start, line_end,
				criteria_refs_json, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, newID(), id, position, issue.Key, issue.Severity, issue.Category,
			boolInt(issue.Blocking), issue.Title, issue.Description, filePath,
			issue.LineStart, issue.LineEnd, string(criteriaJSON), now.UnixMilli()); err != nil {
			return Run{}, fmt.Errorf("insert review issue: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("commit review completion: %w", err)
	}
	return r.Get(ctx, id)
}

func (r *Repository) Fail(ctx context.Context, id string, code string, message string, cancelled bool) error {
	status := StatusFailed
	if cancelled {
		status = StatusCancelled
	}
	message = bound(strings.TrimSpace(message), 2048)
	now := r.now().UTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE review_runs
		SET status = ?, failure_code = ?, failure_message = ?, completed_at = ?, updated_at = ?
		WHERE id = ? AND status IN ('PENDING','RUNNING','FAILED','CANCELLED')
	`, status, nullable(strings.TrimSpace(code)), nullable(message), now.UnixMilli(),
		now.UnixMilli(), strings.TrimSpace(id))
	return err
}

const runColumns = `id,workflow_job_id,execution_id,execution_version,check_run_id,checkpoint_id,agent_settings_version,provider,model,status,verdict,summary,total_issues,blocking_issues,usage_json,failure_code,failure_message,started_at,completed_at,created_at,updated_at`

func scanRun(row interface{ Scan(...any) error }) (Run, error) {
	var item Run
	var verdict, failureCode, failureMessage sql.NullString
	var usageJSON string
	var startedAt, completedAt sql.NullInt64
	var createdAt, updatedAt int64
	if err := row.Scan(
		&item.ID, &item.WorkflowJobID, &item.ExecutionID, &item.ExecutionVersion,
		&item.CheckRunID, &item.CheckpointID, &item.AgentSettingsVersion,
		&item.Provider, &item.Model, &item.Status, &verdict, &item.Summary,
		&item.TotalIssues, &item.BlockingIssues, &usageJSON, &failureCode,
		&failureMessage, &startedAt, &completedAt, &createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, fmt.Errorf("scan review run: %w", err)
	}
	item.Verdict = Verdict(verdict.String)
	item.FailureCode = failureCode.String
	item.FailureMessage = failureMessage.String
	item.CreatedAt = time.UnixMilli(createdAt).UTC()
	item.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	if strings.TrimSpace(usageJSON) != "" {
		if err := json.Unmarshal([]byte(usageJSON), &item.Usage); err != nil {
			return Run{}, fmt.Errorf("decode review usage: %w", err)
		}
	}
	if startedAt.Valid {
		value := time.UnixMilli(startedAt.Int64).UTC()
		item.StartedAt = &value
	}
	if completedAt.Valid {
		value := time.UnixMilli(completedAt.Int64).UTC()
		item.CompletedAt = &value
	}
	return item, nil
}

func normalizeCreateInput(input CreateInput) CreateInput {
	input.WorkflowJobID = strings.TrimSpace(input.WorkflowJobID)
	input.ExecutionID = strings.TrimSpace(input.ExecutionID)
	input.CheckRunID = strings.TrimSpace(input.CheckRunID)
	input.CheckpointID = strings.TrimSpace(input.CheckpointID)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Model = strings.TrimSpace(input.Model)
	return input
}

func validateCreateInput(input CreateInput) error {
	if input.WorkflowJobID == "" || input.ExecutionID == "" || input.CheckRunID == "" ||
		input.CheckpointID == "" || input.ExecutionVersion < 1 ||
		input.AgentSettingsVersion < 1 || input.Provider == "" || input.Model == "" {
		return fmt.Errorf("%w: complete review snapshot is required", ErrInvalid)
	}
	return nil
}

func sameSnapshot(item Run, input CreateInput) bool {
	return item.WorkflowJobID == input.WorkflowJobID && item.ExecutionID == input.ExecutionID &&
		item.ExecutionVersion == input.ExecutionVersion && item.CheckRunID == input.CheckRunID &&
		item.CheckpointID == input.CheckpointID && item.AgentSettingsVersion == input.AgentSettingsVersion &&
		item.Provider == input.Provider && item.Model == input.Model
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate review ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func bound(value string, limit int) string {
	if limit > 0 && len(value) > limit {
		return value[:limit]
	}
	return value
}
