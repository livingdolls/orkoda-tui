package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/execution"
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
}

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type Handler struct {
	workflows WorkflowStore
	executions ExecutionStore
	workspaces WorkspaceStore
	checks *Repository
	detector Detector
	runner CommandRunner
	recorder EventRecorder
}

func NewHandler(workflows WorkflowStore, executions ExecutionStore, workspaces WorkspaceStore, checks *Repository, detector Detector, runner CommandRunner, recorder EventRecorder) (*Handler, error) {
	if workflows == nil || executions == nil || workspaces == nil || checks == nil {
		return nil, fmt.Errorf("workflow, execution, workspace, and check dependencies are required")
	}
	return &Handler{workflows: workflows, executions: executions, workspaces: workspaces, checks: checks, detector: detector, runner: runner, recorder: recorder}, nil
}

type dispatchPayload struct {
	WorkflowJobID string `json:"workflow_job_id"`
	WorkflowVersion int `json:"workflow_version"`
	Action workflowjob.Action `json:"action"`
	TargetStatus workflowjob.Status `json:"target_status"`
}

func (h *Handler) HandleDurable(ctx context.Context, queueJob jobqueue.Job) error {
	payload, err := decodeDispatch(queueJob.PayloadJSON)
	if err != nil { return err }
	job, err := h.workflows.Get(ctx, payload.WorkflowJobID)
	if err != nil { return err }
	if job.Version < payload.WorkflowVersion { return fmt.Errorf("workflow version %d has not reached checks dispatch version %d", job.Version, payload.WorkflowVersion) }
	if job.Status != workflowjob.StatusChecking { return nil }
	if job.ExecutionVersion < 1 { return fmt.Errorf("workflow execution version is not initialized") }

	executionItem, err := h.executions.GetByVersion(ctx, job.ID, job.ExecutionVersion)
	if err != nil { return err }
	if executionItem.Status != execution.StatusCompleted { return fmt.Errorf("execution %s is not completed", executionItem.ID) }
	workspaceItem, err := h.workspaces.GetByWorkflow(ctx, job.ID)
	if err != nil { return err }
	if workspaceItem.Status != workspace.StatusReady { return fmt.Errorf("workspace %s is not ready for checks", workspaceItem.ID) }

	run, _, err := h.checks.CreateOrGet(ctx, job.ID, executionItem.ID, workspaceItem.ID, job.ExecutionVersion)
	if err != nil { return err }
	if run.Status == StatusPassed || run.Status == StatusFailed {
		return h.finishWorkflow(ctx, job, run, queueJob.ID)
	}
	profiles, err := h.detector.Detect(workspaceItem.Path)
	if err != nil { return err }
	run, err = h.checks.Start(ctx, run.ID, profiles)
	if err != nil { return err }

	existing, err := h.checks.ListSteps(ctx, run.ID)
	if err != nil { return err }
	passed := make(map[string]bool, len(existing))
	for _, step := range existing { passed[step.Profile] = step.Status == StatusPassed }

	h.record(ctx, job.ID, "checks.started", map[string]any{"check_run_id": run.ID, "execution_id": executionItem.ID, "profile_count": len(profiles)}, time.Now().UTC())
	for _, profile := range profiles {
		if passed[profile.Name] { continue }
		if err := ctx.Err(); err != nil { return err }
		step, err := h.checks.StartStep(ctx, run.ID, profile.Name)
		if err != nil { return err }
		result := h.runner.Run(ctx, workspaceItem.Path, profile)
		if err := h.checks.CompleteStep(context.WithoutCancel(ctx), step.ID, result); err != nil { return err }
		h.record(ctx, job.ID, "checks.step_completed", map[string]any{
			"check_run_id": run.ID, "profile": profile.Name, "passed": result.Passed,
			"exit_code": result.ExitCode, "duration_ms": result.Duration.Milliseconds(),
			"output_truncated": result.Truncated,
		}, time.Now().UTC())
		if err := ctx.Err(); err != nil { return err }
	}

	run, err = h.checks.Finish(ctx, run.ID)
	if err != nil { return err }
	h.record(ctx, job.ID, "checks.completed", map[string]any{
		"check_run_id": run.ID, "status": run.Status, "total_steps": run.TotalSteps,
		"passed_steps": run.PassedSteps, "failed_steps": run.FailedSteps,
	}, time.Now().UTC())
	return h.finishWorkflow(ctx, job, run, queueJob.ID)
}

func decodeDispatch(raw string) (dispatchPayload, error) {
	var payload dispatchPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil { return payload, fmt.Errorf("decode checks dispatch: %w", err) }
	if strings.TrimSpace(payload.WorkflowJobID)=="" || payload.WorkflowVersion<1 || payload.TargetStatus!=workflowjob.StatusChecking { return payload, fmt.Errorf("invalid checks dispatch") }
	return payload,nil
}

func (h *Handler) finishWorkflow(ctx context.Context, job workflowjob.Job, run Run, dispatchID string) error {
	current, err := h.workflows.Get(ctx, job.ID); if err != nil { return err }
	if current.Status != workflowjob.StatusChecking { return nil }
	_, err = h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
		ExpectedVersion: current.Version, Action: workflowjob.ActionChecksCompleted,
		Details: map[string]any{"check_run_id":run.ID,"check_status":run.Status,"passed_steps":run.PassedSteps,"failed_steps":run.FailedSteps,"dispatch_job_id":dispatchID},
	})
	if errors.Is(err, workflowjob.ErrVersionConflict) { latest,getErr:=h.workflows.Get(ctx,current.ID); if getErr==nil && latest.Status!=workflowjob.StatusChecking {return nil} }
	return err
}

func (h *Handler) record(ctx context.Context, jobID,event string,payload any,created time.Time) { if h.recorder!=nil { _=h.recorder.Record(context.WithoutCancel(ctx),jobID,event,payload,created) } }
