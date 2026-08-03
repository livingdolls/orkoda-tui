package approval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/reviewer"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

var (
	ErrWorkflowNotAwaitingDecision = errors.New("workflow is not awaiting a human decision")
	ErrBindingMismatch             = errors.New("approval binding does not match the persisted execution snapshot")
	ErrReviewOverrideRequired      = errors.New("review override acknowledgement is required")
)

type WorkflowStore interface {
	Get(context.Context, string) (workflowjob.Job, error)
	Transition(context.Context, string, workflowjob.TransitionInput) (workflowjob.Job, error)
}

type ExecutionStore interface {
	GetByVersion(context.Context, string, int) (execution.Execution, error)
	ListCheckpoints(context.Context, string) ([]execution.Checkpoint, error)
}

type ReviewStore interface {
	GetByVersion(context.Context, string, int) (reviewer.Run, error)
}

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type DecideInput struct {
	ExpectedVersion  int    `json:"expected_version"`
	ExecutionVersion int    `json:"execution_version"`
	BaseCommitSHA    string `json:"base_commit_sha"`
	PatchChecksum    string `json:"patch_checksum"`
	Note             string `json:"note"`
	ReviewOverride   bool   `json:"review_override"`
}

type Outcome struct {
	Decision Decision        `json:"decision"`
	Workflow workflowjob.Job `json:"workflow"`
}

type Service struct {
	decisions  Store
	workflows  WorkflowStore
	executions ExecutionStore
	reviews    ReviewStore
	recorder   EventRecorder
}

func NewService(
	decisions Store,
	workflows WorkflowStore,
	executions ExecutionStore,
	reviews ReviewStore,
	recorder EventRecorder,
) (*Service, error) {
	if decisions == nil || workflows == nil || executions == nil || reviews == nil {
		return nil, fmt.Errorf("decision, workflow, execution, and review stores are required")
	}
	return &Service{
		decisions: decisions, workflows: workflows, executions: executions,
		reviews: reviews, recorder: recorder,
	}, nil
}

