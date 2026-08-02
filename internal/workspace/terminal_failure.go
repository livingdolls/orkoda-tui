package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

// HandleDurable wraps workspace preparation with terminal-attempt workflow
// failure persistence. Earlier attempts remain in WORKSPACE_PREPARING so the
// durable queue can retry them.
func (h *PrepareHandler) HandleDurable(ctx context.Context, queueJob jobqueue.Job) error {
	handlerErr := h.Handle(ctx, queueJob)
	if handlerErr == nil || ctx.Err() != nil || queueJob.MaxAttempts < 1 || queueJob.Attempts < queueJob.MaxAttempts {
		return handlerErr
	}

	payload, decodeErr := decodePreparePayload(queueJob.PayloadJSON)
	if decodeErr != nil {
		return handlerErr
	}
	workflow, loadErr := h.workflows.Get(ctx, payload.WorkflowJobID)
	if loadErr != nil {
		return handlerErr
	}
	if workflow.Status != workflowjob.StatusWorkspacePreparing || workflow.Version != payload.WorkflowVersion {
		return handlerErr
	}

	message := boundedFailureMessage(handlerErr)
	_, transitionErr := h.workflows.Transition(ctx, workflow.ID, workflowjob.TransitionInput{
		ExpectedVersion: workflow.Version,
		Action:          workflowjob.ActionFail,
		FailureCode:     "WORKSPACE_PREPARATION_FAILED",
		FailureMessage:  message,
		Details: map[string]any{
			"queue_job_id": queueJob.ID,
			"attempt":      queueJob.Attempts,
			"max_attempts": queueJob.MaxAttempts,
		},
	})
	if transitionErr != nil && !errors.Is(transitionErr, workflowjob.ErrVersionConflict) &&
		!errors.Is(transitionErr, workflowjob.ErrInvalidTransition) {
		return fmt.Errorf("%v; persist terminal workflow failure: %w", handlerErr, transitionErr)
	}
	return handlerErr
}

func boundedFailureMessage(err error) string {
	message := "workspace preparation failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = strings.TrimSpace(err.Error())
	}
	const maximum = 2048
	if len(message) > maximum {
		message = message[:maximum]
	}
	return message
}
