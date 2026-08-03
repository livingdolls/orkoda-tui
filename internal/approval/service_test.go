package approval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/reviewer"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type memoryDecisionStore struct {
	item *Decision
}

func (s *memoryDecisionStore) CreateOrGet(_ context.Context, input CreateInput) (Decision, bool, error) {
	if s.item != nil {
		if !sameDecision(*s.item, normalizeInput(input)) {
			return Decision{}, false, ErrSnapshotConflict
		}
		return *s.item, false, nil
	}
	now := time.Now().UTC()
	item := Decision{
		ID: "decision-1", WorkflowJobID: input.WorkflowJobID, ReviewRunID: input.ReviewRunID,
		ExecutionID: input.ExecutionID, ExecutionVersion: input.ExecutionVersion,
		CheckpointID: input.CheckpointID, BaseCommitSHA: input.BaseCommitSHA,
		PatchChecksum: input.PatchChecksum, Kind: input.Kind, Status: StatusPending,
		Note: input.Note, RevisionInstructions: input.RevisionInstructions,
		ReviewOverride: input.ReviewOverride, ReviewerVerdict: input.ReviewerVerdict,
		WorkflowVersionBefore: input.WorkflowVersionBefore,
		RevisionCountBefore: input.RevisionCountBefore, CreatedAt: now, UpdatedAt: now,
	}
	s.item = &item
	return item, true, nil
}

func (s *memoryDecisionStore) Get(_ context.Context, _ string) (Decision, error) {
	if s.item == nil {
		return Decision{}, ErrNotFound
	}
	return *s.item, nil
}

func (s *memoryDecisionStore) GetByVersion(_ context.Context, _ string, _ int) (Decision, error) {
	if s.item == nil {
		return Decision{}, ErrNotFound
	}
	return *s.item, nil
}

func (s *memoryDecisionStore) ListWorkflow(context.Context, string) ([]Decision, error) {
	if s.item == nil {
		return []Decision{}, nil
	}
	return []Decision{*s.item}, nil
}

func (s *memoryDecisionStore) MarkApplied(_ context.Context, _ string, version int) (Decision, error) {
	if s.item == nil {
		return Decision{}, ErrNotFound
	}
	now := time.Now().UTC()
	s.item.Status = StatusApplied
	s.item.WorkflowVersionAfter = version
	s.item.AppliedAt = &now
	s.item.UpdatedAt = now
	return *s.item, nil
}

type memoryWorkflowStore struct {
	job     workflowjob.Job
	actions []workflowjob.Action
}

func (s *memoryWorkflowStore) Get(context.Context, string) (workflowjob.Job, error) {
	return s.job, nil
}

func (s *memoryWorkflowStore) Transition(_ context.Context, _ string, input workflowjob.TransitionInput) (workflowjob.Job, error) {
	if input.ExpectedVersion != s.job.Version {
		return workflowjob.Job{}, workflowjob.ErrVersionConflict
	}
	s.actions = append(s.actions, input.Action)
	s.job.Version++
	switch input.Action {
	case workflowjob.ActionApprove:
		s.job.Status = workflowjob.StatusApproved
	case workflowjob.ActionReject:
		s.job.Status = workflowjob.StatusRejected
	case workflowjob.ActionRequestRevision:
		s.job.Status = workflowjob.StatusRevisionRequired
	case workflowjob.ActionQueueRevision:
		s.job.Status = workflowjob.StatusQueued
		s.job.RevisionCount++
	default:
		return workflowjob.Job{}, workflowjob.ErrInvalidTransition
	}
	return s.job, nil
}

type fixedExecutionStore struct {
	item        execution.Execution
	checkpoints []execution.Checkpoint
}

func (s fixedExecutionStore) GetByVersion(context.Context, string, int) (execution.Execution, error) {
	return s.item, nil
}
func (s fixedExecutionStore) ListCheckpoints(context.Context, string) ([]execution.Checkpoint, error) {
	return s.checkpoints, nil
}

type fixedReviewStore struct{ item reviewer.Run }

func (s fixedReviewStore) GetByVersion(context.Context, string, int) (reviewer.Run, error) {
	return s.item, nil
}

func approvalFixture(verdict reviewer.Verdict) (*Service, *memoryDecisionStore, *memoryWorkflowStore) {
	decisions := &memoryDecisionStore{}
	workflows := &memoryWorkflowStore{job: workflowjob.Job{
		ID: "workflow-1", Status: workflowjob.StatusWaitingApproval,
		Version: 8, ExecutionVersion: 1, RevisionCount: 0,
	}}
	executionStore := fixedExecutionStore{
		item: execution.Execution{
			ID: "execution-1", WorkflowJobID: "workflow-1", ExecutionVersion: 1,
			BaseCommitSHA: "abc123", Status: execution.StatusCompleted,
		},
		checkpoints: []execution.Checkpoint{{
			ID: "checkpoint-1", ExecutionID: "execution-1", BaseCommitSHA: "abc123",
			PatchChecksum: "sha256:patch",
		}},
	}
	reviewStore := fixedReviewStore{item: reviewer.Run{
		ID: "review-1", WorkflowJobID: "workflow-1", ExecutionID: "execution-1",
		ExecutionVersion: 1, CheckpointID: "checkpoint-1", Status: reviewer.StatusCompleted,
		Verdict: verdict,
	}}
	service, err := NewService(decisions, workflows, executionStore, reviewStore, nil)
	if err != nil {
		panic(err)
	}
	return service, decisions, workflows
}

