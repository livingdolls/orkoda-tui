package execution

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type failureWorkflowStore struct {
	jobs        map[string]workflowjob.Job
	transitions []workflowjob.TransitionInput
	err         error
}

func (f *failureWorkflowStore) Get(_ context.Context, id string) (workflowjob.Job, error) {
	job, ok := f.jobs[id]
	if !ok {
		return workflowjob.Job{}, workflowjob.ErrNotFound
	}
	return job, nil
}

func (f *failureWorkflowStore) Transition(
	_ context.Context,
	id string,
	input workflowjob.TransitionInput,
) (workflowjob.Job, error) {
	if f.err != nil {
		return workflowjob.Job{}, f.err
	}
	job, ok := f.jobs[id]
	if !ok {
		return workflowjob.Job{}, workflowjob.ErrNotFound
	}
	if job.Version != input.ExpectedVersion {
		return workflowjob.Job{}, workflowjob.ErrVersionConflict
	}
	f.transitions = append(f.transitions, input)
	job.Status = workflowjob.StatusFailed
	job.Version++
	job.FailureCode = input.FailureCode
	job.FailureMessage = input.FailureMessage
	f.jobs[id] = job
	return job, nil
}

func TestFailWorkflowPersistsExecutorFailureImmediately(t *testing.T) {
	store := &failureWorkflowStore{jobs: map[string]workflowjob.Job{
		"workflow-1": {
			ID: "workflow-1", Status: workflowjob.StatusExecuting, Version: 4,
		},
	}}
	handler := &Handler{workflows: store}
	cause := &persistedExecutorFailure{
		code: "EXECUTOR_FAILED", message: "provider returned an invalid response",
	}
	err := handler.failWorkflow(context.Background(), store.jobs["workflow-1"], jobqueue.Job{
		Attempts: 1, MaxAttempts: 3,
	}, cause)
	if err != nil {
		t.Fatalf("failWorkflow() error = %v", err)
	}
	if len(store.transitions) != 1 {
		t.Fatalf("transitions = %#v", store.transitions)
	}
	transition := store.transitions[0]
	if transition.Action != workflowjob.ActionFail ||
		transition.FailureCode != "EXECUTOR_FAILED" ||
		transition.FailureMessage != "provider returned an invalid response" {
		t.Fatalf("transition = %#v", transition)
	}
	if store.jobs["workflow-1"].Status != workflowjob.StatusFailed {
		t.Fatalf("workflow status = %s", store.jobs["workflow-1"].Status)
	}
}

func TestExecutionFailurePreservesStoredCodeAndMessage(t *testing.T) {
	err := executionFailure(Execution{
		FailureCode: "EXECUTOR_FAILED", FailureMessage: "stored failure",
	})
	code, message, paused := classifyExecutorError(err)
	if code != "EXECUTOR_FAILED" || message != "stored failure" || paused {
		t.Fatalf("classification = %q %q paused=%v", code, message, paused)
	}
}

