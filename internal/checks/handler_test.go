package checks

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
	"github.com/livingdolls/orkoda-tui/internal/workspace"
)

type fakeCheckWorkflows struct {
	job         workflowjob.Job
	transitions []workflowjob.TransitionInput
}

func (f *fakeCheckWorkflows) Get(context.Context, string) (workflowjob.Job, error) {
	return f.job, nil
}

func (f *fakeCheckWorkflows) Transition(
	_ context.Context,
	_ string,
	input workflowjob.TransitionInput,
) (workflowjob.Job, error) {
	if input.ExpectedVersion != f.job.Version {
		return workflowjob.Job{}, workflowjob.ErrVersionConflict
	}
	f.transitions = append(f.transitions, input)
	f.job.Version++
	switch input.Action {
	case workflowjob.ActionChecksCompleted:
		f.job.Status = workflowjob.StatusReviewing
	case workflowjob.ActionFail:
		f.job.Status = workflowjob.StatusFailed
		f.job.FailureCode = input.FailureCode
		f.job.FailureMessage = input.FailureMessage
	}
	return f.job, nil
}

type fakeCheckExecutions struct{ item execution.Execution }

func (f fakeCheckExecutions) GetByVersion(context.Context, string, int) (execution.Execution, error) {
	return f.item, nil
}

type fakeCheckWorkspaces struct {
	item         workspace.Workspace
	lease        workspace.Lease
	acquireCalls int
	releaseCalls int
}

func (f *fakeCheckWorkspaces) GetByWorkflow(context.Context, string) (workspace.Workspace, error) {
	return f.item, nil
}

func (f *fakeCheckWorkspaces) AcquireWrite(
	context.Context,
	string,
	string,
	time.Duration,
) (workspace.Lease, error) {
	f.acquireCalls++
	f.item.Status = workspace.StatusWriteLocked
	f.lease.Workspace = f.item
	return f.lease, nil
}

func (f *fakeCheckWorkspaces) Renew(
	context.Context,
	string,
	string,
	time.Duration,
) (workspace.Lease, error) {
	return f.lease, nil
}

func (f *fakeCheckWorkspaces) ReleaseWrite(
	_ context.Context,
	_ string,
	_ string,
	head string,
	dirty bool,
) (workspace.Workspace, error) {
	f.releaseCalls++
	f.item.Status = workspace.StatusReady
	f.item.HeadSHA = head
	f.item.Dirty = dirty
	return f.item, nil
}

type fakeCheckStore struct {
	run   Run
	steps []Step
}

func (f *fakeCheckStore) CreateOrGet(
	context.Context,
	string,
	string,
	string,
	int,
) (Run, bool, error) {
	if f.run.ID == "" {
		f.run = Run{ID: "check-1", Status: StatusPending, ExecutionVersion: 1}
		return f.run, true, nil
	}
	return f.run, false, nil
}

