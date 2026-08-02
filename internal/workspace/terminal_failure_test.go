package workspace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

func TestHandleDurableFailsWorkflowOnLastQueueAttempt(t *testing.T) {
	workflow := workflowjob.Job{
		ID: "workflow-1", BaseCommitSHA: "abc123",
		Status: workflowjob.StatusWorkspacePreparing, Version: 2,
	}
	workflows := &fakeWorkflowStore{job: workflow}
	workspaces := &fakeWorkspaceStore{
		item:   Workspace{ID: "workspace-1", WorkflowJobID: workflow.ID, Path: "/tmp/workspace", Status: StatusRequested},
		source: SourceRepository{LocalPath: "/tmp/source"},
		lease:  Lease{Workspace: Workspace{ID: "workspace-1"}, Token: "lease-token"},
	}
	worktrees := &fakeWorktree{err: errors.New("git worktree failed")}
	handler, err := NewPrepareHandler(workflows, workspaces, worktrees, nil, "worker-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	queueJob := prepareQueueJob()
	queueJob.Attempts = 3
	queueJob.MaxAttempts = 3
	if err := handler.HandleDurable(context.Background(), queueJob); err == nil {
		t.Fatal("HandleDurable() expected an error")
	}
	if len(workflows.transitionCalls) != 1 {
		t.Fatalf("transition calls = %#v", workflows.transitionCalls)
	}
	transition := workflows.transitionCalls[0]
	if transition.Action != workflowjob.ActionFail || transition.FailureCode != "WORKSPACE_PREPARATION_FAILED" {
		t.Fatalf("terminal transition = %#v", transition)
	}
	if transition.Details["attempt"] != 3 || transition.Details["max_attempts"] != 3 {
		t.Fatalf("terminal details = %#v", transition.Details)
	}
}

func TestHandleDurableKeepsWorkflowPreparingBeforeLastAttempt(t *testing.T) {
	workflow := workflowjob.Job{
		ID: "workflow-1", BaseCommitSHA: "abc123",
		Status: workflowjob.StatusWorkspacePreparing, Version: 2,
	}
	workflows := &fakeWorkflowStore{job: workflow}
	workspaces := &fakeWorkspaceStore{
		item:   Workspace{ID: "workspace-1", WorkflowJobID: workflow.ID, Path: "/tmp/workspace", Status: StatusRequested},
		source: SourceRepository{LocalPath: "/tmp/source"},
		lease:  Lease{Workspace: Workspace{ID: "workspace-1"}, Token: "lease-token"},
	}
	handler, _ := NewPrepareHandler(
		workflows, workspaces, &fakeWorktree{err: errors.New("temporary")}, nil, "worker-1", time.Minute,
	)
	queueJob := prepareQueueJob()
	queueJob.Attempts = 1
	queueJob.MaxAttempts = 3
	if err := handler.HandleDurable(context.Background(), queueJob); err == nil {
		t.Fatal("HandleDurable() expected an error")
	}
	if len(workflows.transitionCalls) != 0 {
		t.Fatalf("unexpected transition calls = %#v", workflows.transitionCalls)
	}
}

func TestHandleDurableDoesNotFailAdvancedWorkflow(t *testing.T) {
	workflow := workflowjob.Job{
		ID: "workflow-1", Status: workflowjob.StatusQueued, Version: 3,
	}
	workflows := &fakeWorkflowStore{job: workflow}
	handler, _ := NewPrepareHandler(
		workflows, &fakeWorkspaceStore{}, &fakeWorktree{}, nil, "worker-1", time.Minute,
	)
	queueJob := jobqueue.Job{
		ID: "queue-1", Type: "workflow.prepare_workspace", Attempts: 3, MaxAttempts: 3,
		PayloadJSON: `{"workflow_job_id":"workflow-1","workflow_version":2,"action":"START","target_status":"WORKSPACE_PREPARING"}`,
	}
	if err := handler.HandleDurable(context.Background(), queueJob); err != nil {
		t.Fatalf("HandleDurable() stale no-op error = %v", err)
	}
	if len(workflows.transitionCalls) != 0 {
		t.Fatalf("unexpected transition calls = %#v", workflows.transitionCalls)
	}
}
