from pathlib import Path


def read(path: str) -> str:
    return Path(path).read_text()


def write(path: str, content: str) -> None:
    Path(path).write_text(content)


def replace_once(path: str, old: str, new: str) -> None:
    content = read(path)
    if content.count(old) != 1:
        raise SystemExit(f"expected one match in {path}, found {content.count(old)}: {old[:100]!r}")
    write(path, content.replace(old, new, 1))


def create_once(path: str, content: str) -> None:
    target = Path(path)
    if target.exists():
        raise SystemExit(f"file already exists: {path}")
    target.write_text(content)


replace_once(
    "internal/execution/llm_runner.go",
    '''type ExecutorPauseError struct {
\tCode    string
\tMessage string
}

func (e *ExecutorPauseError) Error() string {
\tif e == nil {
\t\treturn "executor paused"
\t}
\treturn e.Message
}
''',
    '''type ExecutorPauseError struct {
\tCode    string
\tMessage string
}

func (e *ExecutorPauseError) Error() string {
\tif e == nil {
\t\treturn "executor paused"
\t}
\treturn e.Message
}

type persistedExecutorFailure struct {
\tcode    string
\tmessage string
}

func (e *persistedExecutorFailure) Error() string {
\tif e == nil || strings.TrimSpace(e.message) == "" {
\t\treturn "Executor execution failed."
\t}
\treturn e.message
}

func executionFailure(item Execution) error {
\tcode := strings.TrimSpace(item.FailureCode)
\tif code == "" {
\t\tcode = "EXECUTOR_FAILED"
\t}
\tmessage := strings.TrimSpace(item.FailureMessage)
\tif message == "" {
\t\tmessage = "Executor execution failed."
\t}
\treturn &persistedExecutorFailure{code: code, message: message}
}
''',
)
replace_once(
    "internal/execution/llm_runner.go",
    '''func classifyExecutorError(err error) (code, message string, pause bool) {
\tvar pauseError *ExecutorPauseError
\tif errors.As(err, &pauseError) {
\t\treturn pauseError.Code, pauseError.Message, true
\t}
\treturn "EXECUTOR_FAILED", err.Error(), false
}
''',
    '''func classifyExecutorError(err error) (code, message string, pause bool) {
\tvar pauseError *ExecutorPauseError
\tif errors.As(err, &pauseError) {
\t\treturn pauseError.Code, pauseError.Message, true
\t}
\tvar persistedFailure *persistedExecutorFailure
\tif errors.As(err, &persistedFailure) {
\t\treturn persistedFailure.code, persistedFailure.Error(), false
\t}
\treturn "EXECUTOR_FAILED", err.Error(), false
}
''',
)

