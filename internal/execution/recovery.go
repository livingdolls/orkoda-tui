package execution

import (
	"context"
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
