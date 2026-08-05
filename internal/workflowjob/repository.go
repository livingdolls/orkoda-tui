package workflowjob

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
)

var (
	ErrNotFound          = errors.New("workflow job not found")
	ErrProjectNotFound   = errors.New("project not found")
	ErrPlanNotReady      = errors.New("plan is not ready for execution")
	ErrInvalidJob        = errors.New("invalid workflow job")
	ErrActiveJob         = errors.New("an active workflow job already exists")
	ErrVersionConflict   = errors.New("workflow job version conflict")
	ErrInvalidTransition = errors.New("invalid workflow transition")
	ErrRevisionLimit     = errors.New("workflow revision limit reached")
)

type Limits struct {
	MaxRevisions             int `json:"max_revisions"`
	MaxStageAttempts         int `json:"max_stage_attempts"`
	MaxExecutorTurns         int `json:"max_executor_turns"`
	MaxToolCalls             int `json:"max_tool_calls"`
	MaxConsecutiveToolErrors int `json:"max_consecutive_tool_errors"`
	MaxNoProgressTurns       int `json:"max_no_progress_turns"`
	WallClockSeconds         int `json:"wall_clock_seconds"`
}

type AgentSelection struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type Job struct {
	ID                    string         `json:"id"`
	ProjectID             string         `json:"project_id"`
	PlanID                string         `json:"plan_id"`
	PlanVersionID         string         `json:"plan_version_id"`
	RepositoryID          string         `json:"repository_id"`
	BaseBranch            string         `json:"base_branch"`
	BaseCommitSHA         string         `json:"base_commit_sha"`
	AgentSettingsVersion  int            `json:"agent_settings_version"`
	Executor              AgentSelection `json:"executor"`
	Reviewer              AgentSelection `json:"reviewer"`
	Status                Status         `json:"status"`
	Version               int            `json:"version"`
	CurrentDispatchID     string         `json:"current_dispatch_id,omitempty"`
	RetryStatus           Status         `json:"retry_status,omitempty"`
	ExecutionVersion      int            `json:"execution_version"`
	RevisionCount         int            `json:"revision_count"`
	Limits                Limits         `json:"limits"`
	CancellationRequested bool           `json:"cancellation_requested"`
	FailureCode           string         `json:"failure_code,omitempty"`
	FailureMessage        string         `json:"failure_message,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
	CompletedAt           *time.Time     `json:"completed_at,omitempty"`
}

type Transition struct {
	Sequence        int64           `json:"sequence"`
	WorkflowJobID   string          `json:"workflow_job_id"`
	FromStatus      Status          `json:"from_status,omitempty"`
	Action          Action          `json:"action"`
	ToStatus        Status          `json:"to_status"`
	WorkflowVersion int             `json:"workflow_version"`
	DispatchJobID   string          `json:"dispatch_job_id,omitempty"`
	Details         json.RawMessage `json:"details"`
	CreatedAt       time.Time       `json:"created_at"`
}

type CreateInput struct {
	ProjectID            string         `json:"project_id"`
	PlanID               string         `json:"plan_id"`
	RepositoryID         string         `json:"repository_id"`
	BaseBranch           string         `json:"base_branch"`
	AgentSettingsVersion int            `json:"agent_settings_version"`
	Executor             AgentSelection `json:"executor"`
	Reviewer             AgentSelection `json:"reviewer"`
	Limits               Limits         `json:"limits"`
}

type TransitionInput struct {
	ExpectedVersion         int            `json:"expected_version"`
	Action                  Action         `json:"action"`
	FailureCode             string         `json:"failure_code"`
	FailureMessage          string         `json:"failure_message"`
	Details                 map[string]any `json:"details"`
	AdditionalExecutorTurns int            `json:"additional_executor_turns"`
}

type DurableQueue interface {
	EnqueueTx(context.Context, *sql.Tx, string, string, int, time.Time) (jobqueue.Job, error)
}

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type Repository struct {
	db       *sql.DB
	queue    DurableQueue
	recorder EventRecorder
}

func NewRepository(db *sql.DB, queue DurableQueue, recorder EventRecorder) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	if queue == nil {
		return nil, fmt.Errorf("durable queue is required")
	}
	return &Repository{db: db, queue: queue, recorder: recorder}, nil
}

func (r *Repository) Create(ctx context.Context, input CreateInput) (Job, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.PlanID = strings.TrimSpace(input.PlanID)
	input.RepositoryID = strings.TrimSpace(input.RepositoryID)
	input.BaseBranch = strings.TrimSpace(input.BaseBranch)
	input.Executor = normalizeAgentSelection(input.Executor)
	input.Reviewer = normalizeAgentSelection(input.Reviewer)
	input.Limits = normalizeLimits(input.Limits)
	if err := validateAgentSelections(input.AgentSettingsVersion, input.Executor, input.Reviewer); err != nil {
		return Job{}, err
	}
	if input.ProjectID == "" || input.PlanID == "" || input.RepositoryID == "" {
		return Job{}, fmt.Errorf("%w: project_id, plan_id, and repository_id are required", ErrInvalidJob)
	}
	if err := validateLimits(input.Limits); err != nil {
		return Job{}, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, fmt.Errorf("begin workflow job creation: %w", err)
	}
	defer tx.Rollback()

	var planStatus, planVersionID, currentBranch, headSHA string
	err = tx.QueryRowContext(ctx, `
		SELECT p.status, pv.id, r.current_branch, r.head_sha
		FROM plans p
		JOIN plan_versions pv ON pv.plan_id = p.id AND pv.version = p.current_version
		JOIN repositories r ON r.project_id = p.project_id AND r.id = ?
		WHERE p.id = ? AND p.project_id = ?
	`, input.RepositoryID, input.PlanID, input.ProjectID).Scan(
		&planStatus, &planVersionID, &currentBranch, &headSHA,
	)
	if errors.Is(err, sql.ErrNoRows) {
		var projectExists int
		if checkErr := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ?`, input.ProjectID).Scan(&projectExists); errors.Is(checkErr, sql.ErrNoRows) {
			return Job{}, ErrProjectNotFound
		}
		return Job{}, fmt.Errorf("%w: plan or repository does not belong to the project", ErrInvalidJob)
	}
	if err != nil {
		return Job{}, fmt.Errorf("resolve workflow job inputs: %w", err)
	}
	if planStatus != "READY" && planStatus != "APPROVED" {
		return Job{}, fmt.Errorf("%w: current plan status is %s", ErrPlanNotReady, planStatus)
	}
	if input.BaseBranch == "" {
		input.BaseBranch = currentBranch
	}
	if err := validateBaseBranch(input.BaseBranch); err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(headSHA) == "" {
		return Job{}, fmt.Errorf("%w: repository HEAD is required", ErrInvalidJob)
	}

	var activeID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM workflow_jobs
		WHERE plan_version_id = ? AND repository_id = ?
			AND status NOT IN ('COMPLETED', 'REJECTED', 'CANCELLED')
		LIMIT 1
	`, planVersionID, input.RepositoryID).Scan(&activeID)
	if err == nil {
		return Job{}, fmt.Errorf("%w: %s", ErrActiveJob, activeID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Job{}, fmt.Errorf("check active workflow job: %w", err)
	}

	now := time.Now().UTC()
	job := Job{
		ID:                   newID(),
		ProjectID:            input.ProjectID,
		PlanID:               input.PlanID,
		PlanVersionID:        planVersionID,
		RepositoryID:         input.RepositoryID,
		BaseBranch:           input.BaseBranch,
		BaseCommitSHA:        headSHA,
		AgentSettingsVersion: input.AgentSettingsVersion,
		Executor:             input.Executor,
		Reviewer:             input.Reviewer,
		Status:               StatusReady,
		Version:              1,
		Limits:               input.Limits,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workflow_jobs (
			id, project_id, plan_id, plan_version_id, repository_id,
			base_branch, base_commit_sha, agent_settings_version,
			executor_provider, executor_model, reviewer_provider, reviewer_model,
			status, version,
			max_revisions, max_stage_attempts, max_executor_turns, max_tool_calls,
			max_consecutive_tool_errors, max_no_progress_turns, wall_clock_seconds,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, job.ID, job.ProjectID, job.PlanID, job.PlanVersionID, job.RepositoryID,
		job.BaseBranch, job.BaseCommitSHA, job.AgentSettingsVersion,
		job.Executor.Provider, job.Executor.Model, job.Reviewer.Provider, job.Reviewer.Model,
		job.Status, job.Limits.MaxRevisions, job.Limits.MaxStageAttempts,
		job.Limits.MaxExecutorTurns, job.Limits.MaxToolCalls,
		job.Limits.MaxConsecutiveToolErrors, job.Limits.MaxNoProgressTurns,
		job.Limits.WallClockSeconds,
		now.UnixMilli(), now.UnixMilli()); err != nil {
		return Job{}, fmt.Errorf("insert workflow job: %w", err)
	}
	if err := insertTransition(ctx, tx, job.ID, "", ActionCreate, StatusReady, 1, "", nil, now); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, fmt.Errorf("commit workflow job creation: %w", err)
	}
	r.record(ctx, job.ID, "workflow.created", map[string]any{
		"project_id": job.ProjectID, "plan_id": job.PlanID,
		"plan_version_id": job.PlanVersionID, "repository_id": job.RepositoryID,
		"agent_settings_version": job.AgentSettingsVersion,
		"executor":               job.Executor, "reviewer": job.Reviewer,
		"status": job.Status, "version": job.Version,
	}, now)
	return job, nil
}

func (r *Repository) Get(ctx context.Context, jobID string) (Job, error) {
	return loadJob(ctx, r.db, strings.TrimSpace(jobID))
}

func (r *Repository) ListProject(ctx context.Context, projectID string) ([]Job, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+jobColumns+`
		FROM workflow_jobs WHERE project_id = ?
		ORDER BY updated_at DESC, created_at DESC
	`, strings.TrimSpace(projectID))
	if err != nil {
		return nil, fmt.Errorf("list workflow jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan workflow job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workflow jobs: %w", err)
	}
	return jobs, nil
}

func (r *Repository) Transition(ctx context.Context, jobID string, input TransitionInput) (Job, error) {
	jobID = strings.TrimSpace(jobID)
	input.Action = Action(strings.ToUpper(strings.TrimSpace(string(input.Action))))
	input.FailureCode = strings.TrimSpace(input.FailureCode)
	input.FailureMessage = strings.TrimSpace(input.FailureMessage)
	if jobID == "" || input.ExpectedVersion < 1 || input.Action == "" {
		return Job{}, fmt.Errorf("%w: job ID, expected_version, and action are required", ErrInvalidJob)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, fmt.Errorf("begin workflow transition: %w", err)
	}
	defer tx.Rollback()

	job, err := loadJob(ctx, tx, jobID)
	if err != nil {
		return Job{}, err
	}
	if job.Version != input.ExpectedVersion {
		return Job{}, fmt.Errorf("%w: expected %d, current %d", ErrVersionConflict, input.ExpectedVersion, job.Version)
	}

	if input.Action == ActionContinueExecution {
		if !isExecutorPauseCode(job.FailureCode) || (job.RetryStatus != StatusExecuting && job.RetryStatus != StatusQueued) {
			return Job{}, invalidTransition(job.Status, input.Action)
		}
		if input.AdditionalExecutorTurns < 1 || input.AdditionalExecutorTurns > 64 || job.Limits.MaxExecutorTurns+input.AdditionalExecutorTurns > 128 {
			return Job{}, fmt.Errorf("%w: additional_executor_turns must keep max_executor_turns between 1 and 128", ErrInvalidJob)
		}
	}

	next, err := nextStatus(job.Status, input.Action, job.RetryStatus)
	if err != nil {
		return Job{}, err
	}
	if input.Action == ActionQueueRevision && job.RevisionCount >= job.Limits.MaxRevisions {
		return Job{}, fmt.Errorf("%w: maximum is %d", ErrRevisionLimit, job.Limits.MaxRevisions)
	}
	if input.Action == ActionFail && input.FailureMessage == "" {
		return Job{}, fmt.Errorf("%w: failure_message is required for FAIL", ErrInvalidJob)
	}

	detailsJSON, err := safeDetails(input.Details)
	if err != nil {
		return Job{}, err
	}
	now := time.Now().UTC()
	nextVersion := job.Version + 1
	dispatchID := ""
	dispatchType, shouldDispatch := dispatchFor(input.Action)
	if input.Action == ActionRetry {
		dispatchType, shouldDispatch = dispatchForRetry(next)
	}
	if shouldDispatch {
		payload, err := json.Marshal(map[string]any{
			"workflow_job_id":  job.ID,
			"workflow_version": nextVersion,
			"action":           input.Action,
			"target_status":    next,
		})
		if err != nil {
			return Job{}, fmt.Errorf("marshal workflow dispatch: %w", err)
		}
		dispatch, err := r.queue.EnqueueTx(
			ctx, tx, dispatchType, string(payload), job.Limits.MaxStageAttempts, now,
		)
		if err != nil {
			return Job{}, fmt.Errorf("enqueue workflow dispatch: %w", err)
		}
		dispatchID = dispatch.ID
	}
	// EXECUTION_STARTED is handled by the workflow.execute job that was
	// already recorded while the workflow was QUEUED. Keep that dispatch ID
	// attached to the workflow until the stage finishes so stale jobs and dead
	// dispatches can be identified reliably.
	if input.Action == ActionExecutionStarted && dispatchID == "" {
		dispatchID = job.CurrentDispatchID
	}

	retryStatus := job.RetryStatus
	failureCode := job.FailureCode
	failureMessage := job.FailureMessage
	cancellationRequested := job.CancellationRequested
	revisionCount := job.RevisionCount
	executionVersion := job.ExecutionVersion
	maxExecutorTurns := job.Limits.MaxExecutorTurns
	var completedAt *time.Time
	if input.Action == ActionFail {
		retryStatus = job.Status
		failureCode = input.FailureCode
		if failureCode == "" {
			failureCode = "STAGE_FAILED"
		}
		failureMessage = input.FailureMessage
	}
	if input.Action == ActionRetry || input.Action == ActionRestart {
		retryStatus = ""
		failureCode = ""
		failureMessage = ""
	}
	if input.Action == ActionRestart {
		cancellationRequested = false
		revisionCount = 0
	}
	if input.Action == ActionContinueExecution {
		maxExecutorTurns += input.AdditionalExecutorTurns
		retryStatus = ""
		failureCode = ""
		failureMessage = ""
	}
	if input.Action == ActionCancel {
		cancellationRequested = true
	}
	if input.Action == ActionQueueRevision {
		revisionCount++
	}
	if input.Action == ActionExecutionStarted {
		executionVersion++
	}
	if isTerminal(next) {
		value := now
		completedAt = &value
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE workflow_jobs
		SET status = ?, version = ?, current_dispatch_id = ?, retry_status = ?,
			execution_version = ?, revision_count = ?, max_executor_turns = ?, cancellation_requested = ?,
			failure_code = ?, failure_message = ?, updated_at = ?, completed_at = ?
		WHERE id = ? AND version = ?
	`, next, nextVersion, nullableString(dispatchID), nullableStatus(retryStatus),
		executionVersion, revisionCount, maxExecutorTurns, boolInteger(cancellationRequested),
		nullableString(failureCode), nullableString(failureMessage), now.UnixMilli(),
		nullableTime(completedAt), job.ID, job.Version)
	if err != nil {
		return Job{}, fmt.Errorf("update workflow job: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return Job{}, fmt.Errorf("read workflow transition rows: %w", err)
	} else if affected != 1 {
		return Job{}, ErrVersionConflict
	}
	if err := insertTransition(
		ctx, tx, job.ID, job.Status, input.Action, next, nextVersion,
		dispatchID, detailsJSON, now,
	); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, fmt.Errorf("commit workflow transition: %w", err)
	}

	r.record(ctx, job.ID, "workflow.transitioned", map[string]any{
		"project_id": job.ProjectID, "from_status": job.Status,
		"to_status": next, "action": input.Action, "version": nextVersion,
		"dispatch_job_id": dispatchID,
	}, now)
	return r.Get(ctx, job.ID)
}