replace_once(
    "internal/execution/handler.go",
    '''\tif executionItem.Status == StatusFailed {
\t\treturn fmt.Errorf("execution %s is failed and requires workflow retry", executionItem.ID)
\t}
''',
    '''\tif executionItem.Status == StatusFailed {
\t\treturn h.failWorkflow(ctx, job, queueJob, executionFailure(executionItem))
\t}
''',
)
replace_once(
    "internal/execution/handler.go",
    "h.failWorkflowOnLastAttempt(context.WithoutCancel(ctx), job, queueJob, runErr)",
    "h.failWorkflow(context.WithoutCancel(ctx), job, queueJob, runErr)",
)
replace_once(
    "internal/execution/handler.go",
    "h.failWorkflowOnLastAttempt(context.WithoutCancel(ctx), job, queueJob, cancellation)",
    "h.failWorkflow(context.WithoutCancel(ctx), job, queueJob, cancellation)",
)
replace_once(
    "internal/execution/handler.go",
    "h.failWorkflowOnLastAttempt(context.WithoutCancel(ctx), job, queueJob, err)",
    "h.failWorkflow(context.WithoutCancel(ctx), job, queueJob, err)",
)
replace_once(
    "internal/execution/handler.go",
    '''func (h *Handler) failWorkflowOnLastAttempt(
\tctx context.Context,
\tjob workflowjob.Job,
\tqueueJob jobqueue.Job,
\tcause error,
) error {
\tif queueJob.Attempts < queueJob.MaxAttempts {
\t\treturn cause
\t}
\tcurrent, err := h.workflows.Get(ctx, job.ID)
\tif err != nil {
\t\treturn cause
\t}
\tif current.Status == workflowjob.StatusExecuting {
\t\tcode, message, _ := classifyExecutorError(cause)
\t\t_, _ = h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
\t\t\tExpectedVersion: current.Version,
\t\t\tAction:          workflowjob.ActionFail,
\t\t\tFailureCode:     code,
\t\t\tFailureMessage:  message,
\t\t\tDetails: map[string]any{
\t\t\t\t"attempt":      queueJob.Attempts,
\t\t\t\t"max_attempts": queueJob.MaxAttempts,
\t\t\t},
\t\t})
\t}
\treturn cause
}
''',
    '''func (h *Handler) failWorkflow(
\tctx context.Context,
\tjob workflowjob.Job,
\tqueueJob jobqueue.Job,
\tcause error,
) error {
\tcurrent, err := h.workflows.Get(ctx, job.ID)
\tif err != nil {
\t\treturn fmt.Errorf("load workflow after Executor failure: %w", err)
\t}
\tif current.Status != workflowjob.StatusExecuting {
\t\treturn nil
\t}
\tcode, message, _ := classifyExecutorError(cause)
\tupdated, err := h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
\t\tExpectedVersion: current.Version,
\t\tAction:          workflowjob.ActionFail,
\t\tFailureCode:     code,
\t\tFailureMessage:  message,
\t\tDetails: map[string]any{
\t\t\t"attempt":      queueJob.Attempts,
\t\t\t"max_attempts": queueJob.MaxAttempts,
\t\t\t"terminal":     true,
\t\t},
\t})
\tif err != nil {
\t\tif errors.Is(err, workflowjob.ErrVersionConflict) {
\t\t\tlatest, getErr := h.workflows.Get(ctx, current.ID)
\t\t\tif getErr == nil && latest.Status != workflowjob.StatusExecuting {
\t\t\t\treturn nil
\t\t\t}
\t\t}
\t\treturn fmt.Errorf("mark workflow failed after Executor failure: %w", err)
\t}
\th.record(ctx, updated.ID, "execution.failed", map[string]any{
\t\t"failure_code":    code,
\t\t"failure_message": message,
\t\t"attempt":         queueJob.Attempts,
\t}, time.Now().UTC())
\treturn nil
}
''',
)

