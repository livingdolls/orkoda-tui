package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

const persistenceTimeout = 5 * time.Second

type WorkflowStore interface {
	Get(context.Context, string) (workflowjob.Job, error)
	Transition(context.Context, string, workflowjob.TransitionInput) (workflowjob.Job, error)
}

type WorkspaceStore interface {
	EnsureForWorkflow(context.Context, string) (Workspace, SourceRepository, error)
	GetByWorkflow(context.Context, string) (Workspace, error)
	Acquire(context.Context, string, string, time.Duration) (Lease, error)
	Release(context.Context, string, string) error
	MarkReady(context.Context, string, string, string, bool) (Workspace, error)
	MarkFailed(context.Context, string, string, string) error
}

type Worktree interface {
	Prepare(context.Context, string, string, string) (WorktreeSnapshot, error)
	Inspect(context.Context, string, string) (WorktreeSnapshot, error)
}

type ActivityRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type PrepareHandler struct {
	workflows  WorkflowStore
	workspaces WorkspaceStore
	worktrees  Worktree
	activities ActivityRecorder
	owner      string
	leaseTTL   time.Duration
	now        func() time.Time
}

type preparePayload struct {
	WorkflowJobID   string             `json:"workflow_job_id"`
	WorkflowVersion int                `json:"workflow_version"`
	Action          workflowjob.Action `json:"action"`
	TargetStatus    workflowjob.Status `json:"target_status"`
}

func NewPrepareHandler(
	workflows WorkflowStore,
	workspaces WorkspaceStore,
	worktrees Worktree,
	activities ActivityRecorder,
	owner string,
	leaseTTL time.Duration,
) (*PrepareHandler, error) {
	if workflows == nil || workspaces == nil || worktrees == nil {
		return nil, fmt.Errorf("workflow store, workspace store, and worktree manager are required")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("workspace lease owner is required")
	}
	if leaseTTL <= 0 {
		return nil, fmt.Errorf("workspace lease TTL must be greater than zero")
	}
	if activities == nil {
		activities = discardActivityRecorder{}
	}
	return &PrepareHandler{
		workflows: workflows, workspaces: workspaces, worktrees: worktrees,
		activities: activities, owner: owner, leaseTTL: leaseTTL, now: time.Now,
	}, nil
}

func (h *PrepareHandler) Handle(ctx context.Context, queueJob jobqueue.Job) error {
	if queueJob.Type != "workflow.prepare_workspace" {
		return fmt.Errorf("unexpected job type %q", queueJob.Type)
	}
	payload, err := decodePreparePayload(queueJob.PayloadJSON)
	if err != nil {
		return err
	}

	workflow, err := h.workflows.Get(ctx, payload.WorkflowJobID)
	if err != nil {
		return fmt.Errorf("load workflow for workspace preparation: %w", err)
	}
	if isStalePrepareDispatch(workflow, payload) {
		return nil
	}
	if workflow.Status != workflowjob.StatusWorkspacePreparing || workflow.Version != payload.WorkflowVersion {
		return fmt.Errorf(
			"workflow %s is %s at version %d, expected WORKSPACE_PREPARING at version %d",
			workflow.ID, workflow.Status, workflow.Version, payload.WorkflowVersion,
		)
	}

	item, source, err := h.workspaces.EnsureForWorkflow(ctx, workflow.ID)
	if err != nil {
		return fmt.Errorf("ensure workflow workspace: %w", err)
	}
	if item.Status == StatusReady {
		snapshot, err := h.worktrees.Inspect(ctx, item.Path, workflow.BaseCommitSHA)
		if err != nil {
			return fmt.Errorf("verify ready workspace: %w", err)
		}
		if snapshot.Dirty {
			return fmt.Errorf("ready workspace is unexpectedly dirty")
		}
		return h.advanceWorkflow(ctx, workflow, item, snapshot)
	}

	lease, err := h.workspaces.Acquire(ctx, item.ID, h.owner, h.leaseTTL)
	if err != nil {
		return fmt.Errorf("acquire workspace preparation lease: %w", err)
	}
	defer h.releaseLease(ctx, item.ID, lease.Token)

	now := h.now().UTC()
	if err := h.record(ctx, workflow.ID, "workspace.preparing", map[string]any{
		"workspace_id":     item.ID,
		"repository_id":    workflow.RepositoryID,
		"workflow_version": workflow.Version,
		"queue_attempt":    queueJob.Attempts,
	}, now); err != nil {
		return err
	}

	snapshot, err := h.worktrees.Prepare(ctx, source.LocalPath, item.Path, workflow.BaseCommitSHA)
	if err != nil {
		h.persistFailure(ctx, workflow.ID, item.ID, lease.Token, err)
		return fmt.Errorf("prepare isolated Git worktree: %w", err)
	}
	if snapshot.Dirty {
		err := fmt.Errorf("new workspace is unexpectedly dirty")
		h.persistFailure(ctx, workflow.ID, item.ID, lease.Token, err)
		return err
	}

	ready, err := h.workspaces.MarkReady(ctx, item.ID, lease.Token, snapshot.HeadSHA, snapshot.Dirty)
	if err != nil {
		return fmt.Errorf("persist ready workspace: %w", err)
	}
	return h.advanceWorkflow(ctx, workflow, ready, snapshot)
}

