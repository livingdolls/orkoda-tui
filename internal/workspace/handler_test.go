package workspace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type fakeWorkflowStore struct {
	job             workflowjob.Job
	transitionCalls []workflowjob.TransitionInput
	transitionErr   error
}

func (f *fakeWorkflowStore) Get(context.Context, string) (workflowjob.Job, error) {
	return f.job, nil
}

func (f *fakeWorkflowStore) Transition(_ context.Context, _ string, input workflowjob.TransitionInput) (workflowjob.Job, error) {
	f.transitionCalls = append(f.transitionCalls, input)
	if f.transitionErr != nil {
		return workflowjob.Job{}, f.transitionErr
	}
	f.job.Version++
	f.job.Status = workflowjob.StatusQueued
	return f.job, nil
}

type fakeWorkspaceStore struct {
	item         Workspace
	source       SourceRepository
	lease        Lease
	acquireErr   error
	ensureCalls  int
	acquireCalls int
	restartCalls int
	readyCalls   int
	failedCalls  int
	releaseCalls int
}

func (f *fakeWorkspaceStore) EnsureForWorkflow(context.Context, string) (Workspace, SourceRepository, error) {
	f.ensureCalls++
	return f.item, f.source, nil
}

func (f *fakeWorkspaceStore) GetByWorkflow(context.Context, string) (Workspace, error) {
	return f.item, nil
}

func (f *fakeWorkspaceStore) Acquire(context.Context, string, string, time.Duration) (Lease, error) {
	f.acquireCalls++
	if f.acquireErr != nil {
		return Lease{}, f.acquireErr
	}
	return f.lease, nil
}

func (f *fakeWorkspaceStore) AcquireRestart(context.Context, string, string, time.Duration) (Lease, error) {
	f.restartCalls++
	if f.acquireErr != nil {
		return Lease{}, f.acquireErr
	}
	return f.lease, nil
}

func (f *fakeWorkspaceStore) Release(context.Context, string, string) error {
	f.releaseCalls++
	return nil
}

func (f *fakeWorkspaceStore) MarkReady(_ context.Context, _ string, _ string, head string, dirty bool) (Workspace, error) {
	f.readyCalls++
	f.item.Status = StatusReady
	f.item.HeadSHA = head
	f.item.Dirty = dirty
	return f.item, nil
}

func (f *fakeWorkspaceStore) MarkFailed(context.Context, string, string, string) error {
	f.failedCalls++
	return nil
}

type fakeWorktree struct {
	prepareCalls int
	inspectCalls int
	removeCalls  int
	snapshot     WorktreeSnapshot
	err          error
}

func (f *fakeWorktree) Prepare(context.Context, string, string, string) (WorktreeSnapshot, error) {
	f.prepareCalls++
	return f.snapshot, f.err
}

func (f *fakeWorktree) Inspect(context.Context, string, string) (WorktreeSnapshot, error) {
	f.inspectCalls++
	return f.snapshot, f.err
}

func (f *fakeWorktree) Remove(context.Context, string, string) error {
	f.removeCalls++
	return f.err
}

type recordedActivity struct {
	eventType string
	payload   any
}

type fakeWorkspaceActivities struct {
	items []recordedActivity
}

func (f *fakeWorkspaceActivities) Record(_ context.Context, _ string, eventType string, payload any, _ time.Time) error {
	f.items = append(f.items, recordedActivity{eventType: eventType, payload: payload})
	return nil
}