func (r *Repository) ListTransitions(ctx context.Context, jobID string) ([]Transition, error) {
	if _, err := r.Get(ctx, jobID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT sequence, workflow_job_id, COALESCE(from_status, ''), action,
			to_status, workflow_version, COALESCE(dispatch_job_id, ''), details_json, created_at
		FROM workflow_job_transitions
		WHERE workflow_job_id = ? ORDER BY sequence ASC
	`, strings.TrimSpace(jobID))
	if err != nil {
		return nil, fmt.Errorf("list workflow transitions: %w", err)
	}
	defer rows.Close()

	transitions := make([]Transition, 0)
	for rows.Next() {
		var transition Transition
		var details string
		var createdAt int64
		if err := rows.Scan(
			&transition.Sequence, &transition.WorkflowJobID, &transition.FromStatus,
			&transition.Action, &transition.ToStatus, &transition.WorkflowVersion,
			&transition.DispatchJobID, &details, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan workflow transition: %w", err)
		}
		transition.Details = json.RawMessage(details)
		transition.CreatedAt = time.UnixMilli(createdAt).UTC()
		transitions = append(transitions, transition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workflow transitions: %w", err)
	}
	return transitions, nil
}

const jobColumns = `
	id, project_id, plan_id, plan_version_id, repository_id,
	base_branch, base_commit_sha, agent_settings_version,
	COALESCE(executor_provider, ''), COALESCE(executor_model, ''),
	COALESCE(reviewer_provider, ''), COALESCE(reviewer_model, ''),
	status, version,
	COALESCE(current_dispatch_id, ''), COALESCE(retry_status, ''),
	execution_version, revision_count, max_revisions, max_stage_attempts,
	max_executor_turns, max_tool_calls, max_consecutive_tool_errors,
	max_no_progress_turns, wall_clock_seconds, cancellation_requested,
	COALESCE(failure_code, ''), COALESCE(failure_message, ''),
	created_at, updated_at, completed_at`

func loadJob(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, jobID string) (Job, error) {
	job, err := scanJob(queryer.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM workflow_jobs WHERE id = ?`, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("read workflow job: %w", err)
	}
	return job, nil
}

