package publication

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/approval"
	"github.com/livingdolls/orkoda-tui/internal/checks"
	"github.com/livingdolls/orkoda-tui/internal/gitstate"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
	"github.com/livingdolls/orkoda-tui/internal/workspace"
)

func TestPublicationHandlerCommitsApprovedSnapshot(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "base.txt")
	runGit(t, root, "commit", "-m", "base")
	base, err := gitstate.Run(context.Background(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	base = stringsTrim(base)
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("approved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := gitstate.Capture(context.Background(), root, gitstate.DefaultMaxPatchBytes)
	if err != nil {
		t.Fatal(err)
	}

	workflow := &publicationWorkflowFake{job: workflowjob.Job{
		ID: "workflow-1", Status: workflowjob.StatusPublishing, Version: 5,
		BaseCommitSHA: base, ExecutionVersion: 1,
	}}
	workspaceFake := &publicationWorkspaceFake{item: workspace.Workspace{
		ID: "workspace-1", WorkflowJobID: "workflow-1", Path: root,
		BaseCommitSHA: base, Status: workspace.StatusReady,
	}}
	store := &publicationStoreFake{}
	handler, err := NewHandler(
		workflow, workspaceFake,
		publicationApprovalFake{item: approval.Decision{
			ID: "approval-1", WorkflowJobID: "workflow-1", ExecutionVersion: 1,
			Kind: approval.KindApprove, Status: approval.StatusApplied,
			BaseCommitSHA: base, PatchChecksum: snapshot.Checksum,
		}},
		publicationCheckFake{item: checks.Run{ID: "check-1", Status: checks.StatusPassed}},
		store, nil, "worker-1", time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(dispatchPayload{
		WorkflowJobID: "workflow-1", WorkflowVersion: 5,
		Action: workflowjob.ActionPublish, TargetStatus: workflowjob.StatusPublishing,
	})
	if err := handler.HandleDurable(context.Background(), jobqueue.Job{ID: "dispatch-1", PayloadJSON: string(payload), Attempts: 1, MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	if workflow.job.Status != workflowjob.StatusCompleted || store.item.PublishedCommitSHA == "" {
		t.Fatalf("workflow=%#v publication=%#v", workflow.job, store.item)
	}
	current, err := gitstate.Run(context.Background(), root, "rev-parse", "HEAD")
	if err != nil || stringsTrim(current) != store.item.PublishedCommitSHA {
		t.Fatalf("HEAD=%q publication=%#v error=%v", current, store.item, err)
	}
	published := store.item.PublishedCommitSHA
	if err := handler.HandleDurable(context.Background(), jobqueue.Job{ID: "dispatch-retry", PayloadJSON: string(payload), Attempts: 2, MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	current, err = gitstate.Run(context.Background(), root, "rev-parse", "HEAD")
	if err != nil || stringsTrim(current) != published {
		t.Fatalf("duplicate publication changed HEAD=%q, want %q", current, published)
	}
}

func TestPublicationHandlerRecoversWhenPersistenceFailsAfterCommit(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "base.txt")
	runGit(t, root, "commit", "-m", "base")
	base, err := gitstate.Run(context.Background(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	base = stringsTrim(base)
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("approved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := gitstate.Capture(context.Background(), root, gitstate.DefaultMaxPatchBytes)
	if err != nil {
		t.Fatal(err)
	}
	workflow := &publicationWorkflowFake{job: workflowjob.Job{
		ID: "workflow-recovery", Status: workflowjob.StatusPublishing, Version: 3,
		BaseCommitSHA: base, ExecutionVersion: 1,
	}}
	workspaceFake := &publicationWorkspaceFake{item: workspace.Workspace{
		ID: "workspace-recovery", WorkflowJobID: workflow.job.ID, Path: root,
		BaseCommitSHA: base, Status: workspace.StatusReady,
	}}
	store := &publicationStoreFake{failOnce: true}
	handler, err := NewHandler(
		workflow, workspaceFake,
		publicationApprovalFake{item: approval.Decision{
			ID: "approval-recovery", WorkflowJobID: workflow.job.ID, ExecutionVersion: 1,
			Kind: approval.KindApprove, Status: approval.StatusApplied,
			BaseCommitSHA: base, PatchChecksum: snapshot.Checksum,
		}},
		publicationCheckFake{item: checks.Run{ID: "check-recovery", Status: checks.StatusPassed}},
		store, nil, "worker-recovery", time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(dispatchPayload{
		WorkflowJobID: workflow.job.ID, WorkflowVersion: workflow.job.Version,
		Action: workflowjob.ActionPublish, TargetStatus: workflowjob.StatusPublishing,
	})
	firstErr := handler.HandleDurable(context.Background(), jobqueue.Job{ID: "dispatch-first", PayloadJSON: string(payload), Attempts: 1, MaxAttempts: 3})
	if firstErr == nil {
		t.Fatal("first publication unexpectedly succeeded")
	}
	firstHead, err := currentHead(context.Background(), root)
	if err != nil || firstHead == base {
		t.Fatalf("commit was not durable after persistence failure: head=%q base=%q err=%v", firstHead, base, err)
	}
	if store.item.ID != "" {
		t.Fatal("publication record was persisted despite injected failure")
	}
	if err := handler.HandleDurable(context.Background(), jobqueue.Job{ID: "dispatch-retry", PayloadJSON: string(payload), Attempts: 2, MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	secondHead, err := currentHead(context.Background(), root)
	if err != nil || secondHead != firstHead {
		t.Fatalf("recovery changed published HEAD: first=%q second=%q err=%v", firstHead, secondHead, err)
	}
	if workflow.job.Status != workflowjob.StatusCompleted || store.item.Status != "COMPLETED" {
		t.Fatalf("workflow=%#v publication=%#v", workflow.job, store.item)
	}
}

type publicationWorkflowFake struct {
	job workflowjob.Job
}

func (f *publicationWorkflowFake) Get(context.Context, string) (workflowjob.Job, error) {
	return f.job, nil
}
func (f *publicationWorkflowFake) Transition(_ context.Context, _ string, input workflowjob.TransitionInput) (workflowjob.Job, error) {
	if input.ExpectedVersion != f.job.Version {
		return workflowjob.Job{}, workflowjob.ErrVersionConflict
	}
	f.job.Version++
	if input.Action == workflowjob.ActionPublicationCompleted {
		f.job.Status = workflowjob.StatusCompleted
		return f.job, nil
	}
	return workflowjob.Job{}, workflowjob.ErrInvalidTransition
}

type publicationWorkspaceFake struct {
	item  workspace.Workspace
	lease workspace.Lease
}

func (f *publicationWorkspaceFake) GetByWorkflow(context.Context, string) (workspace.Workspace, error) {
	return f.item, nil
}
func (f *publicationWorkspaceFake) AcquireWrite(_ context.Context, _ string, _ string, _ time.Duration) (workspace.Lease, error) {
	f.item.Status = workspace.StatusWriteLocked
	f.lease = workspace.Lease{Workspace: f.item, Token: "lease-1"}
	return f.lease, nil
}
func (f *publicationWorkspaceFake) Renew(context.Context, string, string, time.Duration) (workspace.Lease, error) {
	return f.lease, nil
}
func (f *publicationWorkspaceFake) Release(context.Context, string, string) error { return nil }
func (f *publicationWorkspaceFake) ReleaseWrite(_ context.Context, _ string, token, head string, dirty bool) (workspace.Workspace, error) {
	if token != f.lease.Token {
		return workspace.Workspace{}, workspace.ErrLeaseLost
	}
	f.item.Status, f.item.HeadSHA, f.item.Dirty = workspace.StatusReady, head, dirty
	return f.item, nil
}

type publicationApprovalFake struct{ item approval.Decision }

func (f publicationApprovalFake) GetByVersion(context.Context, string, int) (approval.Decision, error) {
	return f.item, nil
}

type publicationCheckFake struct{ item checks.Run }

func (f publicationCheckFake) GetByVersion(context.Context, string, int) (checks.Run, error) {
	return f.item, nil
}

type publicationStoreFake struct {
	item     Record
	failOnce bool
}

func (f *publicationStoreFake) GetByWorkflow(context.Context, string) (Record, error) {
	if f.item.ID == "" {
		return Record{}, ErrNotFound
	}
	return f.item, nil
}
func (f *publicationStoreFake) Complete(_ context.Context, item Record) (Record, error) {
	if item.WorkflowJobID == "" {
		return Record{}, errors.New("workflow ID required")
	}
	if f.failOnce {
		f.failOnce = false
		return Record{}, errors.New("injected publication persistence failure")
	}
	item.ID = "publication-1"
	item.Status = "COMPLETED"
	f.item = item
	return item, nil
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func stringsTrim(value string) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r' || value[len(value)-1] == ' ') {
		value = value[:len(value)-1]
	}
	return value
}