func (s *Service) Decide(ctx context.Context, workflowID string, kind Kind, input DecideInput) (Outcome, error) {
	workflowID = strings.TrimSpace(workflowID)
	input.BaseCommitSHA = strings.TrimSpace(input.BaseCommitSHA)
	input.PatchChecksum = strings.TrimSpace(input.PatchChecksum)
	input.Note = strings.TrimSpace(input.Note)
	kind = Kind(strings.ToUpper(strings.TrimSpace(string(kind))))
	if workflowID == "" || input.ExpectedVersion < 1 || input.ExecutionVersion < 1 ||
		input.BaseCommitSHA == "" || input.PatchChecksum == "" {
		return Outcome{}, fmt.Errorf("%w: expected_version, execution_version, base_commit_sha, and patch_checksum are required", ErrInvalid)
	}
	if kind != KindApprove && kind != KindRequestRevision && kind != KindReject {
		return Outcome{}, fmt.Errorf("%w: decision is invalid", ErrInvalid)
	}
	if len(input.Note) > 8000 {
		return Outcome{}, fmt.Errorf("%w: decision note exceeds 8000 characters", ErrInvalid)
	}
	if (kind == KindRequestRevision || kind == KindReject) && input.Note == "" {
		return Outcome{}, fmt.Errorf("%w: a reason is required for %s", ErrInvalid, kind)
	}

	job, err := s.workflows.Get(ctx, workflowID)
	if err != nil {
		return Outcome{}, err
	}
	executionItem, err := s.executions.GetByVersion(ctx, workflowID, input.ExecutionVersion)
	if err != nil {
		return Outcome{}, err
	}
	if executionItem.Status != execution.StatusCompleted {
		return Outcome{}, fmt.Errorf("%w: execution is not completed", ErrBindingMismatch)
	}
	reviewRun, err := s.reviews.GetByVersion(ctx, workflowID, input.ExecutionVersion)
	if err != nil {
		return Outcome{}, err
	}
	if reviewRun.Status != reviewer.StatusCompleted {
		return Outcome{}, fmt.Errorf("%w: review is not completed", ErrBindingMismatch)
	}
	checkpoints, err := s.executions.ListCheckpoints(ctx, executionItem.ID)
	if err != nil {
		return Outcome{}, err
	}
	if len(checkpoints) == 0 {
		return Outcome{}, fmt.Errorf("%w: execution has no patch checkpoint", ErrBindingMismatch)
	}
	checkpoint := checkpoints[len(checkpoints)-1]
	if executionItem.BaseCommitSHA != input.BaseCommitSHA ||
		checkpoint.BaseCommitSHA != input.BaseCommitSHA ||
		checkpoint.PatchChecksum != input.PatchChecksum ||
		reviewRun.ExecutionID != executionItem.ID || reviewRun.CheckpointID != checkpoint.ID {
		return Outcome{}, ErrBindingMismatch
	}
	if job.ExecutionVersion < input.ExecutionVersion {
		return Outcome{}, ErrBindingMismatch
	}
	if kind == KindApprove && reviewRun.Verdict == reviewer.VerdictRequestRevision {
		if !input.ReviewOverride || input.Note == "" {
			return Outcome{}, ErrReviewOverrideRequired
		}
	}

	revisionCountBefore := job.RevisionCount
	existingDecision, existingErr := s.decisions.GetByVersion(ctx, workflowID, input.ExecutionVersion)
	switch {
	case existingErr == nil:
		revisionCountBefore = existingDecision.RevisionCountBefore
	case errors.Is(existingErr, ErrNotFound):
		if job.Status != workflowjob.StatusWaitingApproval ||
			job.Version != input.ExpectedVersion ||
			job.ExecutionVersion != input.ExecutionVersion {
			return Outcome{}, ErrWorkflowNotAwaitingDecision
		}
	default:
		return Outcome{}, existingErr
	}

	createInput := CreateInput{
		WorkflowJobID:         workflowID,
		ReviewRunID:           reviewRun.ID,
		ExecutionID:           executionItem.ID,
		ExecutionVersion:      input.ExecutionVersion,
		CheckpointID:          checkpoint.ID,
		BaseCommitSHA:         input.BaseCommitSHA,
		PatchChecksum:         input.PatchChecksum,
		Kind:                  kind,
		Note:                  input.Note,
		ReviewOverride:        input.ReviewOverride,
		ReviewerVerdict:       string(reviewRun.Verdict),
		WorkflowVersionBefore: input.ExpectedVersion,
		RevisionCountBefore:   revisionCountBefore,
	}
	if kind == KindRequestRevision {
		createInput.RevisionInstructions = input.Note
	}
	decision, created, err := s.decisions.CreateOrGet(ctx, createInput)
	if err != nil {
		return Outcome{}, err
	}
	if created {
		s.record(ctx, workflowID, "approval.decision_recorded", map[string]any{
			"decision_id": decision.ID, "decision": decision.Kind,
			"execution_version": decision.ExecutionVersion,
			"base_commit_sha":   decision.BaseCommitSHA,
			"patch_checksum":    decision.PatchChecksum,
			"review_override":   decision.ReviewOverride,
		}, decision.CreatedAt)
	}
	if decision.Status == StatusApplied {
		return Outcome{Decision: decision, Workflow: job}, nil
	}

	job, err = s.apply(ctx, job, decision)
	if err != nil {
		return Outcome{}, err
	}
	decision, err = s.decisions.MarkApplied(ctx, decision.ID, job.Version)
	if err != nil {
		return Outcome{}, err
	}
	s.record(ctx, workflowID, "approval.decision_applied", map[string]any{
		"decision_id": decision.ID, "decision": decision.Kind,
		"execution_version": decision.ExecutionVersion,
		"workflow_version":  job.Version, "workflow_status": job.Status,
	}, time.Now().UTC())
	return Outcome{Decision: decision, Workflow: job}, nil
}

