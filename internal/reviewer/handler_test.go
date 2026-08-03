package reviewer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
	"github.com/livingdolls/orkoda-tui/internal/checks"
	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type fakeWorkflowStore struct {
	job         workflowjob.Job
	transitions []workflowjob.TransitionInput
}

func (f *fakeWorkflowStore) Get(context.Context, string) (workflowjob.Job, error) {
	return f.job, nil
}

func (f *fakeWorkflowStore) Transition(_ context.Context, _ string, input workflowjob.TransitionInput) (workflowjob.Job, error) {
	f.transitions = append(f.transitions, input)
	f.job.Version++
	if input.Action == workflowjob.ActionReviewCompleted {
		f.job.Status = workflowjob.StatusWaitingApproval
	} else if input.Action == workflowjob.ActionFail {
		f.job.Status = workflowjob.StatusFailed
	}
	return f.job, nil
}

type fakeExecutionStore struct {
	item        execution.Execution
	checkpoints []execution.Checkpoint
}

func (f *fakeExecutionStore) GetByVersion(context.Context, string, int) (execution.Execution, error) {
	return f.item, nil
}

func (f *fakeExecutionStore) ListCheckpoints(context.Context, string) ([]execution.Checkpoint, error) {
	return f.checkpoints, nil
}

type fakeCheckStore struct {
	run   checks.Run
	steps []checks.Step
}

func (f *fakeCheckStore) GetByVersion(context.Context, string, int) (checks.Run, error) {
	return f.run, nil
}

func (f *fakeCheckStore) ListSteps(context.Context, string) ([]checks.Step, error) {
	return f.steps, nil
}

type fakeSettingsStore struct {
	settings agentconfig.Settings
}

func (f *fakeSettingsStore) Get(context.Context, string) (agentconfig.Settings, error) {
	return f.settings, nil
}

type fakeContextSource struct{}

func (fakeContextSource) Build(
	context.Context,
	string,
	execution.Execution,
	execution.Checkpoint,
	checks.Run,
	[]checks.Step,
) (Context, ValidationContext, error) {
	return Context{
		Requirement:        "Implement the feature.",
		AcceptanceCriteria: []Criterion{{ID: "requirement.ac-1", Text: "Works."}},
		ExecutionVersion:   1,
		ChangedFiles:       []string{"internal/example.go"},
		Checks:             []CheckEvidence{{Profile: "go.test", Status: checks.StatusPassed}},
	}, ValidationContext{
		ChangedFiles: map[string]struct{}{"internal/example.go": {}},
		CriteriaRefs: map[string]struct{}{"requirement.ac-1": {}},
	}, nil
}

type fakeGateway struct {
	response llm.Response
	err      error
	calls    int
}

func (f *fakeGateway) Complete(context.Context, string, llm.Request) (llm.Response, error) {
	f.calls++
	return f.response, f.err
}

type fakeReviewStore struct {
	run       Run
	result    Result
	failCalls int
}

func (f *fakeReviewStore) CreateOrGet(context.Context, CreateInput) (Run, bool, error) {
	return f.run, f.run.Status == "", nil
}

func (f *fakeReviewStore) Get(context.Context, string) (Run, error) {
	return f.run, nil
}

func (f *fakeReviewStore) GetByVersion(context.Context, string, int) (Run, error) {
	return f.run, nil
}

func (f *fakeReviewStore) ListWorkflow(context.Context, string) ([]Run, error) {
	return []Run{f.run}, nil
}

func (f *fakeReviewStore) ListIssues(context.Context, string) ([]Issue, error) {
	return f.result.Issues, nil
}

func (f *fakeReviewStore) Start(context.Context, string) (Run, error) {
	f.run.Status = StatusRunning
	return f.run, nil
}

func (f *fakeReviewStore) Complete(_ context.Context, _ string, result Result, _ llm.Usage) (Run, error) {
	f.result = result
	f.run.Status = StatusCompleted
	f.run.Verdict = result.Verdict
	f.run.Summary = result.Summary
	f.run.TotalIssues = len(result.Issues)
	for _, issue := range result.Issues {
		if issue.Blocking {
			f.run.BlockingIssues++
		}
	}
	return f.run, nil
}

func (f *fakeReviewStore) Fail(context.Context, string, string, string, bool) error {
	f.failCalls++
	return nil
}

