from pathlib import Path


def read(path: str) -> str:
    return Path(path).read_text()


def write(path: str, content: str) -> None:
    Path(path).write_text(content)


def replace_once(path: str, old: str, new: str) -> None:
    content = read(path)
    count = content.count(old)
    if count != 1:
        raise SystemExit(f"expected one match in {path}, found {count}: {old[:120]!r}")
    write(path, content.replace(old, new, 1))


def append_once(path: str, marker: str, content: str) -> None:
    current = read(path)
    if marker in current:
        return
    write(path, current.rstrip() + "\n\n" + content.strip() + "\n")


replace_once(
    "internal/workflowjob/repository.go",
    '''\tif shouldDispatch {
\t\tpayload, err := json.Marshal(map[string]any{''',
    '''\tif shouldDispatch {
\t\tpayload, err := json.Marshal(map[string]any{''',
)
replace_once(
    "internal/workflowjob/repository.go",
    '''\t\tdispatchID = dispatch.ID
\t}

\tretryStatus := job.RetryStatus''',
    '''\t\tdispatchID = dispatch.ID
\t}
\t// EXECUTION_STARTED is handled by the workflow.execute job that was
\t// already recorded while the workflow was QUEUED. Keep that dispatch ID
\t// attached to the workflow until the stage finishes so stale jobs and dead
\t// dispatches can be identified reliably.
\tif input.Action == ActionExecutionStarted && dispatchID == "" {
\t\tdispatchID = job.CurrentDispatchID
\t}

\tretryStatus := job.RetryStatus''',
)

replace_once(
    "internal/execution/handler.go",
    '''func (h *Handler) HandleDurable(ctx context.Context, queueJob jobqueue.Job) error {''',
    '''func (h *Handler) HandleDurable(ctx context.Context, queueJob jobqueue.Job) (resultErr error) {''',
)
replace_once(
    "internal/execution/handler.go",
    '''\tif job.CancellationRequested {
\t\treturn context.Canceled
\t}

\tjob, err = h.ensureExecuting(ctx, job, payload, queueJob.ID)''',
    '''\tif job.CancellationRequested {
\t\treturn context.Canceled
\t}
\tif job.CurrentDispatchID != "" && job.CurrentDispatchID != queueJob.ID {
\t\t// A newer Retry, Continue, or Restart dispatch replaced this job.
\t\treturn nil
\t}
\tdefer func() {
\t\tif resultErr == nil || ctx.Err() != nil || errors.Is(resultErr, context.Canceled) {
\t\t\treturn
\t\t}
\t\tsettled, settleErr := h.settleFinalDispatchFailure(
\t\t\tcontext.WithoutCancel(ctx), payload.WorkflowJobID, queueJob, resultErr,
\t\t)
\t\tif settleErr != nil {
\t\t\tresultErr = errors.Join(resultErr, settleErr)
\t\t\treturn
\t\t}
\t\tif settled {
\t\t\t// The workflow now carries the durable error and exposes Retry/Restart.
\t\t\t// Completing the queue job avoids leaving a redundant DEAD dispatch.
\t\t\tresultErr = nil
\t\t}
\t}()

\tjob, err = h.ensureExecuting(ctx, job, payload, queueJob.ID)''',
)
replace_once(
    "internal/execution/handler.go",
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
}''',
    '''func (h *Handler) failWorkflow(
\tctx context.Context,
\tjob workflowjob.Job,
\tqueueJob jobqueue.Job,
\tcause error,
) error {
\t_, err := h.markWorkflowFailed(ctx, job.ID, queueJob, cause)
\treturn err
}

func (h *Handler) settleFinalDispatchFailure(
\tctx context.Context,
\tworkflowID string,
\tqueueJob jobqueue.Job,
\tcause error,
) (bool, error) {
\tif queueJob.Attempts < queueJob.MaxAttempts {
\t\treturn false, nil
\t}
\treturn h.markWorkflowFailed(ctx, workflowID, queueJob, cause)
}