func (f *fakeCheckStore) Get(context.Context, string) (Run, error) { return f.run, nil }
func (f *fakeCheckStore) GetByVersion(context.Context, string, int) (Run, error) {
	return f.run, nil
}
func (f *fakeCheckStore) ListWorkflow(context.Context, string) ([]Run, error) {
	return []Run{f.run}, nil
}
func (f *fakeCheckStore) Start(_ context.Context, _ string, profiles []Profile) (Run, error) {
	f.run.Status = StatusRunning
	f.run.TotalSteps = len(profiles)
	if len(f.steps) == 0 {
		for index, profile := range profiles {
			f.steps = append(f.steps, Step{
				ID: "step-" + profile.Name, CheckRunID: f.run.ID,
				Sequence: index + 1, Profile: profile.Name, Status: StatusPending,
			})
		}
	}
	return f.run, nil
}
func (f *fakeCheckStore) RecoverInterrupted(context.Context, string) error {
	for index := range f.steps {
		if f.steps[index].Status == StatusRunning || f.steps[index].Status == StatusCancelled {
			f.steps[index].Status = StatusPending
		}
	}
	return nil
}
func (f *fakeCheckStore) StartStep(_ context.Context, _ string, profile string) (Step, error) {
	for index := range f.steps {
		if f.steps[index].Profile == profile && f.steps[index].Status == StatusPending {
			f.steps[index].Status = StatusRunning
			return f.steps[index], nil
		}
	}
	return Step{}, ErrNotFound
}
func (f *fakeCheckStore) CompleteStep(_ context.Context, stepID string, result Result) error {
	for index := range f.steps {
		if f.steps[index].ID == stepID {
			if result.Passed {
				f.steps[index].Status = StatusPassed
			} else {
				f.steps[index].Status = StatusFailed
			}
			return nil
		}
	}
	return ErrNotFound
}
func (f *fakeCheckStore) CancelStep(_ context.Context, stepID string, _ string) error {
	for index := range f.steps {
		if f.steps[index].ID == stepID {
			f.steps[index].Status = StatusCancelled
			return nil
		}
	}
	return ErrNotFound
}
func (f *fakeCheckStore) ListSteps(context.Context, string) ([]Step, error) {
	return append([]Step(nil), f.steps...), nil
}
func (f *fakeCheckStore) Finish(context.Context, string) (Run, error) {
	f.run.PassedSteps = 0
	f.run.FailedSteps = 0
	for _, step := range f.steps {
		switch step.Status {
		case StatusPassed:
			f.run.PassedSteps++
		case StatusFailed:
			f.run.FailedSteps++
		default:
			return Run{}, ErrInvalid
		}
	}
	if f.run.FailedSteps > 0 {
		f.run.Status = StatusFailed
	} else {
		f.run.Status = StatusPassed
	}
	return f.run, nil
}

type staticDetector struct {
	profiles []Profile
	err      error
}

func (d staticDetector) Detect(string) ([]Profile, error) { return d.profiles, d.err }

type sequenceCheckRunner struct {
	results []Result
	calls   int
}

func (r *sequenceCheckRunner) Run(context.Context, string, Profile) Result {
	result := r.results[r.calls]
	r.calls++
	return result
}

func TestHandlerPersistsFailedChecksAndAdvancesToReview(t *testing.T) {
	root := prepareCheckGitRepository(t)
	workflows := &fakeCheckWorkflows{job: checkingWorkflow()}
	workspaces := &fakeCheckWorkspaces{
		item:  workspace.Workspace{ID: "workspace-1", Path: root, Status: workspace.StatusReady},
		lease: workspace.Lease{Token: "lease-token"},
	}
	store := &fakeCheckStore{}
	runner := &sequenceCheckRunner{results: []Result{
		{Passed: true, ExitCode: 0, OutputLimit: 1024},
		{Passed: false, ExitCode: 1, OutputLimit: 1024, ErrorMessage: "test failed"},
	}}
	handler, err := NewHandler(
		workflows,
		fakeCheckExecutions{item: completedExecution()},
		workspaces,
		store,
		staticDetector{profiles: testProfiles()},
		runner,
		nil,
		"worker-1",
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}

	job := checkQueueJob(1, 3)
	if err := handler.HandleDurable(context.Background(), job); err != nil {
		t.Fatalf("HandleDurable() error = %v", err)
	}
	if workflows.job.Status != workflowjob.StatusReviewing || store.run.Status != StatusFailed {
		t.Fatalf("workflow = %#v, check = %#v", workflows.job, store.run)
	}
	if runner.calls != 2 || workspaces.acquireCalls != 1 || workspaces.releaseCalls != 1 {
		t.Fatalf("calls runner=%d acquire=%d release=%d", runner.calls, workspaces.acquireCalls, workspaces.releaseCalls)
	}

	if err := handler.HandleDurable(context.Background(), job); err != nil {
		t.Fatalf("stale HandleDurable() error = %v", err)
	}
	if runner.calls != 2 {
		t.Fatalf("stale dispatch reran checks: calls=%d", runner.calls)
	}
}