func TestReconcileFailedWorkflowsRepairsOnlyCurrentExecutingExecution(t *testing.T) {
	db := openFailureRecoveryDB(t)
	defer db.Close()
	_, err := db.Exec(`
		INSERT INTO workflow_jobs (id, status, execution_version) VALUES
			('workflow-stuck', 'EXECUTING', 2),
			('workflow-failed', 'FAILED', 1),
			('workflow-running', 'EXECUTING', 1);
		INSERT INTO executions (
			id, workflow_job_id, execution_version, status,
			failure_code, failure_message, updated_at
		) VALUES
			('execution-old', 'workflow-stuck', 1, 'FAILED', 'OLD_FAILURE', 'old', 1),
			('execution-current', 'workflow-stuck', 2, 'FAILED', 'EXECUTOR_FAILED', 'current failure', 2),
			('execution-already-failed', 'workflow-failed', 1, 'FAILED', 'EXECUTOR_FAILED', 'done', 3),
			('execution-running', 'workflow-running', 1, 'RUNNING', NULL, NULL, 4)
	`)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	store := &failureWorkflowStore{jobs: map[string]workflowjob.Job{
		"workflow-stuck": {
			ID: "workflow-stuck", Status: workflowjob.StatusExecuting,
			Version: 7, ExecutionVersion: 2,
		},
		"workflow-failed": {
			ID: "workflow-failed", Status: workflowjob.StatusFailed,
			Version: 5, ExecutionVersion: 1,
		},
		"workflow-running": {
			ID: "workflow-running", Status: workflowjob.StatusExecuting,
			Version: 3, ExecutionVersion: 1,
		},
	}}

	recovered, err := repository.ReconcileFailedWorkflows(context.Background(), store)
	if err != nil {
		t.Fatalf("ReconcileFailedWorkflows() error = %v", err)
	}
	if recovered != 1 || len(store.transitions) != 1 {
		t.Fatalf("recovered=%d transitions=%#v", recovered, store.transitions)
	}
	transition := store.transitions[0]
	if transition.FailureCode != "EXECUTOR_FAILED" ||
		transition.FailureMessage != "current failure" ||
		transition.Details["recovered"] != true ||
		transition.Details["execution_id"] != "execution-current" {
		t.Fatalf("transition = %#v", transition)
	}
}

