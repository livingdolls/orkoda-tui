package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type failedWorkflowStore interface {
	Get(context.Context, string) (workflowjob.Job, error)
	Transition(context.Context, string, workflowjob.TransitionInput) (workflowjob.Job, error)
}

type failedExecutionCandidate struct {
	executionID string
	workflowID  string
	code        string
	message     string
}

// ReconcileFailedWorkflows repairs workflows left EXECUTING by older daemons
// after their current durable execution had already reached FAILED.
func (r *Repository) ReconcileFailedWorkflows(
	ctx context.Context,
	workflows failedWorkflowStore,
) (int, error) {
	if workflows == nil {
		return 0, fmt.Errorf("workflow store is required")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.id, e.workflow_job_id,
			COALESCE(NULLIF(TRIM(e.failure_code), ''), 'EXECUTOR_FAILED'),
			COALESCE(NULLIF(TRIM(e.failure_message), ''), 'Executor execution failed.')
		FROM executions e
		JOIN workflow_jobs w ON w.id = e.workflow_job_id
		WHERE e.status = 'FAILED'
			AND w.status = 'EXECUTING'
			AND e.execution_version = w.execution_version
		ORDER BY e.updated_at ASC
	`)
	if err != nil {
		return 0, fmt.Errorf("list failed Executor workflows: %w", err)
	}
	candidates := make([]failedExecutionCandidate, 0)
	for rows.Next() {
		var candidate failedExecutionCandidate
		if err := rows.Scan(
			&candidate.executionID,
			&candidate.workflowID,
			&candidate.code,
			&candidate.message,
		); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan failed Executor workflow: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close failed Executor workflow rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate failed Executor workflows: %w", err)
	}

	recovered := 0
	for _, candidate := range candidates {
		current, err := workflows.Get(ctx, candidate.workflowID)
		if err != nil {
			return recovered, fmt.Errorf("load failed Executor workflow %s: %w", candidate.workflowID, err)
		}
		if current.Status != workflowjob.StatusExecuting {
			continue
		}
		code := strings.TrimSpace(candidate.code)
		if code == "" {
			code = "EXECUTOR_FAILED"
		}
		message := strings.TrimSpace(candidate.message)
		if message == "" {
			message = "Executor execution failed."
		}
		_, err = workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
			ExpectedVersion: current.Version,
			Action:          workflowjob.ActionFail,
			FailureCode:     code,
			FailureMessage:  message,
			Details: map[string]any{
				"recovered":    true,
				"execution_id": candidate.executionID,
			},
		})
		if err != nil {
			if errors.Is(err, workflowjob.ErrVersionConflict) ||
				errors.Is(err, workflowjob.ErrInvalidTransition) {
				latest, getErr := workflows.Get(ctx, current.ID)
				if getErr == nil && latest.Status != workflowjob.StatusExecuting {
					continue
				}
			}
			return recovered, fmt.Errorf("recover failed Executor workflow %s: %w", current.ID, err)
		}
		recovered++
	}
	return recovered, nil
}

type deadExecutionDispatch struct {
	workflowID string
	dispatchID string
	status     string
	message    string
}

// ReconcileDeadExecutionDispatches repairs workflows whose workflow.execute
// queue job exhausted all retries before the handler could persist FAILED.
// It also handles legacy EXECUTING rows where current_dispatch_id was cleared
// by EXECUTION_STARTED; the dispatch ID remains in that transition's details.
func (r *Repository) ReconcileDeadExecutionDispatches(
	ctx context.Context,
	workflows failedWorkflowStore,
) (int, error) {
	if workflows == nil {
		return 0, fmt.Errorf("workflow store is required")
	}
	candidates := make(map[string]deadExecutionDispatch)

	rows, err := r.db.QueryContext(ctx, `
		SELECT w.id, j.id, j.status,
			COALESCE(
				NULLIF(TRIM(j.last_error), ''),
				CASE WHEN j.status = 'COMPLETED'
					THEN 'Executor dispatch completed without closing the workflow.'
					ELSE 'Executor dispatch exhausted all retries.' END
			)
		FROM workflow_jobs w
		JOIN jobs j ON j.id = w.current_dispatch_id
		WHERE w.status IN ('QUEUED', 'EXECUTING')
			AND j.type = 'workflow.execute'
			AND j.status IN ('DEAD', 'COMPLETED')
	`)
	if err != nil {
		return 0, fmt.Errorf("list dead current Executor dispatches: %w", err)
	}
	for rows.Next() {
		var candidate deadExecutionDispatch
		if err := rows.Scan(&candidate.workflowID, &candidate.dispatchID, &candidate.status, &candidate.message); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan dead current Executor dispatch: %w", err)
		}
		candidates[candidate.workflowID] = candidate
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close dead current Executor dispatch rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate dead current Executor dispatches: %w", err)
	}

	legacyRows, err := r.db.QueryContext(ctx, `
		SELECT w.id, t.details_json
		FROM workflow_jobs w
		JOIN workflow_job_transitions t
			ON t.workflow_job_id = w.id
			AND t.workflow_version = w.version
			AND t.action = 'EXECUTION_STARTED'
		WHERE w.status = 'EXECUTING'
			AND (w.current_dispatch_id IS NULL OR TRIM(w.current_dispatch_id) = '')
	`)
	if err != nil {
		return 0, fmt.Errorf("list legacy Executor dispatch transitions: %w", err)
	}
	legacyDispatches := make([]deadExecutionDispatch, 0)
	for legacyRows.Next() {
		var workflowID, detailsJSON string
		if err := legacyRows.Scan(&workflowID, &detailsJSON); err != nil {
			legacyRows.Close()
			return 0, fmt.Errorf("scan legacy Executor dispatch transition: %w", err)
		}
		var details struct {
			DispatchJobID string `json:"dispatch_job_id"`
		}
		if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
			legacyRows.Close()
			return 0, fmt.Errorf("decode legacy Executor dispatch transition: %w", err)
		}
		dispatchID := strings.TrimSpace(details.DispatchJobID)
		if dispatchID != "" {
			legacyDispatches = append(legacyDispatches, deadExecutionDispatch{
				workflowID: workflowID,
				dispatchID: dispatchID,
			})
		}
	}
	if err := legacyRows.Err(); err != nil {
		legacyRows.Close()
		return 0, fmt.Errorf("iterate legacy Executor dispatch transitions: %w", err)
	}
	if err := legacyRows.Close(); err != nil {
		return 0, fmt.Errorf("close legacy Executor dispatch rows: %w", err)
	}
	// database.Open intentionally uses a single SQLite connection. Finish the
	// transition query before looking up queue jobs to avoid self-deadlock.
	for _, legacy := range legacyDispatches {
		var jobType, status, message string
		err := r.db.QueryRowContext(ctx, `
			SELECT type, status,
				COALESCE(
					NULLIF(TRIM(last_error), ''),
					CASE WHEN status = 'COMPLETED'
						THEN 'Executor dispatch completed without closing the workflow.'
						ELSE 'Executor dispatch exhausted all retries.' END
				)
			FROM jobs WHERE id = ?
		`, legacy.dispatchID).Scan(&jobType, &status, &message)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("load legacy Executor dispatch %s: %w", legacy.dispatchID, err)
		}
		if jobType == "workflow.execute" && (status == "DEAD" || status == "COMPLETED") {
			legacy.status = status
			legacy.message = message
			candidates[legacy.workflowID] = legacy
		}
	}

	recovered := 0
	for _, candidate := range candidates {
		current, err := workflows.Get(ctx, candidate.workflowID)
		if err != nil {
			return recovered, fmt.Errorf("load workflow with dead Executor dispatch %s: %w", candidate.workflowID, err)
		}
		if current.Status != workflowjob.StatusExecuting && current.Status != workflowjob.StatusQueued {
			continue
		}
		_, err = workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
			ExpectedVersion: current.Version,
			Action:          workflowjob.ActionFail,
			FailureCode:     "EXECUTOR_FAILED",
			FailureMessage:  strings.TrimSpace(candidate.message),
			Details: map[string]any{
				"recovered":       true,
				"dispatch_job_id": candidate.dispatchID,
				"dispatch_status": candidate.status,
			},
		})
		if err != nil {
			if errors.Is(err, workflowjob.ErrVersionConflict) || errors.Is(err, workflowjob.ErrInvalidTransition) {
				latest, getErr := workflows.Get(ctx, current.ID)
				if getErr == nil && latest.Status != workflowjob.StatusExecuting && latest.Status != workflowjob.StatusQueued {
					continue
				}
			}
			return recovered, fmt.Errorf("recover dead Executor dispatch workflow %s: %w", current.ID, err)
		}
		recovered++
	}
	return recovered, nil
}