func boundInput() DecideInput {
	return DecideInput{
		ExpectedVersion: 8, ExecutionVersion: 1,
		BaseCommitSHA: "abc123", PatchChecksum: "sha256:patch",
	}
}

func TestServiceApprovesBoundSnapshot(t *testing.T) {
	service, decisions, workflows := approvalFixture(reviewer.VerdictApprove)
	outcome, err := service.Decide(context.Background(), "workflow-1", KindApprove, boundInput())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Workflow.Status != workflowjob.StatusApproved || outcome.Decision.Status != StatusApplied {
		t.Fatalf("outcome = %#v", outcome)
	}
	if len(workflows.actions) != 1 || workflows.actions[0] != workflowjob.ActionApprove {
		t.Fatalf("actions = %#v", workflows.actions)
	}
	if decisions.item == nil || decisions.item.PatchChecksum != "sha256:patch" {
		t.Fatalf("decision = %#v", decisions.item)
	}
}

func TestServiceRejectsBindingMismatch(t *testing.T) {
	service, decisions, _ := approvalFixture(reviewer.VerdictApprove)
	input := boundInput()
	input.PatchChecksum = "sha256:stale"
	_, err := service.Decide(context.Background(), "workflow-1", KindApprove, input)
	if !errors.Is(err, ErrBindingMismatch) {
		t.Fatalf("error = %v", err)
	}
	if decisions.item != nil {
		t.Fatalf("decision should not be persisted: %#v", decisions.item)
	}
}

func TestServiceRequiresExplicitReviewerOverride(t *testing.T) {
	service, decisions, _ := approvalFixture(reviewer.VerdictRequestRevision)
	_, err := service.Decide(context.Background(), "workflow-1", KindApprove, boundInput())
	if !errors.Is(err, ErrReviewOverrideRequired) {
		t.Fatalf("error = %v", err)
	}
	if decisions.item != nil {
		t.Fatal("decision should not be stored before override acknowledgement")
	}
	input := boundInput()
	input.ReviewOverride = true
	input.Note = "I reviewed the blocking issue and accept the local-only risk."
	outcome, err := service.Decide(context.Background(), "workflow-1", KindApprove, input)
	if err != nil || outcome.Workflow.Status != workflowjob.StatusApproved {
		t.Fatalf("outcome=%#v error=%v", outcome, err)
	}
}

func TestServiceRequestsAndQueuesRevision(t *testing.T) {
	service, decisionStore, workflows := approvalFixture(reviewer.VerdictRequestRevision)
	input := boundInput()
	input.Note = "Add a regression test for the failing validation path."
	outcome, err := service.Decide(context.Background(), "workflow-1", KindRequestRevision, input)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Workflow.Status != workflowjob.StatusQueued || outcome.Workflow.RevisionCount != 1 {
		t.Fatalf("workflow = %#v", outcome.Workflow)
	}
	if len(workflows.actions) != 2 || workflows.actions[0] != workflowjob.ActionRequestRevision ||
		workflows.actions[1] != workflowjob.ActionQueueRevision {
		t.Fatalf("actions = %#v", workflows.actions)
	}
	if decisionStore.item == nil || decisionStore.item.RevisionInstructions != input.Note {
		t.Fatalf("decision = %#v", decisionStore.item)
	}
}

func TestServiceResumesRevisionAfterFirstTransition(t *testing.T) {
	service, decisions, workflows := approvalFixture(reviewer.VerdictRequestRevision)
	input := boundInput()
	input.Note = "Fix the blocking issue."
	decisions.item = &Decision{
		ID: "decision-1", WorkflowJobID: "workflow-1", ReviewRunID: "review-1",
		ExecutionID: "execution-1", ExecutionVersion: 1, CheckpointID: "checkpoint-1",
		BaseCommitSHA: "abc123", PatchChecksum: "sha256:patch",
		Kind: KindRequestRevision, Status: StatusPending, Note: input.Note,
		RevisionInstructions: input.Note, ReviewerVerdict: "REQUEST_REVISION",
		WorkflowVersionBefore: 8, RevisionCountBefore: 0,
	}
	workflows.job.Status = workflowjob.StatusRevisionRequired
	workflows.job.Version = 9
	outcome, err := service.Decide(context.Background(), "workflow-1", KindRequestRevision, input)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Workflow.Status != workflowjob.StatusQueued || len(workflows.actions) != 1 ||
		workflows.actions[0] != workflowjob.ActionQueueRevision {
		t.Fatalf("outcome=%#v actions=%#v", outcome, workflows.actions)
	}
}