func TestPrepareHandlerCreatesWorkspaceAndAdvancesWorkflow(t *testing.T) {
	workflow := workflowjob.Job{
		ID: "workflow-1", RepositoryID: "repository-1", BaseCommitSHA: "abc123",
		Status: workflowjob.StatusWorkspacePreparing, Version: 2,
	}
	workflows := &fakeWorkflowStore{job: workflow}
	workspaces := &fakeWorkspaceStore{
		item:   Workspace{ID: "workspace-1", WorkflowJobID: workflow.ID, Path: "/tmp/workspace", Status: StatusRequested},
		source: SourceRepository{ID: "repository-1", LocalPath: "/tmp/source"},
		lease:  Lease{Workspace: Workspace{ID: "workspace-1", Status: StatusPreparing}, Token: "lease-token"},
	}
	worktrees := &fakeWorktree{snapshot: WorktreeSnapshot{Path: "/tmp/workspace", HeadSHA: "abc123"}}
	activities := &fakeWorkspaceActivities{}
	handler, err := NewPrepareHandler(workflows, workspaces, worktrees, activities, "worker-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if err := handler.Handle(context.Background(), prepareQueueJob()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if worktrees.prepareCalls != 1 || workspaces.readyCalls != 1 || workspaces.releaseCalls != 1 {
		t.Fatalf("calls: worktree=%d ready=%d release=%d", worktrees.prepareCalls, workspaces.readyCalls, workspaces.releaseCalls)
	}
	if len(workflows.transitionCalls) != 1 || workflows.transitionCalls[0].Action != workflowjob.ActionWorkspaceReady {
		t.Fatalf("transition calls = %#v", workflows.transitionCalls)
	}
	if len(activities.items) != 2 || activities.items[0].eventType != "workspace.preparing" || activities.items[1].eventType != "workspace.ready" {
		t.Fatalf("activities = %#v", activities.items)
	}
}