create_once(
    "internal/execution/recovery.go",
    '''package execution

import (
\t"context"
\t"errors"
\t"fmt"
\t"strings"

\t"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type failedWorkflowStore interface {
\tGet(context.Context, string) (workflowjob.Job, error)
\tTransition(context.Context, string, workflowjob.TransitionInput) (workflowjob.Job, error)
}

type failedExecutionCandidate struct {
\texecutionID string
\tworkflowID  string
\tcode        string
\tmessage     string
}

// ReconcileFailedWorkflows repairs workflows left EXECUTING by older daemons
// after their current durable execution had already reached FAILED.
func (r *Repository) ReconcileFailedWorkflows(
\tctx context.Context,
\tworkflows failedWorkflowStore,
) (int, error) {
\tif workflows == nil {
\t\treturn 0, fmt.Errorf("workflow store is required")
\t}
\trows, err := r.db.QueryContext(ctx, `
\t\tSELECT e.id, e.workflow_job_id,
\t\t\tCOALESCE(NULLIF(TRIM(e.failure_code), ''), 'EXECUTOR_FAILED'),
\t\t\tCOALESCE(NULLIF(TRIM(e.failure_message), ''), 'Executor execution failed.')
\t\tFROM executions e
\t\tJOIN workflow_jobs w ON w.id = e.workflow_job_id
\t\tWHERE e.status = 'FAILED'
\t\t\tAND w.status = 'EXECUTING'
\t\t\tAND e.execution_version = w.execution_version
\t\tORDER BY e.updated_at ASC
\t`)
\tif err != nil {
\t\treturn 0, fmt.Errorf("list failed Executor workflows: %w", err)
\t}
\tcandidates := make([]failedExecutionCandidate, 0)
\tfor rows.Next() {
\t\tvar candidate failedExecutionCandidate
\t\tif err := rows.Scan(
\t\t\t&candidate.executionID,
\t\t\t&candidate.workflowID,
\t\t\t&candidate.code,
\t\t\t&candidate.message,
\t\t); err != nil {
\t\t\trows.Close()
\t\t\treturn 0, fmt.Errorf("scan failed Executor workflow: %w", err)
\t\t}
\t\tcandidates = append(candidates, candidate)
\t}
\tif err := rows.Close(); err != nil {
\t\treturn 0, fmt.Errorf("close failed Executor workflow rows: %w", err)
\t}
\tif err := rows.Err(); err != nil {
\t\treturn 0, fmt.Errorf("iterate failed Executor workflows: %w", err)
\t}

\trecovered := 0
\tfor _, candidate := range candidates {
\t\tcurrent, err := workflows.Get(ctx, candidate.workflowID)
\t\tif err != nil {
\t\t\treturn recovered, fmt.Errorf("load failed Executor workflow %s: %w", candidate.workflowID, err)
\t\t}
\t\tif current.Status != workflowjob.StatusExecuting {
\t\t\tcontinue
\t\t}
\t\tcode := strings.TrimSpace(candidate.code)
\t\tif code == "" {
\t\t\tcode = "EXECUTOR_FAILED"
\t\t}
\t\tmessage := strings.TrimSpace(candidate.message)
\t\tif message == "" {
\t\t\tmessage = "Executor execution failed."
\t\t}
\t\t_, err = workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
\t\t\tExpectedVersion: current.Version,
\t\t\tAction:          workflowjob.ActionFail,
\t\t\tFailureCode:     code,
\t\t\tFailureMessage:  message,
\t\t\tDetails: map[string]any{
\t\t\t\t"recovered":    true,
\t\t\t\t"execution_id": candidate.executionID,
\t\t\t},
\t\t})
\t\tif err != nil {
\t\t\tif errors.Is(err, workflowjob.ErrVersionConflict) ||
\t\t\t\terrors.Is(err, workflowjob.ErrInvalidTransition) {
\t\t\t\tlatest, getErr := workflows.Get(ctx, current.ID)
\t\t\t\tif getErr == nil && latest.Status != workflowjob.StatusExecuting {
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t}
\t\t\treturn recovered, fmt.Errorf("recover failed Executor workflow %s: %w", current.ID, err)
\t\t}
\t\trecovered++
\t}
\treturn recovered, nil
}
''',
)

replace_once(
    "cmd/api/main.go",
    '''\texecutionRepository, err := execution.NewRepository(db)
\tif err != nil {
\t\treturn err
\t}
\tcheckRepository, err := checks.NewRepository(db)
''',
    '''\texecutionRepository, err := execution.NewRepository(db)
\tif err != nil {
\t\treturn err
\t}
\trecoveredExecutorFailures, err := executionRepository.ReconcileFailedWorkflows(
\t\truntimeCtx,
\t\tworkflowJobRepository,
\t)
\tif err != nil {
\t\treturn err
\t}
\tif recoveredExecutorFailures > 0 {
\t\tlogger.Warn(
\t\t\t"recovered workflows with failed Executor executions",
\t\t\t"count",
\t\t\trecoveredExecutorFailures,
\t\t)
\t}
\tcheckRepository, err := checks.NewRepository(db)
''',
)

