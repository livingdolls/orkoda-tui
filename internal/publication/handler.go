package publication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/approval"
	"github.com/livingdolls/orkoda-tui/internal/checks"
	"github.com/livingdolls/orkoda-tui/internal/gitstate"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
	"github.com/livingdolls/orkoda-tui/internal/workspace"
)

type WorkflowStore interface {
	Get(context.Context, string) (workflowjob.Job, error)
	Transition(context.Context, string, workflowjob.TransitionInput) (workflowjob.Job, error)
}

type WorkspaceStore interface {
	GetByWorkflow(context.Context, string) (workspace.Workspace, error)
	AcquireWrite(context.Context, string, string, time.Duration) (workspace.Lease, error)
	Renew(context.Context, string, string, time.Duration) (workspace.Lease, error)
	Release(context.Context, string, string) error
	ReleaseWrite(context.Context, string, string, string, bool) (workspace.Workspace, error)
}

type ApprovalStore interface {
	GetByVersion(context.Context, string, int) (approval.Decision, error)
}

type CheckStore interface {
	GetByVersion(context.Context, string, int) (checks.Run, error)
}

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type Handler struct {
	workflows    WorkflowStore
	workspaces   WorkspaceStore
	approvals    ApprovalStore
	checks       CheckStore
	publications Store
	recorder     EventRecorder
	workerID     string
	leaseTTL     time.Duration
}

func NewHandler(
	workflows WorkflowStore,
	workspaces WorkspaceStore,
	approvals ApprovalStore,
	checkStore CheckStore,
	publications Store,
	recorder EventRecorder,
	workerID string,
	leaseTTL time.Duration,
) (*Handler, error) {
	if workflows == nil || workspaces == nil || approvals == nil || checkStore == nil ||
		publications == nil || strings.TrimSpace(workerID) == "" || leaseTTL <= 0 {
		return nil, fmt.Errorf("publication workflow, workspace, approval, check, persistence, worker, and lease dependencies are required")
	}
	return &Handler{
		workflows: workflows, workspaces: workspaces, approvals: approvals, checks: checkStore,
		publications: publications, recorder: recorder, workerID: workerID, leaseTTL: leaseTTL,
	}, nil
}

type dispatchPayload struct {
	WorkflowJobID   string             `json:"workflow_job_id"`
	WorkflowVersion int                `json:"workflow_version"`
	Action          workflowjob.Action `json:"action"`
	TargetStatus    workflowjob.Status `json:"target_status"`
}

