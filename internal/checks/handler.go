package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/gitstate"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
	"github.com/livingdolls/orkoda-tui/internal/workspace"
)

type WorkflowStore interface {
	Get(context.Context, string) (workflowjob.Job, error)
	Transition(context.Context, string, workflowjob.TransitionInput) (workflowjob.Job, error)
}

type ExecutionStore interface {
	GetByVersion(context.Context, string, int) (execution.Execution, error)
}

type WorkspaceStore interface {
	GetByWorkflow(context.Context, string) (workspace.Workspace, error)
	AcquireWrite(context.Context, string, string, time.Duration) (workspace.Lease, error)
	Renew(context.Context, string, string, time.Duration) (workspace.Lease, error)
	ReleaseWrite(context.Context, string, string, string, bool) (workspace.Workspace, error)
}

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type Handler struct {
	workflows  WorkflowStore
	executions ExecutionStore
	workspaces WorkspaceStore
	checks     Store
	detector   ProfileDetector
	runner     Runner
	recorder   EventRecorder
	workerID   string
	leaseTTL   time.Duration
}

func NewHandler(
	workflows WorkflowStore,
	executions ExecutionStore,
	workspaces WorkspaceStore,
	checkStore Store,
	detector ProfileDetector,
	runner Runner,
	recorder EventRecorder,
	workerID string,
	leaseTTL time.Duration,
) (*Handler, error) {
	workerID = strings.TrimSpace(workerID)
	if workflows == nil || executions == nil || workspaces == nil || checkStore == nil ||
		detector == nil || runner == nil || workerID == "" || leaseTTL <= 0 {
		return nil, fmt.Errorf("workflow, execution, workspace, check, detector, runner, worker, and lease dependencies are required")
	}
	return &Handler{
		workflows:  workflows,
		executions: executions,
		workspaces: workspaces,
		checks:     checkStore,
		detector:   detector,
		runner:     runner,
		recorder:   recorder,
		workerID:   workerID,
		leaseTTL:   leaseTTL,
	}, nil
}

type dispatchPayload struct {
	WorkflowJobID   string             `json:"workflow_job_id"`
	WorkflowVersion int                `json:"workflow_version"`
	Action          workflowjob.Action `json:"action"`
	TargetStatus    workflowjob.Status `json:"target_status"`
}

func (h *Handler) HandleDurable(ctx context.Context, queueJob jobqueue.Job) error {
	payload, err := decodeDispatch(queueJob.PayloadJSON)
	if err != nil {
		return err
	}
	if err := h.handle(ctx, queueJob, payload); err != nil {
		if queueJob.Attempts >= queueJob.MaxAttempts && !errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			h.failWorkflow(context.WithoutCancel(ctx), payload.WorkflowJobID, queueJob.ID, err)
		}
		return err
	}
	return nil
}