func (h *Handler) markWorkflowFailed(
\tctx context.Context,
\tworkflowID string,
\tqueueJob jobqueue.Job,
\tcause error,
) (bool, error) {
\tcurrent, err := h.workflows.Get(ctx, workflowID)
\tif err != nil {
\t\treturn false, fmt.Errorf("load workflow after Executor failure: %w", err)
\t}
\tif current.Status != workflowjob.StatusExecuting && current.Status != workflowjob.StatusQueued {
\t\treturn false, nil
\t}
\tif current.CurrentDispatchID != "" && current.CurrentDispatchID != queueJob.ID {
\t\treturn false, nil
\t}
\tcode, message, paused := classifyExecutorError(cause)
\tif paused {
\t\treturn false, nil
\t}
\tupdated, err := h.workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
\t\tExpectedVersion: current.Version,
\t\tAction:          workflowjob.ActionFail,
\t\tFailureCode:     code,
\t\tFailureMessage:  message,
\t\tDetails: map[string]any{
\t\t\t"attempt":         queueJob.Attempts,
\t\t\t"max_attempts":    queueJob.MaxAttempts,
\t\t\t"terminal":        true,
\t\t\t"dispatch_job_id": queueJob.ID,
\t\t},
\t})
\tif err != nil {
\t\tif errors.Is(err, workflowjob.ErrVersionConflict) || errors.Is(err, workflowjob.ErrInvalidTransition) {
\t\t\tlatest, getErr := h.workflows.Get(ctx, current.ID)
\t\t\tif getErr == nil && latest.Status != workflowjob.StatusExecuting && latest.Status != workflowjob.StatusQueued {
\t\t\t\treturn false, nil
\t\t\t}
\t\t}
\t\treturn false, fmt.Errorf("mark workflow failed after Executor failure: %w", err)
\t}
\th.record(ctx, updated.ID, "execution.failed", map[string]any{
\t\t"failure_code":    code,
\t\t"failure_message": message,
\t\t"attempt":         queueJob.Attempts,
\t\t"dispatch_job_id": queueJob.ID,
\t}, time.Now().UTC())
\treturn true, nil
}''',
)

append_once(
    "internal/execution/recovery.go",
    "func (r *Repository) ReconcileDeadExecutionDispatches(",
    '''type deadExecutionDispatch struct {
\tworkflowID string
\tdispatchID string
\tmessage    string
}