func (h *Handler) HandleDurable(ctx context.Context, queueJob jobqueue.Job) error {
	var payload dispatchPayload
	if err := json.Unmarshal([]byte(queueJob.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("decode publication dispatch: %w", err)
	}
	if strings.TrimSpace(payload.WorkflowJobID) == "" || payload.WorkflowVersion < 1 ||
		payload.Action != workflowjob.ActionPublish || payload.TargetStatus != workflowjob.StatusPublishing {
		return fmt.Errorf("invalid publication dispatch")
	}
	return h.handle(ctx, queueJob, payload)
}

func (h *Handler) handle(ctx context.Context, queueJob jobqueue.Job, payload dispatchPayload) (returnErr error) {
	job, err := h.workflows.Get(ctx, payload.WorkflowJobID)
	if err != nil {
		return err
	}
	if job.Version < payload.WorkflowVersion {
		return fmt.Errorf("workflow version %d has not reached publication dispatch version %d", job.Version, payload.WorkflowVersion)
	}
	if job.Status != workflowjob.StatusPublishing {
		return nil
	}
	if job.CancellationRequested {
		return context.Canceled
	}
	if job.ExecutionVersion < 1 {
		return fmt.Errorf("workflow execution version is not initialized")
	}
	if existing, getErr := h.publications.GetByWorkflow(ctx, job.ID); getErr == nil && existing.Status == "COMPLETED" {
		return h.finishWorkflow(ctx, job, existing, queueJob.ID)
	} else if getErr != nil && !errors.Is(getErr, ErrNotFound) {
		return getErr
	}

	decision, err := h.approvals.GetByVersion(ctx, job.ID, job.ExecutionVersion)
	if err != nil {
		return err
	}
	if decision.Status != approval.StatusApplied || decision.Kind != approval.KindApprove {
		return fmt.Errorf("workflow approval is not an applied APPROVE decision")
	}
	checkRun, err := h.checks.GetByVersion(ctx, job.ID, job.ExecutionVersion)
	if err != nil {
		return err
	}
	if checkRun.Status != checks.StatusPassed {
		if checkRun.Status != checks.StatusFailed || !decision.ReviewOverride || strings.TrimSpace(decision.Note) == "" {
			return fmt.Errorf("publication requires passed checks, got %s", checkRun.Status)
		}
	}
	item, err := h.workspaces.GetByWorkflow(ctx, job.ID)
	if err != nil {
		return err
	}
	if item.Status != workspace.StatusReady {
		return fmt.Errorf("workspace %s is not ready for publication", item.ID)
	}
	lease, err := h.workspaces.AcquireWrite(ctx, item.ID, h.workerID+":publish", h.leaseTTL)
	if err != nil {
		return err
	}
	released := false
	defer func() {
		if released {
			return
		}
		persistCtx := context.WithoutCancel(ctx)
		if snapshot, snapshotErr := gitstate.Capture(persistCtx, item.Path, gitstate.DefaultMaxPatchBytes); snapshotErr == nil {
			_, _ = h.workspaces.ReleaseWrite(persistCtx, item.ID, lease.Token, snapshot.Head, snapshot.Dirty)
		} else {
			_ = h.workspaces.Release(persistCtx, item.ID, lease.Token)
		}
	}()

	stageCtx, cancel := workflowjob.WithWallClock(ctx, job)
	defer cancel()
	leaseErrors := make(chan error, 1)
	go h.renewLease(stageCtx, item.ID, lease.Token, cancel, leaseErrors)
	cancellationErrors := make(chan error, 1)
	cancellationDone := workflowjob.StartCancellationWatcher(stageCtx, job.ID, 500*time.Millisecond, h.workflows.Get, cancel, cancellationErrors)
	defer func() {
		cancel()
		<-cancellationDone
	}()

	snapshot, err := gitstate.Capture(stageCtx, item.Path, gitstate.DefaultMaxPatchBytes)
	if err != nil {
		return err
	}
	if err := stageError(stageCtx, leaseErrors, cancellationErrors); err != nil {
		return err
	}
	if snapshot.Head != job.BaseCommitSHA || snapshot.Head != decision.BaseCommitSHA || snapshot.Checksum != decision.PatchChecksum {
		if snapshot.Head != job.BaseCommitSHA && isPublishedCommit(
			stageCtx, item.Path, job.BaseCommitSHA, job.ID, decision.PatchChecksum,
		) {
			publishedHead, headErr := currentHead(stageCtx, item.Path)
			if headErr != nil {
				return headErr
			}
			record, saveErr := h.publications.Complete(context.WithoutCancel(ctx), Record{
				WorkflowJobID: job.ID, ExecutionVersion: job.ExecutionVersion,
				ApprovalDecisionID: decision.ID, BaseCommitSHA: job.BaseCommitSHA,
				PatchChecksum: decision.PatchChecksum, PublishedCommitSHA: publishedHead,
			})
			if saveErr != nil {
				return saveErr
			}
			if _, releaseErr := h.workspaces.ReleaseWrite(context.WithoutCancel(ctx), item.ID, lease.Token, publishedHead, false); releaseErr != nil {
				return releaseErr
			}
			released = true
			return h.finishWorkflow(ctx, job, record, queueJob.ID)
		}
		return fmt.Errorf("publication snapshot does not match the approved base or patch")
	}

	if _, err := gitstate.Run(stageCtx, item.Path, "add", "--all"); err != nil {
		return fmt.Errorf("stage approved workspace patch: %w", err)
	}
	message := "Orkoda publish: " + job.ID
	if _, err := gitstate.Run(stageCtx, item.Path,
		"-c", "user.name=Orkoda", "-c", "user.email=orkoda@localhost",
		"commit", "--no-verify", "--no-gpg-sign", "--allow-empty", "-m", message,
	); err != nil {
		return fmt.Errorf("commit approved workspace patch: %w", err)
	}
	publishedHead, err := currentHead(stageCtx, item.Path)
	if err != nil {
		return err
	}
	publishedPatch, err := gitstate.Run(stageCtx, item.Path, "diff", job.BaseCommitSHA, "HEAD", "--binary", "--no-ext-diff", "--no-color")
	if err != nil {
		return fmt.Errorf("verify published workspace patch: %w", err)
	}
	if gitstate.Checksum(publishedPatch) != decision.PatchChecksum {
		return fmt.Errorf("published commit does not match the approved patch")
	}
	record, err := h.publications.Complete(context.WithoutCancel(ctx), Record{
		WorkflowJobID: job.ID, ExecutionVersion: job.ExecutionVersion,
		ApprovalDecisionID: decision.ID, BaseCommitSHA: job.BaseCommitSHA,
		PatchChecksum: decision.PatchChecksum, PublishedCommitSHA: publishedHead,
	})
	if err != nil {
		return err
	}
	if _, err := h.workspaces.ReleaseWrite(context.WithoutCancel(ctx), item.ID, lease.Token, publishedHead, false); err != nil {
		return err
	}
	released = true
	h.record(ctx, job.ID, "publication.completed", map[string]any{
		"publication_id": record.ID, "published_commit_sha": publishedHead,
		"approval_decision_id": decision.ID,
	}, time.Now().UTC())
	return h.finishWorkflow(ctx, job, record, queueJob.ID)
}

func stageError(ctx context.Context, leaseErrors, cancellationErrors <-chan error) error {
	select {
	case err := <-leaseErrors:
		return err
	default:
	}
	select {
	case err := <-cancellationErrors:
		return err
	default:
	}
	return ctx.Err()
}

func currentHead(ctx context.Context, root string) (string, error) {
	head, err := gitstate.Run(ctx, root, "rev-parse", "HEAD")
	return strings.TrimSpace(head), err
}

func isPublishedCommit(ctx context.Context, root, base, workflowID, expectedChecksum string) bool {
	head, err := currentHead(ctx, root)
	if err != nil || head == base {
		return false
	}
	parent, err := gitstate.Run(ctx, root, "rev-parse", "HEAD^")
	if err != nil || strings.TrimSpace(parent) != base {
		return false
	}
	subject, err := gitstate.Run(ctx, root, "log", "-1", "--format=%s")
	if err != nil || strings.TrimSpace(subject) != "Orkoda publish: "+workflowID {
		return false
	}
	patch, err := gitstate.Run(ctx, root, "diff", base, "HEAD", "--binary", "--no-ext-diff", "--no-color")
	return err == nil && gitstate.Checksum(patch) == expectedChecksum
}

func (h *Handler) finishWorkflow(ctx context.Context, job workflowjob.Job, record Record, dispatchID string) error {
	current, err := h.workflows.Get(ctx, job.ID)
	if err != nil {
		return err
	}
	if current.Status != workflowjob.StatusPublishing {
		return nil
	}
	_, err = h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
		ExpectedVersion: current.Version,
		Action:          workflowjob.ActionPublicationCompleted,
		Details: map[string]any{
			"publication_id": record.ID, "published_commit_sha": record.PublishedCommitSHA,
			"dispatch_job_id": dispatchID,
		},
	})
	if errors.Is(err, workflowjob.ErrVersionConflict) {
		latest, getErr := h.workflows.Get(ctx, current.ID)
		if getErr == nil && latest.Status != workflowjob.StatusPublishing {
			return nil
		}
	}
	return err
}

func (h *Handler) renewLease(ctx context.Context, workspaceID, token string, cancel context.CancelFunc, result chan<- error) {
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
			if _, err := h.workspaces.Renew(context.WithoutCancel(ctx), workspaceID, token, h.leaseTTL); err != nil {
				select {
				case result <- fmt.Errorf("renew publication workspace lease: %w", err):
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (h *Handler) record(ctx context.Context, jobID, event string, payload any, created time.Time) {
	if h.recorder != nil {
		_ = h.recorder.Record(context.WithoutCancel(ctx), jobID, event, payload, created)
	}
}