func (s *Service) Get(ctx context.Context, decisionID string) (Decision, error) {
	return s.decisions.Get(ctx, decisionID)
}

func (s *Service) ListWorkflow(ctx context.Context, workflowID string) ([]Decision, error) {
	return s.decisions.ListWorkflow(ctx, workflowID)
}

func (s *Service) apply(ctx context.Context, job workflowjob.Job, decision Decision) (workflowjob.Job, error) {
	if job.Status == workflowjob.StatusWaitingApproval && job.Version != decision.WorkflowVersionBefore {
		return workflowjob.Job{}, workflowjob.ErrVersionConflict
	}
	details := map[string]any{
		"approval_decision_id": decision.ID,
		"execution_version":    decision.ExecutionVersion,
		"base_commit_sha":      decision.BaseCommitSHA,
		"patch_checksum":       decision.PatchChecksum,
		"review_run_id":        decision.ReviewRunID,
		"review_override":      decision.ReviewOverride,
	}

	switch decision.Kind {
	case KindApprove:
		if approvalAlreadyAdvanced(job.Status) {
			return job, nil
		}
		if job.Status != workflowjob.StatusWaitingApproval {
			return workflowjob.Job{}, ErrWorkflowNotAwaitingDecision
		}
		return s.workflows.Transition(ctx, job.ID, workflowjob.TransitionInput{
			ExpectedVersion: job.Version, Action: workflowjob.ActionApprove, Details: details,
		})
	case KindReject:
		if job.Status == workflowjob.StatusRejected {
			return job, nil
		}
		if job.Status != workflowjob.StatusWaitingApproval {
			return workflowjob.Job{}, ErrWorkflowNotAwaitingDecision
		}
		return s.workflows.Transition(ctx, job.ID, workflowjob.TransitionInput{
			ExpectedVersion: job.Version, Action: workflowjob.ActionReject, Details: details,
		})
	case KindRequestRevision:
		return s.applyRevision(ctx, job, decision, details)
	default:
		return workflowjob.Job{}, ErrInvalid
	}
}

func (s *Service) applyRevision(
	ctx context.Context,
	job workflowjob.Job,
	decision Decision,
	details map[string]any,
) (workflowjob.Job, error) {
	if job.RevisionCount > decision.RevisionCountBefore {
		return job, nil
	}
	var err error
	if job.Status == workflowjob.StatusWaitingApproval {
		job, err = s.workflows.Transition(ctx, job.ID, workflowjob.TransitionInput{
			ExpectedVersion: job.Version,
			Action:          workflowjob.ActionRequestRevision,
			Details:         details,
		})
		if err != nil {
			return workflowjob.Job{}, err
		}
	}
	if job.Status == workflowjob.StatusRevisionRequired {
		job, err = s.workflows.Transition(ctx, job.ID, workflowjob.TransitionInput{
			ExpectedVersion: job.Version,
			Action:          workflowjob.ActionQueueRevision,
			Details:         details,
		})
		if err != nil {
			return workflowjob.Job{}, err
		}
	}
	if job.RevisionCount <= decision.RevisionCountBefore {
		return workflowjob.Job{}, ErrWorkflowNotAwaitingDecision
	}
	return job, nil
}

func approvalAlreadyAdvanced(status workflowjob.Status) bool {
	switch status {
	case workflowjob.StatusApproved, workflowjob.StatusPublishing, workflowjob.StatusCompleted:
		return true
	default:
		return false
	}
}

func (s *Service) record(ctx context.Context, workflowID, event string, payload any, createdAt time.Time) {
	if s.recorder != nil {
		_ = s.recorder.Record(context.WithoutCancel(ctx), workflowID, event, payload, createdAt)
	}
}