// ReconcileDeadExecutionDispatches repairs workflows whose workflow.execute
// queue job exhausted all retries before the handler could persist FAILED.
// It also handles legacy EXECUTING rows where current_dispatch_id was cleared
// by EXECUTION_STARTED; the dispatch ID remains in that transition's details.
func (r *Repository) ReconcileDeadExecutionDispatches(
\tctx context.Context,
\tworkflows failedWorkflowStore,
) (int, error) {
\tif workflows == nil {
\t\treturn 0, fmt.Errorf("workflow store is required")
\t}
\tcandidates := make(map[string]deadExecutionDispatch)

\trows, err := r.db.QueryContext(ctx, `
\t\tSELECT w.id, j.id,
\t\t\tCOALESCE(NULLIF(TRIM(j.last_error), ''), 'Executor dispatch exhausted all retries.')
\t\tFROM workflow_jobs w
\t\tJOIN jobs j ON j.id = w.current_dispatch_id
\t\tWHERE w.status IN ('QUEUED', 'EXECUTING')
\t\t\tAND j.type = 'workflow.execute'
\t\t\tAND j.status = 'DEAD'
\t`)
\tif err != nil {
\t\treturn 0, fmt.Errorf("list dead current Executor dispatches: %w", err)
\t}
\tfor rows.Next() {
\t\tvar candidate deadExecutionDispatch
\t\tif err := rows.Scan(&candidate.workflowID, &candidate.dispatchID, &candidate.message); err != nil {
\t\t\trows.Close()
\t\t\treturn 0, fmt.Errorf("scan dead current Executor dispatch: %w", err)
\t\t}
\t\tcandidates[candidate.workflowID] = candidate
\t}
\tif err := rows.Close(); err != nil {
\t\treturn 0, fmt.Errorf("close dead current Executor dispatch rows: %w", err)
\t}
\tif err := rows.Err(); err != nil {
\t\treturn 0, fmt.Errorf("iterate dead current Executor dispatches: %w", err)
\t}

\tlegacyRows, err := r.db.QueryContext(ctx, `
\t\tSELECT w.id, t.details_json
\t\tFROM workflow_jobs w
\t\tJOIN workflow_job_transitions t
\t\t\tON t.workflow_job_id = w.id
\t\t\tAND t.workflow_version = w.version
\t\t\tAND t.action = 'EXECUTION_STARTED'
\t\tWHERE w.status = 'EXECUTING'
\t\t\tAND (w.current_dispatch_id IS NULL OR TRIM(w.current_dispatch_id) = '')
\t`)
\tif err != nil {
\t\treturn 0, fmt.Errorf("list legacy Executor dispatch transitions: %w", err)
\t}
\tfor legacyRows.Next() {
\t\tvar workflowID, detailsJSON string
\t\tif err := legacyRows.Scan(&workflowID, &detailsJSON); err != nil {
\t\t\tlegacyRows.Close()
\t\t\treturn 0, fmt.Errorf("scan legacy Executor dispatch transition: %w", err)
\t\t}
\t\tvar details struct {
\t\t\tDispatchJobID string `json:"dispatch_job_id"`
\t\t}
\t\tif err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
\t\t\tlegacyRows.Close()
\t\t\treturn 0, fmt.Errorf("decode legacy Executor dispatch transition: %w", err)
\t\t}
\t\tdispatchID := strings.TrimSpace(details.DispatchJobID)
\t\tif dispatchID == "" {
\t\t\tcontinue
\t\t}
\t\tvar jobType, status, message string
\t\terr := r.db.QueryRowContext(ctx, `
\t\t\tSELECT type, status,
\t\t\t\tCOALESCE(NULLIF(TRIM(last_error), ''), 'Executor dispatch exhausted all retries.')
\t\t\tFROM jobs WHERE id = ?
\t\t`, dispatchID).Scan(&jobType, &status, &message)
\t\tif errors.Is(err, sql.ErrNoRows) {
\t\t\tcontinue
\t\t}
\t\tif err != nil {
\t\t\tlegacyRows.Close()
\t\t\treturn 0, fmt.Errorf("load legacy Executor dispatch %s: %w", dispatchID, err)
\t\t}
\t\tif jobType == "workflow.execute" && status == "DEAD" {
\t\t\tcandidates[workflowID] = deadExecutionDispatch{
\t\t\t\tworkflowID: workflowID, dispatchID: dispatchID, message: message,
\t\t\t}
\t\t}
\t}
\tif err := legacyRows.Close(); err != nil {
\t\treturn 0, fmt.Errorf("close legacy Executor dispatch rows: %w", err)
\t}
\tif err := legacyRows.Err(); err != nil {
\t\treturn 0, fmt.Errorf("iterate legacy Executor dispatch transitions: %w", err)
\t}