func scanJob(scanner interface{ Scan(...any) error }) (Job, error) {
	var job Job
	var createdAt, updatedAt int64
	var completedAt sql.NullInt64
	var cancellationRequested int
	err := scanner.Scan(
		&job.ID, &job.ProjectID, &job.PlanID, &job.PlanVersionID, &job.RepositoryID,
		&job.BaseBranch, &job.BaseCommitSHA, &job.AgentSettingsVersion,
		&job.Executor.Provider, &job.Executor.Model,
		&job.Reviewer.Provider, &job.Reviewer.Model, &job.Status, &job.Version,
		&job.CurrentDispatchID, &job.RetryStatus, &job.ExecutionVersion, &job.RevisionCount,
		&job.Limits.MaxRevisions, &job.Limits.MaxStageAttempts,
		&job.Limits.MaxExecutorTurns, &job.Limits.MaxToolCalls,
		&job.Limits.MaxConsecutiveToolErrors, &job.Limits.MaxNoProgressTurns,
		&job.Limits.WallClockSeconds, &cancellationRequested, &job.FailureCode,
		&job.FailureMessage, &createdAt, &updatedAt, &completedAt,
	)
	if err != nil {
		return Job{}, err
	}
	job.CancellationRequested = cancellationRequested == 1
	job.CreatedAt = time.UnixMilli(createdAt).UTC()
	job.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	if completedAt.Valid {
		value := time.UnixMilli(completedAt.Int64).UTC()
		job.CompletedAt = &value
	}
	return job, nil
}