func TestReconcileFailedWorkflowsReturnsTransitionFailure(t *testing.T) {
	db := openFailureRecoveryDB(t)
	defer db.Close()
	_, err := db.Exec(`
		INSERT INTO workflow_jobs (id, status, execution_version)
		VALUES ('workflow-stuck', 'EXECUTING', 1);
		INSERT INTO executions (
			id, workflow_job_id, execution_version, status,
			failure_code, failure_message, updated_at
		) VALUES (
			'execution-1', 'workflow-stuck', 1, 'FAILED',
			'EXECUTOR_FAILED', 'failed', 1
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	repository, _ := NewRepository(db)
	store := &failureWorkflowStore{
		jobs: map[string]workflowjob.Job{
			"workflow-stuck": {
				ID: "workflow-stuck", Status: workflowjob.StatusExecuting, Version: 1,
			},
		},
		err: errors.New("database unavailable"),
	}
	if _, err := repository.ReconcileFailedWorkflows(context.Background(), store); err == nil {
		t.Fatal("expected reconciliation error")
	}
}

func openFailureRecoveryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE workflow_jobs (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			current_dispatch_id TEXT,
			execution_version INTEGER NOT NULL
		);
		CREATE TABLE executions (
			id TEXT PRIMARY KEY,
			workflow_job_id TEXT NOT NULL,
			execution_version INTEGER NOT NULL,
			status TEXT NOT NULL,
			failure_code TEXT,
			failure_message TEXT,
			updated_at INTEGER NOT NULL
		);
		CREATE TABLE jobs (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			status TEXT NOT NULL,
			last_error TEXT
		);
		CREATE TABLE workflow_job_transitions (
			workflow_job_id TEXT NOT NULL,
			action TEXT NOT NULL,
			workflow_version INTEGER NOT NULL,
			details_json TEXT NOT NULL DEFAULT '{}'
		)
	`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

func TestSettleFinalDispatchFailureMarksWorkflowFailed(t *testing.T) {
	store := &failureWorkflowStore{jobs: map[string]workflowjob.Job{
		"workflow-1": {
			ID: "workflow-1", Status: workflowjob.StatusExecuting, Version: 4,
			CurrentDispatchID: "dispatch-1",
		},
	}}
	handler := &Handler{workflows: store}
	settled, err := handler.settleFinalDispatchFailure(
		context.Background(),
		"workflow-1",
		jobqueue.Job{ID: "dispatch-1", Attempts: 3, MaxAttempts: 3},
		errors.New("load Executor settings: provider unavailable"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !settled || store.jobs["workflow-1"].Status != workflowjob.StatusFailed {
		t.Fatalf("settled=%v workflow=%#v", settled, store.jobs["workflow-1"])
	}
	if store.jobs["workflow-1"].FailureCode != "EXECUTOR_FAILED" {
		t.Fatalf("failure code = %q", store.jobs["workflow-1"].FailureCode)
	}
}

func TestSettleDispatchFailureWaitsForFinalAttemptAndRejectsStaleJob(t *testing.T) {
	store := &failureWorkflowStore{jobs: map[string]workflowjob.Job{
		"workflow-1": {
			ID: "workflow-1", Status: workflowjob.StatusExecuting, Version: 4,
			CurrentDispatchID: "dispatch-current",
		},
	}}
	handler := &Handler{workflows: store}
	settled, err := handler.settleFinalDispatchFailure(
		context.Background(), "workflow-1",
		jobqueue.Job{ID: "dispatch-current", Attempts: 1, MaxAttempts: 3},
		errors.New("temporary error"),
	)
	if err != nil || settled {
		t.Fatalf("non-final settled=%v error=%v", settled, err)
	}
	settled, err = handler.settleFinalDispatchFailure(
		context.Background(), "workflow-1",
		jobqueue.Job{ID: "dispatch-stale", Attempts: 3, MaxAttempts: 3},
		errors.New("stale error"),
	)
	if err != nil || settled || len(store.transitions) != 0 {
		t.Fatalf("stale settled=%v error=%v transitions=%#v", settled, err, store.transitions)
	}
}

func TestReconcileDeadExecutionDispatchesRepairsCurrentAndLegacyWorkflows(t *testing.T) {
	db := openFailureRecoveryDB(t)
	defer db.Close()
	_, err := db.Exec(`
		INSERT INTO workflow_jobs (
			id, status, version, current_dispatch_id, execution_version
		) VALUES
			('workflow-queued', 'QUEUED', 3, 'dispatch-queued', 1),
			('workflow-legacy', 'EXECUTING', 7, NULL, 2),
			('workflow-live', 'EXECUTING', 4, 'dispatch-live', 1);
		INSERT INTO jobs (id, type, status, last_error) VALUES
			('dispatch-queued', 'workflow.execute', 'DEAD', 'settings unavailable'),
			('dispatch-legacy', 'workflow.execute', 'DEAD', 'write lease failed'),
			('dispatch-live', 'workflow.execute', 'RUNNING', NULL);
		INSERT INTO workflow_job_transitions (
			workflow_job_id, action, workflow_version, details_json
		) VALUES
			('workflow-legacy', 'EXECUTION_STARTED', 7, '{"dispatch_job_id":"dispatch-legacy"}')
	`)
	if err != nil {
		t.Fatal(err)
	}
	repository, _ := NewRepository(db)
	store := &failureWorkflowStore{jobs: map[string]workflowjob.Job{
		"workflow-queued": {
			ID: "workflow-queued", Status: workflowjob.StatusQueued, Version: 3,
			CurrentDispatchID: "dispatch-queued", ExecutionVersion: 1,
		},
		"workflow-legacy": {
			ID: "workflow-legacy", Status: workflowjob.StatusExecuting, Version: 7,
			ExecutionVersion: 2,
		},
		"workflow-live": {
			ID: "workflow-live", Status: workflowjob.StatusExecuting, Version: 4,
			CurrentDispatchID: "dispatch-live", ExecutionVersion: 1,
		},
	}}

	recovered, err := repository.ReconcileDeadExecutionDispatches(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 2 || len(store.transitions) != 2 {
		t.Fatalf("recovered=%d transitions=%#v", recovered, store.transitions)
	}
	if store.jobs["workflow-queued"].Status != workflowjob.StatusFailed ||
		store.jobs["workflow-legacy"].Status != workflowjob.StatusFailed ||
		store.jobs["workflow-live"].Status != workflowjob.StatusExecuting {
		t.Fatalf("jobs = %#v", store.jobs)
	}
}