\trecovered := 0
\tfor _, candidate := range candidates {
\t\tcurrent, err := workflows.Get(ctx, candidate.workflowID)
\t\tif err != nil {
\t\t\treturn recovered, fmt.Errorf("load workflow with dead Executor dispatch %s: %w", candidate.workflowID, err)
\t\t}
\t\tif current.Status != workflowjob.StatusExecuting && current.Status != workflowjob.StatusQueued {
\t\t\tcontinue
\t\t}
\t\t_, err = workflows.Transition(ctx, current.ID, workflowjob.TransitionInput{
\t\t\tExpectedVersion: current.Version,
\t\t\tAction:          workflowjob.ActionFail,
\t\t\tFailureCode:     "EXECUTOR_FAILED",
\t\t\tFailureMessage:  strings.TrimSpace(candidate.message),
\t\t\tDetails: map[string]any{
\t\t\t\t"recovered":       true,
\t\t\t\t"dispatch_job_id": candidate.dispatchID,
\t\t\t\t"dispatch_dead":   true,
\t\t\t},
\t\t})
\t\tif err != nil {
\t\t\tif errors.Is(err, workflowjob.ErrVersionConflict) || errors.Is(err, workflowjob.ErrInvalidTransition) {
\t\t\t\tlatest, getErr := workflows.Get(ctx, current.ID)
\t\t\t\tif getErr == nil && latest.Status != workflowjob.StatusExecuting && latest.Status != workflowjob.StatusQueued {
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t}
\t\t\treturn recovered, fmt.Errorf("recover dead Executor dispatch workflow %s: %w", current.ID, err)
\t\t}
\t\trecovered++
\t}
\treturn recovered, nil
}''',
)
replace_once(
    "internal/execution/recovery.go",
    '''import (
\t"context"
\t"errors"
\t"fmt"
\t"strings"''',
    '''import (
\t"context"
\t"database/sql"
\t"encoding/json"
\t"errors"
\t"fmt"
\t"strings"''',
)

replace_once(
    "cmd/api/main.go",
    '''\tif recoveredExecutorFailures > 0 {
\t\tlogger.Warn(
\t\t\t"recovered workflows with failed Executor executions",
\t\t\t"count",
\t\t\trecoveredExecutorFailures,
\t\t)
\t}
\tcheckRepository, err := checks.NewRepository(db)''',
    '''\tif recoveredExecutorFailures > 0 {
\t\tlogger.Warn(
\t\t\t"recovered workflows with failed Executor executions",
\t\t\t"count",
\t\t\trecoveredExecutorFailures,
\t\t)
\t}
\trecoveredDeadExecutorDispatches, err := executionRepository.ReconcileDeadExecutionDispatches(
\t\truntimeCtx,
\t\tworkflowJobRepository,
\t)
\tif err != nil {
\t\treturn err
\t}
\tif recoveredDeadExecutorDispatches > 0 {
\t\tlogger.Warn(
\t\t\t"recovered workflows with dead Executor dispatches",
\t\t\t"count",
\t\t\trecoveredDeadExecutorDispatches,
\t\t)
\t}
\tcheckRepository, err := checks.NewRepository(db)''',
)

append_once(
    "internal/workflowjob/repository_test.go",
    "func TestExecutionStartedPreservesCurrentDispatch",
    '''func TestExecutionStartedPreservesCurrentDispatch(t *testing.T) {
\trepository, _, db, _, input := openWorkflowRepository(t, "READY")
\tdefer db.Close()
\tctx := context.Background()

\tjob, err := repository.Create(ctx, input)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tjob, err = repository.Transition(ctx, job.ID, TransitionInput{
\t\tExpectedVersion: job.Version,
\t\tAction:          ActionStart,
\t})
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tjob, err = repository.Transition(ctx, job.ID, TransitionInput{
\t\tExpectedVersion: job.Version,
\t\tAction:          ActionWorkspaceReady,
\t})
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tdispatchID := job.CurrentDispatchID
\tif dispatchID == "" {
\t\tt.Fatal("queued workflow has no execute dispatch")
\t}

\tjob, err = repository.Transition(ctx, job.ID, TransitionInput{
\t\tExpectedVersion: job.Version,
\t\tAction:          ActionExecutionStarted,
\t\tDetails:         map[string]any{"dispatch_job_id": dispatchID},
\t})
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif job.Status != StatusExecuting || job.CurrentDispatchID != dispatchID {
\t\tt.Fatalf("job = %#v", job)
\t}
\ttransitions, err := repository.ListTransitions(ctx, job.ID)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tlast := transitions[len(transitions)-1]
\tif last.Action != ActionExecutionStarted || last.DispatchJobID != dispatchID {
\t\tt.Fatalf("last transition = %#v", last)
\t}
}''',
)

replace_once(
    "internal/execution/failure_recovery_test.go",
    '''\t\tCREATE TABLE workflow_jobs (
\t\t\tid TEXT PRIMARY KEY,
\t\t\tstatus TEXT NOT NULL,
\t\t\texecution_version INTEGER NOT NULL
\t\t);''',
    '''\t\tCREATE TABLE workflow_jobs (
\t\t\tid TEXT PRIMARY KEY,
\t\t\tstatus TEXT NOT NULL,
\t\t\tversion INTEGER NOT NULL DEFAULT 1,
\t\t\tcurrent_dispatch_id TEXT,
\t\t\texecution_version INTEGER NOT NULL
\t\t);''',
)
replace_once(
    "internal/execution/failure_recovery_test.go",
    '''\t\tCREATE TABLE executions (
\t\t\tid TEXT PRIMARY KEY,
\t\t\tworkflow_job_id TEXT NOT NULL,
\t\t\texecution_version INTEGER NOT NULL,
\t\t\tstatus TEXT NOT NULL,
\t\t\tfailure_code TEXT,
\t\t\tfailure_message TEXT,
\t\t\tupdated_at INTEGER NOT NULL
\t\t)
\t`)''',
    '''\t\tCREATE TABLE executions (
\t\t\tid TEXT PRIMARY KEY,
\t\t\tworkflow_job_id TEXT NOT NULL,
\t\t\texecution_version INTEGER NOT NULL,
\t\t\tstatus TEXT NOT NULL,
\t\t\tfailure_code TEXT,
\t\t\tfailure_message TEXT,
\t\t\tupdated_at INTEGER NOT NULL
\t\t);
\t\tCREATE TABLE jobs (
\t\t\tid TEXT PRIMARY KEY,
\t\t\ttype TEXT NOT NULL,
\t\t\tstatus TEXT NOT NULL,
\t\t\tlast_error TEXT
\t\t);
\t\tCREATE TABLE workflow_job_transitions (
\t\t\tworkflow_job_id TEXT NOT NULL,
\t\t\taction TEXT NOT NULL,
\t\t\tworkflow_version INTEGER NOT NULL,
\t\t\tdetails_json TEXT NOT NULL DEFAULT '{}'
\t\t)
\t`)''',
)
append_once(
    "internal/execution/failure_recovery_test.go",
    "func TestSettleFinalDispatchFailureMarksWorkflowFailed",
    '''func TestSettleFinalDispatchFailureMarksWorkflowFailed(t *testing.T) {
\tstore := &failureWorkflowStore{jobs: map[string]workflowjob.Job{
\t\t"workflow-1": {
\t\t\tID: "workflow-1", Status: workflowjob.StatusExecuting, Version: 4,
\t\t\tCurrentDispatchID: "dispatch-1",
\t\t},
\t}}
\thandler := &Handler{workflows: store}
\tsettled, err := handler.settleFinalDispatchFailure(
\t\tcontext.Background(),
\t\t"workflow-1",
\t\tjobqueue.Job{ID: "dispatch-1", Attempts: 3, MaxAttempts: 3},
\t\terrors.New("load Executor settings: provider unavailable"),
\t)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif !settled || store.jobs["workflow-1"].Status != workflowjob.StatusFailed {
\t\tt.Fatalf("settled=%v workflow=%#v", settled, store.jobs["workflow-1"])
\t}
\tif store.jobs["workflow-1"].FailureCode != "EXECUTOR_FAILED" {
\t\tt.Fatalf("failure code = %q", store.jobs["workflow-1"].FailureCode)
\t}
}

func TestSettleDispatchFailureWaitsForFinalAttemptAndRejectsStaleJob(t *testing.T) {
\tstore := &failureWorkflowStore{jobs: map[string]workflowjob.Job{
\t\t"workflow-1": {
\t\t\tID: "workflow-1", Status: workflowjob.StatusExecuting, Version: 4,
\t\t\tCurrentDispatchID: "dispatch-current",
\t\t},
\t}}
\thandler := &Handler{workflows: store}
\tsettled, err := handler.settleFinalDispatchFailure(
\t\tcontext.Background(), "workflow-1",
\t\tjobqueue.Job{ID: "dispatch-current", Attempts: 1, MaxAttempts: 3},
\t\terrors.New("temporary error"),
\t)
\tif err != nil || settled {
\t\tt.Fatalf("non-final settled=%v error=%v", settled, err)
\t}
\tsettled, err = handler.settleFinalDispatchFailure(
\t\tcontext.Background(), "workflow-1",
\t\tjobqueue.Job{ID: "dispatch-stale", Attempts: 3, MaxAttempts: 3},
\t\terrors.New("stale error"),
\t)
\tif err != nil || settled || len(store.transitions) != 0 {
\t\tt.Fatalf("stale settled=%v error=%v transitions=%#v", settled, err, store.transitions)
\t}
}

func TestReconcileDeadExecutionDispatchesRepairsCurrentAndLegacyWorkflows(t *testing.T) {
\tdb := openFailureRecoveryDB(t)
\tdefer db.Close()
\t_, err := db.Exec(`
\t\tINSERT INTO workflow_jobs (
\t\t\tid, status, version, current_dispatch_id, execution_version
\t\t) VALUES
\t\t\t('workflow-queued', 'QUEUED', 3, 'dispatch-queued', 1),
\t\t\t('workflow-legacy', 'EXECUTING', 7, NULL, 2),
\t\t\t('workflow-live', 'EXECUTING', 4, 'dispatch-live', 1);
\t\tINSERT INTO jobs (id, type, status, last_error) VALUES
\t\t\t('dispatch-queued', 'workflow.execute', 'DEAD', 'settings unavailable'),
\t\t\t('dispatch-legacy', 'workflow.execute', 'DEAD', 'write lease failed'),
\t\t\t('dispatch-live', 'workflow.execute', 'RUNNING', NULL);
\t\tINSERT INTO workflow_job_transitions (
\t\t\tworkflow_job_id, action, workflow_version, details_json
\t\t) VALUES
\t\t\t('workflow-legacy', 'EXECUTION_STARTED', 7, '{"dispatch_job_id":"dispatch-legacy"}')
\t`)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\trepository, _ := NewRepository(db)
\tstore := &failureWorkflowStore{jobs: map[string]workflowjob.Job{
\t\t"workflow-queued": {
\t\t\tID: "workflow-queued", Status: workflowjob.StatusQueued, Version: 3,
\t\t\tCurrentDispatchID: "dispatch-queued", ExecutionVersion: 1,
\t\t},
\t\t"workflow-legacy": {
\t\t\tID: "workflow-legacy", Status: workflowjob.StatusExecuting, Version: 7,
\t\t\tExecutionVersion: 2,
\t\t},
\t\t"workflow-live": {
\t\t\tID: "workflow-live", Status: workflowjob.StatusExecuting, Version: 4,
\t\t\tCurrentDispatchID: "dispatch-live", ExecutionVersion: 1,
\t\t},
\t}}

\trecovered, err := repository.ReconcileDeadExecutionDispatches(context.Background(), store)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif recovered != 2 || len(store.transitions) != 2 {
\t\tt.Fatalf("recovered=%d transitions=%#v", recovered, store.transitions)
\t}
\tif store.jobs["workflow-queued"].Status != workflowjob.StatusFailed ||
\t\tstore.jobs["workflow-legacy"].Status != workflowjob.StatusFailed ||
\t\tstore.jobs["workflow-live"].Status != workflowjob.StatusExecuting {
\t\tt.Fatalf("jobs = %#v", store.jobs)
\t}
}''',
)

print("restarted executor stall fix applied")