func insertTransition(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
	from Status,
	action Action,
	to Status,
	version int,
	dispatchID string,
	details []byte,
	createdAt time.Time,
) error {
	if len(details) == 0 {
		details = []byte("{}")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workflow_job_transitions (
			workflow_job_id, from_status, action, to_status, workflow_version,
			dispatch_job_id, details_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, jobID, nullableStatus(from), action, to, version, nullableString(dispatchID),
		string(details), createdAt.UnixMilli()); err != nil {
		return fmt.Errorf("insert workflow transition: %w", err)
	}
	return nil
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxRevisions == 0 {
		limits.MaxRevisions = 3
	}
	if limits.MaxStageAttempts == 0 {
		limits.MaxStageAttempts = 3
	}
	if limits.MaxExecutorTurns == 0 {
		limits.MaxExecutorTurns = 32
	}
	if limits.MaxToolCalls == 0 {
		limits.MaxToolCalls = 24
	}
	if limits.MaxConsecutiveToolErrors == 0 {
		limits.MaxConsecutiveToolErrors = 3
	}
	if limits.MaxNoProgressTurns == 0 {
		limits.MaxNoProgressTurns = 4
	}
	if limits.WallClockSeconds == 0 {
		limits.WallClockSeconds = 3600
	}
	return limits
}

func normalizeAgentSelection(selection AgentSelection) AgentSelection {
	selection.Provider = strings.ToLower(strings.TrimSpace(selection.Provider))
	selection.Model = strings.TrimSpace(selection.Model)
	return selection
}

func validateAgentSelections(settingsVersion int, executor, reviewer AgentSelection) error {
	if settingsVersion < 0 {
		return fmt.Errorf("%w: agent_settings_version must not be negative", ErrInvalidJob)
	}
	executorComplete := executor.Provider != "" && executor.Model != ""
	reviewerComplete := reviewer.Provider != "" && reviewer.Model != ""
	if (executor.Provider == "") != (executor.Model == "") ||
		(reviewer.Provider == "") != (reviewer.Model == "") {
		return fmt.Errorf("%w: each agent selection requires both provider and model", ErrInvalidJob)
	}
	if executorComplete != reviewerComplete {
		return fmt.Errorf("%w: executor and reviewer must both be selected or both omitted", ErrInvalidJob)
	}
	if executorComplete && executor.Provider == reviewer.Provider && executor.Model == reviewer.Model {
		return fmt.Errorf("%w: executor and reviewer must use different provider/model pairs", ErrInvalidJob)
	}
	return nil
}

func validateLimits(limits Limits) error {
	if limits.MaxRevisions < 0 || limits.MaxRevisions > 20 {
		return fmt.Errorf("%w: max_revisions must be between 0 and 20", ErrInvalidJob)
	}
	if limits.MaxStageAttempts < 1 || limits.MaxStageAttempts > 10 {
		return fmt.Errorf("%w: max_stage_attempts must be between 1 and 10", ErrInvalidJob)
	}
	if limits.MaxExecutorTurns < 2 || limits.MaxExecutorTurns > 128 {
		return fmt.Errorf("%w: max_executor_turns must be between 2 and 128", ErrInvalidJob)
	}
	if limits.MaxToolCalls < 1 || limits.MaxToolCalls > 1000 {
		return fmt.Errorf("%w: max_tool_calls must be between 1 and 1000", ErrInvalidJob)
	}
	if limits.MaxConsecutiveToolErrors < 1 || limits.MaxConsecutiveToolErrors > 10 {
		return fmt.Errorf("%w: max_consecutive_tool_errors must be between 1 and 10", ErrInvalidJob)
	}
	if limits.MaxNoProgressTurns < 1 || limits.MaxNoProgressTurns > 20 {
		return fmt.Errorf("%w: max_no_progress_turns must be between 1 and 20", ErrInvalidJob)
	}
	if limits.WallClockSeconds < 60 || limits.WallClockSeconds > 86400 {
		return fmt.Errorf("%w: wall_clock_seconds must be between 60 and 86400", ErrInvalidJob)
	}
	return nil
}

func validateBaseBranch(value string) error {
	invalid := value == "" || len(value) > 255 || strings.HasPrefix(value, "-") ||
		strings.Contains(value, "..") || strings.Contains(value, "@{") ||
		strings.ContainsAny(value, " ~^:?*[\\") ||
		strings.HasSuffix(value, ".") || strings.HasSuffix(value, "/")
	if !invalid {
		for _, character := range value {
			if character < 0x20 || character == 0x7f {
				invalid = true
				break
			}
		}
	}
	if invalid {
		return fmt.Errorf("%w: base_branch is not a safe Git branch name", ErrInvalidJob)
	}
	return nil
}

func safeDetails(details map[string]any) ([]byte, error) {
	if details == nil {
		return []byte("{}"), nil
	}
	value, err := json.Marshal(details)
	if err != nil {
		return nil, fmt.Errorf("%w: details must be JSON serializable", ErrInvalidJob)
	}
	if len(value) > 32*1024 {
		return nil, fmt.Errorf("%w: transition details exceed 32768 bytes", ErrInvalidJob)
	}
	return value, nil
}

func dispatchForRetry(status Status) (string, bool) {
	switch status {
	case StatusWorkspacePreparing:
		return "workflow.prepare_workspace", true
	case StatusQueued, StatusExecuting:
		return "workflow.execute", true
	case StatusChecking:
		return "workflow.run_checks", true
	case StatusReviewing:
		return "workflow.review", true
	case StatusPublishing:
		return "workflow.publish", true
	default:
		return "", false
	}
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableStatus(value Status) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().UnixMilli()
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate workflow job ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}

func (r *Repository) record(ctx context.Context, jobID, eventType string, payload any, createdAt time.Time) {
	if r.recorder == nil {
		return
	}
	if err := r.recorder.Record(ctx, jobID, eventType, payload, createdAt); err != nil {
		slog.Warn("record workflow activity", "job_id", jobID, "event_type", eventType, "error", err)
	}
}