func (h *Handler) handle(ctx context.Context, queueJob jobqueue.Job, payload dispatchPayload) (returnErr error) {
	job, err := h.workflows.Get(ctx, payload.WorkflowJobID)
	if err != nil {
		return err
	}
	if job.Version < payload.WorkflowVersion {
		return fmt.Errorf(
			"workflow version %d has not reached checks dispatch version %d",
			job.Version,
			payload.WorkflowVersion,
		)
	}
	if job.Status != workflowjob.StatusChecking {
		return nil
	}
	if job.CancellationRequested {
		return context.Canceled
	}
	if job.ExecutionVersion < 1 {
		return fmt.Errorf("workflow execution version is not initialized")
	}

	executionItem, err := h.executions.GetByVersion(ctx, job.ID, job.ExecutionVersion)
	if err != nil {
		return err
	}
	if executionItem.Status != execution.StatusCompleted {
		return fmt.Errorf("execution %s is not completed", executionItem.ID)
	}
	workspaceItem, err := h.workspaces.GetByWorkflow(ctx, job.ID)
	if err != nil {
		return err
	}
	if workspaceItem.Status != workspace.StatusReady {
		return fmt.Errorf("workspace %s is not ready for checks", workspaceItem.ID)
	}

	run, _, err := h.checks.CreateOrGet(
		ctx,
		job.ID,
		executionItem.ID,
		workspaceItem.ID,
		job.ExecutionVersion,
	)
	if err != nil {
		return err
	}
	if run.Status == StatusPassed || run.Status == StatusFailed {
		return h.finishWorkflow(ctx, job, run, queueJob.ID)
	}
	profiles, err := h.detector.Detect(workspaceItem.Path)
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		return ErrNoProfiles
	}
	baseline, err := gitstate.Capture(ctx, workspaceItem.Path, gitstate.DefaultMaxPatchBytes)
	if err != nil {
		return err
	}
	if job.BaseCommitSHA != "" && baseline.Head != job.BaseCommitSHA {
		return fmt.Errorf("workspace HEAD %s does not match workflow base commit %s", baseline.Head, job.BaseCommitSHA)
	}

	lease, err := h.workspaces.AcquireWrite(ctx, workspaceItem.ID, h.workerID+":checks", h.leaseTTL)
	if err != nil {
		return err
	}
	released := false
	defer func() {
		if released {
			return
		}
		if releaseErr := h.releaseLease(context.WithoutCancel(ctx), lease); releaseErr != nil && returnErr == nil {
			returnErr = releaseErr
		}
	}()

	leaseCtx, cancelLease := workflowjob.WithWallClock(ctx, job)
	defer cancelLease()
	renewErrors := make(chan error, 1)
	go h.renewLease(leaseCtx, lease.Workspace.ID, lease.Token, cancelLease, renewErrors)
	cancellationErrors := make(chan error, 1)
	cancellationDone := workflowjob.StartCancellationWatcher(
		leaseCtx, job.ID, 500*time.Millisecond, h.workflows.Get, cancelLease, cancellationErrors,
	)
	defer func() {
		cancelLease()
		<-cancellationDone
	}()

	run, err = h.checks.Start(leaseCtx, run.ID, profiles)
	if err != nil {
		return err
	}
	if err := h.checks.RecoverInterrupted(leaseCtx, run.ID); err != nil {
		return err
	}
	existing, err := h.checks.ListSteps(leaseCtx, run.ID)
	if err != nil {
		return err
	}
	completed := make(map[string]bool, len(existing))
	for _, step := range existing {
		completed[step.Profile] = step.Status == StatusPassed || step.Status == StatusFailed
	}

	h.record(ctx, job.ID, "checks.started", map[string]any{
		"check_run_id":  run.ID,
		"execution_id":  executionItem.ID,
		"profile_count": len(profiles),
	}, time.Now().UTC())
	for _, profile := range profiles {
		if completed[profile.Name] {
			continue
		}
		if err := leaseContextError(leaseCtx, renewErrors); err != nil {
			return err
		}
		step, err := h.checks.StartStep(leaseCtx, run.ID, profile.Name)
		if err != nil {
			return err
		}
		result := h.runner.Run(leaseCtx, workspaceItem.Path, profile)
		if cancellation, ok := takeCancellationError(cancellationErrors); ok {
			if errors.Is(cancellation, context.Canceled) {
				h.cancelRun(context.WithoutCancel(ctx), run.ID, "check run cancelled")
			}
			_ = h.checks.CancelStep(context.WithoutCancel(ctx), step.ID, cancellation.Error())
			return cancellation
		}
		if result.Cancelled || leaseCtx.Err() != nil {
			_ = h.checks.CancelStep(
				context.WithoutCancel(ctx),
				step.ID,
				result.ErrorMessage,
			)
			if err := leaseContextError(leaseCtx, renewErrors); err != nil {
				return err
			}
			if err := leaseContextError(leaseCtx, cancellationErrors); err != nil {
				if errors.Is(err, context.Canceled) && ctx.Err() == nil {
					h.cancelRun(context.WithoutCancel(ctx), run.ID, "check run cancelled")
				}
				return err
			}
			return context.Canceled
		}
		if err := leaseContextError(leaseCtx, cancellationErrors); err != nil {
			if errors.Is(err, context.Canceled) && ctx.Err() == nil {
				h.cancelRun(context.WithoutCancel(ctx), run.ID, "check run cancelled")
			}
			return err
		}
		after, snapshotErr := gitstate.Capture(leaseCtx, workspaceItem.Path, gitstate.DefaultMaxPatchBytes)
		if snapshotErr != nil {
			return snapshotErr
		}
		if after.Head != baseline.Head || after.Checksum != baseline.Checksum {
			return fmt.Errorf("check profile %q modified the workspace", profile.Name)
		}
		if err := h.checks.CompleteStep(context.WithoutCancel(ctx), step.ID, result); err != nil {
			return err
		}
		h.record(ctx, job.ID, "checks.step_completed", map[string]any{
			"check_run_id":     run.ID,
			"profile":          profile.Name,
			"passed":           result.Passed,
			"exit_code":        result.ExitCode,
			"duration_ms":      result.Duration.Milliseconds(),
			"timed_out":        result.TimedOut,
			"output_truncated": result.Truncated,
		}, time.Now().UTC())
	}

	run, err = h.checks.Finish(leaseCtx, run.ID)
	if err != nil {
		return err
	}
	h.record(ctx, job.ID, "checks.completed", map[string]any{
		"check_run_id": run.ID,
		"status":       run.Status,
		"total_steps":  run.TotalSteps,
		"passed_steps": run.PassedSteps,
		"failed_steps": run.FailedSteps,
	}, time.Now().UTC())
	cancelLease()
	if err := h.releaseLease(context.WithoutCancel(ctx), lease); err != nil {
		return err
	}
	released = true
	return h.finishWorkflow(ctx, job, run, queueJob.ID)
}