func TestPrepareHandlerResumesReadyWorkspaceWithoutCreatingAnotherWorktree(t *testing.T) {
	workflow := workflowjob.Job{
		ID: "workflow-1", BaseCommitSHA: "abc123",
		Status: workflowjob.StatusWorkspacePreparing, Version: 2,
	}
	workflows := &fakeWorkflowStore{job: workflow}
	workspaces := &fakeWorkspaceStore{
		item: Workspace{ID: "workspace-1", WorkflowJobID: workflow.ID, Path: "/tmp/workspace", Status: StatusReady, HeadSHA: "abc123"},
	}
	worktrees := &fakeWorktree{snapshot: WorktreeSnapshot{Path: "/tmp/workspace", HeadSHA: "abc123"}}
	handler, _ := NewPrepareHandler(workflows, workspaces, worktrees, nil, "worker-1", time.Minute)

	if err := handler.Handle(context.Background(), prepareQueueJob()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if worktrees.prepareCalls != 0 || worktrees.inspectCalls != 1 || workspaces.acquireCalls != 0 {
		t.Fatalf("calls: prepare=%d inspect=%d acquire=%d", worktrees.prepareCalls, worktrees.inspectCalls, workspaces.acquireCalls)
	}
}

func TestPrepareHandlerTreatsAdvancedWorkflowAsStaleNoOp(t *testing.T) {
	workflows := &fakeWorkflowStore{job: workflowjob.Job{
		ID: "workflow-1", Status: workflowjob.StatusQueued, Version: 3,
	}}
	workspaces := &fakeWorkspaceStore{}
	worktrees := &fakeWorktree{}
	handler, _ := NewPrepareHandler(workflows, workspaces, worktrees, nil, "worker-1", time.Minute)

	if err := handler.Handle(context.Background(), prepareQueueJob()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if workspaces.ensureCalls != 0 || worktrees.prepareCalls != 0 {
		t.Fatalf("stale dispatch performed side effects")
	}
}

func TestPrepareHandlerPersistsFailureAndReturnsForQueueRetry(t *testing.T) {
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
	handler, _ := NewPrepareHandler(workflows, workspaces, worktrees, nil, "worker-1", time.Minute)

	err := handler.Handle(context.Background(), prepareQueueJob())
	if err == nil || workspaces.failedCalls != 1 || workspaces.releaseCalls != 1 {
		t.Fatalf("Handle() error = %v failed=%d release=%d", err, workspaces.failedCalls, workspaces.releaseCalls)
	}
	if len(workflows.transitionCalls) != 0 {
		t.Fatalf("unexpected workflow transition = %#v", workflows.transitionCalls)
	}
}

func TestPrepareHandlerReturnsLeaseContentionForRetry(t *testing.T) {
	workflow := workflowjob.Job{
		ID: "workflow-1", BaseCommitSHA: "abc123",
		Status: workflowjob.StatusWorkspacePreparing, Version: 2,
	}
	workspaces := &fakeWorkspaceStore{
		item:       Workspace{ID: "workspace-1", Status: StatusPreparing},
		acquireErr: ErrLeaseUnavailable,
	}
	handler, _ := NewPrepareHandler(&fakeWorkflowStore{job: workflow}, workspaces, &fakeWorktree{}, nil, "worker-1", time.Minute)
	if err := handler.Handle(context.Background(), prepareQueueJob()); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("Handle() error = %v", err)
	}
}

func prepareQueueJob() jobqueue.Job {
	return jobqueue.Job{
		ID: "queue-1", Type: "workflow.prepare_workspace", Attempts: 1,
		PayloadJSON: `{"workflow_job_id":"workflow-1","workflow_version":2,"action":"START","target_status":"WORKSPACE_PREPARING"}`,
	}
}

func TestPrepareHandlerRestartsWorkspaceFromPinnedBase(t *testing.T) {
	workflow := workflowjob.Job{
		ID: "workflow-1", RepositoryID: "repository-1", BaseCommitSHA: "abc123",
		Status: workflowjob.StatusWorkspacePreparing, Version: 8,
	}
	workflows := &fakeWorkflowStore{job: workflow}
	workspaces := &fakeWorkspaceStore{
		item: Workspace{
			ID: "workspace-1", WorkflowJobID: workflow.ID,
			Path: "/tmp/workspace", Status: StatusReady, Dirty: true,
		},
		source: SourceRepository{ID: "repository-1", LocalPath: "/tmp/source"},
		lease: Lease{
			Workspace: Workspace{ID: "workspace-1", Status: StatusPreparing},
			Token:     "restart-token",
		},
	}
	worktrees := &fakeWorktree{snapshot: WorktreeSnapshot{
		Path: "/tmp/workspace", HeadSHA: "abc123", Dirty: false,
	}}
	activities := &fakeWorkspaceActivities{}
	handler, err := NewPrepareHandler(
		workflows,
		workspaces,
		worktrees,
		activities,
		"worker-1",
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := handler.Handle(context.Background(), restartQueueJob()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if workspaces.restartCalls != 1 || worktrees.removeCalls != 1 ||
		worktrees.prepareCalls != 1 || workspaces.readyCalls != 1 {
		t.Fatalf(
			"calls restart=%d remove=%d prepare=%d ready=%d",
			workspaces.restartCalls,
			worktrees.removeCalls,
			worktrees.prepareCalls,
			workspaces.readyCalls,
		)
	}
	if len(workflows.transitionCalls) != 1 ||
		workflows.transitionCalls[0].Action != workflowjob.ActionWorkspaceReady {
		t.Fatalf("transition calls = %#v", workflows.transitionCalls)
	}
	if len(activities.items) != 3 ||
		activities.items[0].eventType != "workspace.restarting" ||
		activities.items[1].eventType != "workspace.restarted" ||
		activities.items[2].eventType != "workspace.ready" {
		t.Fatalf("activities = %#v", activities.items)
	}
}

func restartQueueJob() jobqueue.Job {
	return jobqueue.Job{
		ID: "queue-restart", Type: "workflow.prepare_workspace", Attempts: 1,
		PayloadJSON: `{"workflow_job_id":"workflow-1","workflow_version":8,"action":"RESTART","target_status":"WORKSPACE_PREPARING"}`,
	}
}