create_once(
    "internal/execution/failure_recovery_test.go",
    '''package execution

import (
\t"context"
\t"database/sql"
\t"errors"
\t"path/filepath"
\t"testing"

\t"github.com/livingdolls/orkoda-tui/internal/database"
\t"github.com/livingdolls/orkoda-tui/internal/jobqueue"
\t"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type failureWorkflowStore struct {
\tjobs        map[string]workflowjob.Job
\ttransitions []workflowjob.TransitionInput
\terr         error
}

func (f *failureWorkflowStore) Get(_ context.Context, id string) (workflowjob.Job, error) {
\tjob, ok := f.jobs[id]
\tif !ok {
\t\treturn workflowjob.Job{}, workflowjob.ErrNotFound
\t}
\treturn job, nil
}

func (f *failureWorkflowStore) Transition(
\t_ context.Context,
\tid string,
\tinput workflowjob.TransitionInput,
) (workflowjob.Job, error) {
\tif f.err != nil {
\t\treturn workflowjob.Job{}, f.err
\t}
\tjob, ok := f.jobs[id]
\tif !ok {
\t\treturn workflowjob.Job{}, workflowjob.ErrNotFound
\t}
\tif job.Version != input.ExpectedVersion {
\t\treturn workflowjob.Job{}, workflowjob.ErrVersionConflict
\t}
\tf.transitions = append(f.transitions, input)
\tjob.Status = workflowjob.StatusFailed
\tjob.Version++
\tjob.FailureCode = input.FailureCode
\tjob.FailureMessage = input.FailureMessage
\tf.jobs[id] = job
\treturn job, nil
}

func TestFailWorkflowPersistsExecutorFailureImmediately(t *testing.T) {
\tstore := &failureWorkflowStore{jobs: map[string]workflowjob.Job{
\t\t"workflow-1": {
\t\t\tID: "workflow-1", Status: workflowjob.StatusExecuting, Version: 4,
\t\t},
\t}}
\thandler := &Handler{workflows: store}
\tcause := &persistedExecutorFailure{
\t\tcode: "EXECUTOR_FAILED", message: "provider returned an invalid response",
\t}
\terr := handler.failWorkflow(context.Background(), store.jobs["workflow-1"], jobqueue.Job{
\t\tAttempts: 1, MaxAttempts: 3,
\t}, cause)
\tif err != nil {
\t\tt.Fatalf("failWorkflow() error = %v", err)
\t}
\tif len(store.transitions) != 1 {
\t\tt.Fatalf("transitions = %#v", store.transitions)
\t}
\ttransition := store.transitions[0]
\tif transition.Action != workflowjob.ActionFail ||
\t\ttransition.FailureCode != "EXECUTOR_FAILED" ||
\t\ttransition.FailureMessage != "provider returned an invalid response" {
\t\tt.Fatalf("transition = %#v", transition)
\t}
\tif store.jobs["workflow-1"].Status != workflowjob.StatusFailed {
\t\tt.Fatalf("workflow status = %s", store.jobs["workflow-1"].Status)
\t}
}

func TestExecutionFailurePreservesStoredCodeAndMessage(t *testing.T) {
\terr := executionFailure(Execution{
\t\tFailureCode: "EXECUTOR_FAILED", FailureMessage: "stored failure",
\t})
\tcode, message, paused := classifyExecutorError(err)
\tif code != "EXECUTOR_FAILED" || message != "stored failure" || paused {
\t\tt.Fatalf("classification = %q %q paused=%v", code, message, paused)
\t}
}

func TestReconcileFailedWorkflowsRepairsOnlyCurrentExecutingExecution(t *testing.T) {
\tdb := openFailureRecoveryDB(t)
\tdefer db.Close()
\t_, err := db.Exec(`
\t\tINSERT INTO workflow_jobs (id, status, execution_version) VALUES
\t\t\t('workflow-stuck', 'EXECUTING', 2),
\t\t\t('workflow-failed', 'FAILED', 1),
\t\t\t('workflow-running', 'EXECUTING', 1);
\t\tINSERT INTO executions (
\t\t\tid, workflow_job_id, execution_version, status,
\t\t\tfailure_code, failure_message, updated_at
\t\t) VALUES
\t\t\t('execution-old', 'workflow-stuck', 1, 'FAILED', 'OLD_FAILURE', 'old', 1),
\t\t\t('execution-current', 'workflow-stuck', 2, 'FAILED', 'EXECUTOR_FAILED', 'current failure', 2),
\t\t\t('execution-already-failed', 'workflow-failed', 1, 'FAILED', 'EXECUTOR_FAILED', 'done', 3),
\t\t\t('execution-running', 'workflow-running', 1, 'RUNNING', NULL, NULL, 4)
\t`)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\trepository, err := NewRepository(db)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tstore := &failureWorkflowStore{jobs: map[string]workflowjob.Job{
\t\t"workflow-stuck": {
\t\t\tID: "workflow-stuck", Status: workflowjob.StatusExecuting,
\t\t\tVersion: 7, ExecutionVersion: 2,
\t\t},
\t\t"workflow-failed": {
\t\t\tID: "workflow-failed", Status: workflowjob.StatusFailed,
\t\t\tVersion: 5, ExecutionVersion: 1,
\t\t},
\t\t"workflow-running": {
\t\t\tID: "workflow-running", Status: workflowjob.StatusExecuting,
\t\t\tVersion: 3, ExecutionVersion: 1,
\t\t},
\t}}

\trecovered, err := repository.ReconcileFailedWorkflows(context.Background(), store)
\tif err != nil {
\t\tt.Fatalf("ReconcileFailedWorkflows() error = %v", err)
\t}
\tif recovered != 1 || len(store.transitions) != 1 {
\t\tt.Fatalf("recovered=%d transitions=%#v", recovered, store.transitions)
\t}
\ttransition := store.transitions[0]
\tif transition.FailureCode != "EXECUTOR_FAILED" ||
\t\ttransition.FailureMessage != "current failure" ||
\t\ttransition.Details["recovered"] != true ||
\t\ttransition.Details["execution_id"] != "execution-current" {
\t\tt.Fatalf("transition = %#v", transition)
\t}
}

func TestReconcileFailedWorkflowsReturnsTransitionFailure(t *testing.T) {
\tdb := openFailureRecoveryDB(t)
\tdefer db.Close()
\t_, err := db.Exec(`
\t\tINSERT INTO workflow_jobs (id, status, execution_version)
\t\tVALUES ('workflow-stuck', 'EXECUTING', 1);
\t\tINSERT INTO executions (
\t\t\tid, workflow_job_id, execution_version, status,
\t\t\tfailure_code, failure_message, updated_at
\t\t) VALUES (
\t\t\t'execution-1', 'workflow-stuck', 1, 'FAILED',
\t\t\t'EXECUTOR_FAILED', 'failed', 1
\t\t)
\t`)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\trepository, _ := NewRepository(db)
\tstore := &failureWorkflowStore{
\t\tjobs: map[string]workflowjob.Job{
\t\t\t"workflow-stuck": {
\t\t\t\tID: "workflow-stuck", Status: workflowjob.StatusExecuting, Version: 1,
\t\t\t},
\t\t},
\t\terr: errors.New("database unavailable"),
\t}
\tif _, err := repository.ReconcileFailedWorkflows(context.Background(), store); err == nil {
\t\tt.Fatal("expected reconciliation error")
\t}
}

func openFailureRecoveryDB(t *testing.T) *sql.DB {
\tt.Helper()
\tdb, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "recovery.db"))
\tif err != nil {
\t\tt.Fatal(err)
\t}
\t_, err = db.Exec(`
\t\tCREATE TABLE workflow_jobs (
\t\t\tid TEXT PRIMARY KEY,
\t\t\tstatus TEXT NOT NULL,
\t\t\texecution_version INTEGER NOT NULL
\t\t);
\t\tCREATE TABLE executions (
\t\t\tid TEXT PRIMARY KEY,
\t\t\tworkflow_job_id TEXT NOT NULL,
\t\t\texecution_version INTEGER NOT NULL,
\t\t\tstatus TEXT NOT NULL,
\t\t\tfailure_code TEXT,
\t\t\tfailure_message TEXT,
\t\t\tupdated_at INTEGER NOT NULL
\t\t)
\t`)
\tif err != nil {
\t\tdb.Close()
\t\tt.Fatal(err)
\t}
\treturn db
}
''',
)

print("executor failure restart fix applied")