func decodeDispatch(raw string) (dispatchPayload, error) {
	var payload dispatchPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, fmt.Errorf("decode checks dispatch: %w", err)
	}
	payload.WorkflowJobID = strings.TrimSpace(payload.WorkflowJobID)
	if payload.WorkflowJobID == "" || payload.WorkflowVersion < 1 ||
		payload.Action != workflowjob.ActionExecutionCompleted ||
		payload.TargetStatus != workflowjob.StatusChecking {
		return payload, fmt.Errorf("invalid checks dispatch")
	}
	return payload, nil
}

func (h *Handler) finishWorkflow(
	ctx context.Context,
	job workflowjob.Job,
	run Run,
	dispatchID string,
) error {
	current, err := h.workflows.Get(ctx, job.ID)
	if err != nil {
		return err
	}
	if current.Status != workflowjob.StatusChecking {
		return nil
	}
	_, err = h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
		ExpectedVersion: current.Version,
		Action:          workflowjob.ActionChecksCompleted,
		Details: map[string]any{
			"check_run_id":    run.ID,
			"check_status":    run.Status,
			"passed_steps":    run.PassedSteps,
			"failed_steps":    run.FailedSteps,
			"dispatch_job_id": dispatchID,
		},
	})
	if errors.Is(err, workflowjob.ErrVersionConflict) {
		latest, getErr := h.workflows.Get(ctx, current.ID)
		if getErr == nil && latest.Status != workflowjob.StatusChecking {
			return nil
		}
	}
	return err
}

func (h *Handler) failWorkflow(ctx context.Context, workflowID string, dispatchID string, cause error) {
	current, err := h.workflows.Get(ctx, workflowID)
	if err != nil || current.Status != workflowjob.StatusChecking {
		return
	}
	message := strings.TrimSpace(cause.Error())
	if len(message) > 1024 {
		message = message[:1024]
	}
	_, _ = h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
		ExpectedVersion: current.Version,
		Action:          workflowjob.ActionFail,
		FailureCode:     "CHECKS_HANDLER_FAILED",
		FailureMessage:  message,
		Details: map[string]any{
			"dispatch_job_id": dispatchID,
		},
	})
}

func (h *Handler) renewLease(
	ctx context.Context,
	workspaceID string,
	token string,
	cancel context.CancelFunc,
	result chan<- error,
) {
	interval := h.leaseTTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := h.workspaces.Renew(
				context.WithoutCancel(ctx),
				workspaceID,
				token,
				h.leaseTTL,
			); err != nil {
				select {
				case result <- fmt.Errorf("renew checks workspace lease: %w", err):
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (h *Handler) releaseLease(ctx context.Context, lease workspace.Lease) error {
	snapshot, err := gitstate.Capture(ctx, lease.Workspace.Path, gitstate.DefaultMaxPatchBytes)
	if err != nil {
		return err
	}
	_, err = h.workspaces.ReleaseWrite(ctx, lease.Workspace.ID, lease.Token, snapshot.Head, snapshot.Dirty)
	return err
}

func inspectWorkspace(root string) (string, bool, error) {
	snapshot, err := gitstate.Capture(context.Background(), root, gitstate.DefaultMaxPatchBytes)
	if err != nil {
		return "", false, err
	}
	return snapshot.Head, snapshot.Dirty, nil
}

func leaseContextError(ctx context.Context, renewErrors <-chan error) error {
	select {
	case err := <-renewErrors:
		return err
	default:
	}
	return ctx.Err()
}

func takeCancellationError(cancellationErrors <-chan error) (error, bool) {
	select {
	case err := <-cancellationErrors:
		if err == nil {
			return nil, false
		}
		return err, true
	default:
		return nil, false
	}
}

func (h *Handler) cancelRun(ctx context.Context, runID, message string) {
	if store, ok := h.checks.(interface {
		Cancel(context.Context, string, string) (Run, error)
	}); ok {
		_, _ = store.Cancel(ctx, runID, message)
	}
}

func (h *Handler) record(
	ctx context.Context,
	jobID string,
	event string,
	payload any,
	created time.Time,
) {
	if h.recorder != nil {
		_ = h.recorder.Record(context.WithoutCancel(ctx), jobID, event, payload, created)
	}
}