func (h *PrepareHandler) advanceWorkflow(
	ctx context.Context,
	workflow workflowjob.Job,
	item Workspace,
	snapshot WorktreeSnapshot,
) error {
	updated, err := h.workflows.Transition(ctx, workflow.ID, workflowjob.TransitionInput{
		ExpectedVersion: workflow.Version,
		Action:          workflowjob.ActionWorkspaceReady,
		Details: map[string]any{
			"workspace_id": item.ID,
			"head_sha":     snapshot.HeadSHA,
		},
	})
	if err != nil {
		if errors.Is(err, workflowjob.ErrVersionConflict) || errors.Is(err, workflowjob.ErrInvalidTransition) {
			current, loadErr := h.workflows.Get(ctx, workflow.ID)
			if loadErr == nil && current.Version > workflow.Version &&
				current.Status != workflowjob.StatusWorkspacePreparing {
				return nil
			}
		}
		return fmt.Errorf("advance workflow after workspace preparation: %w", err)
	}

	return h.record(ctx, workflow.ID, "workspace.ready", map[string]any{
		"workspace_id":     item.ID,
		"path":             item.Path,
		"head_sha":         snapshot.HeadSHA,
		"workflow_version": updated.Version,
	}, h.now().UTC())
}

func (h *PrepareHandler) persistFailure(ctx context.Context, workflowID, workspaceID, token string, failure error) {
	if ctx.Err() != nil {
		return
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistenceTimeout)
	defer cancel()
	_ = h.workspaces.MarkFailed(persistCtx, workspaceID, token, failure.Error())
	_ = h.activities.Record(persistCtx, workflowID, "workspace.failed", map[string]any{
		"workspace_id": workspaceID,
		"error":        failure.Error(),
	}, h.now().UTC())
}

func (h *PrepareHandler) releaseLease(ctx context.Context, workspaceID, token string) {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistenceTimeout)
	defer cancel()
	err := h.workspaces.Release(persistCtx, workspaceID, token)
	if err != nil && !errors.Is(err, ErrLeaseLost) {
		_ = h.activities.Record(persistCtx, "", "workspace.lease_release_failed", map[string]any{
			"workspace_id": workspaceID,
		}, h.now().UTC())
	}
}

func (h *PrepareHandler) record(ctx context.Context, workflowID, eventType string, payload any, createdAt time.Time) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistenceTimeout)
	defer cancel()
	if err := h.activities.Record(persistCtx, workflowID, eventType, payload, createdAt); err != nil {
		return fmt.Errorf("record %s: %w", eventType, err)
	}
	return nil
}

func decodePreparePayload(raw string) (preparePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload preparePayload
	if err := decoder.Decode(&payload); err != nil {
		return preparePayload{}, fmt.Errorf("decode workspace preparation payload: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return preparePayload{}, fmt.Errorf("workspace preparation payload must contain one JSON object")
	}
	payload.WorkflowJobID = strings.TrimSpace(payload.WorkflowJobID)
	if payload.WorkflowJobID == "" || payload.WorkflowVersion < 1 ||
		payload.TargetStatus != workflowjob.StatusWorkspacePreparing ||
		(payload.Action != workflowjob.ActionStart && payload.Action != workflowjob.ActionRetry) {
		return preparePayload{}, fmt.Errorf("invalid workspace preparation payload")
	}
	return payload, nil
}

func isStalePrepareDispatch(workflow workflowjob.Job, payload preparePayload) bool {
	if workflow.Version < payload.WorkflowVersion {
		return false
	}
	if workflow.Version == payload.WorkflowVersion {
		return workflow.Status != workflowjob.StatusWorkspacePreparing
	}
	return true
}

type discardActivityRecorder struct{}

func (discardActivityRecorder) Record(context.Context, string, string, any, time.Time) error {
	return nil
}