func TestHandlerRecoversCancelledStepOnRetry(t *testing.T) {
	root := prepareCheckGitRepository(t)
	workflows := &fakeCheckWorkflows{job: checkingWorkflow()}
	workspaces := &fakeCheckWorkspaces{
		item:  workspace.Workspace{ID: "workspace-1", Path: root, Status: workspace.StatusReady},
		lease: workspace.Lease{Token: "lease-token"},
	}
	store := &fakeCheckStore{}
	runner := &sequenceCheckRunner{results: []Result{
		{Cancelled: true, ExitCode: -1, OutputLimit: 1024, ErrorMessage: "check cancelled"},
		{Passed: true, ExitCode: 0, OutputLimit: 1024},
	}}
	handler, _ := NewHandler(
		workflows,
		fakeCheckExecutions{item: completedExecution()},
		workspaces,
		store,
		staticDetector{profiles: testProfiles()[:1]},
		runner,
		nil,
		"worker-1",
		time.Minute,
	)

	if err := handler.HandleDurable(context.Background(), checkQueueJob(1, 3)); !errors.Is(err, context.Canceled) {
		t.Fatalf("first HandleDurable() error = %v", err)
	}
	if store.steps[0].Status != StatusCancelled || workflows.job.Status != workflowjob.StatusChecking {
		t.Fatalf("step=%#v workflow=%#v", store.steps[0], workflows.job)
	}
	if err := handler.HandleDurable(context.Background(), checkQueueJob(2, 3)); err != nil {
		t.Fatalf("retry HandleDurable() error = %v", err)
	}
	if store.steps[0].Status != StatusPassed || workflows.job.Status != workflowjob.StatusReviewing {
		t.Fatalf("step=%#v workflow=%#v", store.steps[0], workflows.job)
	}
	if workspaces.releaseCalls != 2 {
		t.Fatalf("release calls = %d", workspaces.releaseCalls)
	}
}

func TestHandlerMovesWorkflowToFailedAfterFinalInfrastructureAttempt(t *testing.T) {
	workflows := &fakeCheckWorkflows{job: checkingWorkflow()}
	handler, _ := NewHandler(
		workflows,
		fakeCheckExecutions{item: completedExecution()},
		&fakeCheckWorkspaces{item: workspace.Workspace{ID: "workspace-1", Status: workspace.StatusReady}},
		&fakeCheckStore{},
		staticDetector{err: errors.New("detector unavailable")},
		&sequenceCheckRunner{},
		nil,
		"worker-1",
		time.Minute,
	)

	if err := handler.HandleDurable(context.Background(), checkQueueJob(3, 3)); err == nil {
		t.Fatal("HandleDurable() unexpectedly succeeded")
	}
	if workflows.job.Status != workflowjob.StatusFailed || workflows.job.FailureCode != "CHECKS_HANDLER_FAILED" {
		t.Fatalf("workflow = %#v", workflows.job)
	}
}

func checkingWorkflow() workflowjob.Job {
	return workflowjob.Job{
		ID: "workflow-1", Status: workflowjob.StatusChecking,
		Version: 4, ExecutionVersion: 1,
	}
}

func completedExecution() execution.Execution {
	return execution.Execution{
		ID: "execution-1", WorkflowJobID: "workflow-1",
		ExecutionVersion: 1, Status: execution.StatusCompleted,
	}
}

func testProfiles() []Profile {
	return []Profile{
		{Name: "go.vet", Command: []string{"go", "vet", "./..."}, Timeout: time.Minute, OutputLimit: 1024},
		{Name: "go.test", Command: []string{"go", "test", "./..."}, Timeout: time.Minute, OutputLimit: 1024},
	}
}

func checkQueueJob(attempts int, maxAttempts int) jobqueue.Job {
	return jobqueue.Job{
		ID: "queue-1", Type: "workflow.run_checks",
		Attempts: attempts, MaxAttempts: maxAttempts,
		PayloadJSON: `{"workflow_job_id":"workflow-1","workflow_version":4,"action":"EXECUTION_COMPLETED","target_status":"CHECKING"}`,
	}
}

func prepareCheckGitRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runCheckGit(t, root, "init")
	runCheckGit(t, root, "config", "user.email", "test@example.com")
	runCheckGit(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCheckGit(t, root, "add", "README.md")
	runCheckGit(t, root, "commit", "-m", "initial")
	return root
}

func runCheckGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