func TestHandlerCompletesReviewAndAdvancesWorkflow(t *testing.T) {
	workflowStore := &fakeWorkflowStore{job: reviewerWorkflow()}
	reviewStore := &fakeReviewStore{run: reviewSnapshot(StatusPending)}
	gateway := &fakeGateway{response: llm.Response{
		Content: `{"verdict":"APPROVE","summary":"Looks good.","issues":[]}`,
		Usage:   llm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}}
	handler := newTestHandler(t, workflowStore, reviewStore, gateway)
	if err := handler.HandleDurable(context.Background(), reviewerQueueJob(1, 3)); err != nil {
		t.Fatalf("HandleDurable() error = %v", err)
	}
	if gateway.calls != 1 || reviewStore.run.Status != StatusCompleted {
		t.Fatalf("gateway calls=%d review=%#v", gateway.calls, reviewStore.run)
	}
	if len(workflowStore.transitions) != 1 || workflowStore.transitions[0].Action != workflowjob.ActionReviewCompleted {
		t.Fatalf("transitions = %#v", workflowStore.transitions)
	}
}

func TestHandlerTreatsCompletedReviewAsIdempotent(t *testing.T) {
	workflowStore := &fakeWorkflowStore{job: reviewerWorkflow()}
	reviewStore := &fakeReviewStore{run: reviewSnapshot(StatusCompleted)}
	reviewStore.run.Verdict = VerdictApprove
	gateway := &fakeGateway{}
	handler := newTestHandler(t, workflowStore, reviewStore, gateway)
	if err := handler.HandleDurable(context.Background(), reviewerQueueJob(1, 3)); err != nil {
		t.Fatalf("HandleDurable() error = %v", err)
	}
	if gateway.calls != 0 || len(workflowStore.transitions) != 1 {
		t.Fatalf("gateway calls=%d transitions=%#v", gateway.calls, workflowStore.transitions)
	}
}

func TestHandlerFailsWorkflowAfterFinalInfrastructureAttempt(t *testing.T) {
	workflowStore := &fakeWorkflowStore{job: reviewerWorkflow()}
	reviewStore := &fakeReviewStore{run: reviewSnapshot(StatusPending)}
	gateway := &fakeGateway{err: errors.New("provider unavailable")}
	handler := newTestHandler(t, workflowStore, reviewStore, gateway)
	err := handler.HandleDurable(context.Background(), reviewerQueueJob(3, 3))
	if err == nil {
		t.Fatal("HandleDurable() expected an error")
	}
	if reviewStore.failCalls != 1 {
		t.Fatalf("fail calls = %d", reviewStore.failCalls)
	}
	if len(workflowStore.transitions) != 1 || workflowStore.transitions[0].Action != workflowjob.ActionFail {
		t.Fatalf("transitions = %#v", workflowStore.transitions)
	}
}

func newTestHandler(
	t *testing.T,
	workflowStore *fakeWorkflowStore,
	reviewStore *fakeReviewStore,
	gateway *fakeGateway,
) *Handler {
	t.Helper()
	handler, err := NewHandler(
		workflowStore,
		&fakeExecutionStore{
			item: execution.Execution{ID: "execution-1", ExecutionVersion: 1, Status: execution.StatusCompleted},
			checkpoints: []execution.Checkpoint{
				{ID: "checkpoint-1", ExecutionID: "execution-1", ChangedFilesJSON: json.RawMessage(`["internal/example.go"]`)},
			},
		},
		&fakeCheckStore{run: checks.Run{ID: "check-1", Status: checks.StatusPassed}},
		&fakeSettingsStore{settings: agentconfig.Settings{
			ProjectID: "project-1",
			Version:   2,
			Agents: []agentconfig.AgentConfig{
				{Role: agentconfig.RoleReviewer, Enabled: true, Temperature: 0.1, MaxOutputTokens: 4096},
			},
		}},
		reviewStore,
		fakeContextSource{},
		gateway,
		nil,
		"local-fake",
		"local-reviewer",
	)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func reviewerWorkflow() workflowjob.Job {
	return workflowjob.Job{
		ID:              "workflow-1",
		ProjectID:       "project-1",
		PlanVersionID:   "plan-version-1",
		Status:          workflowjob.StatusReviewing,
		Version:         5,
		ExecutionVersion: 1,
	}
}

func reviewSnapshot(status Status) Run {
	return Run{
		ID:                   "review-1",
		WorkflowJobID:        "workflow-1",
		ExecutionID:          "execution-1",
		ExecutionVersion:     1,
		CheckRunID:           "check-1",
		CheckpointID:         "checkpoint-1",
		AgentSettingsVersion: 2,
		Provider:             "local-fake",
		Model:                "local-reviewer",
		Status:               status,
	}
}

func reviewerQueueJob(attempts int, maxAttempts int) jobqueue.Job {
	return jobqueue.Job{
		ID:          "queue-1",
		Type:        "workflow.review",
		Attempts:    attempts,
		MaxAttempts: maxAttempts,
		PayloadJSON: `{"workflow_job_id":"workflow-1","workflow_version":5,"action":"CHECKS_COMPLETED","target_status":"REVIEWING"}`,
	}
}
