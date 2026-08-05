from pathlib import Path


def read(path: str) -> str:
    return Path(path).read_text()


def write(path: str, content: str) -> None:
    Path(path).write_text(content)


def replace_once(path: str, old: str, new: str) -> None:
    content = read(path)
    if content.count(old) != 1:
        raise SystemExit(f"expected one match in {path}, found {content.count(old)}")
    write(path, content.replace(old, new, 1))


def append_once(path: str, marker: str, content: str) -> None:
    current = read(path)
    if marker in current:
        return
    write(path, current.rstrip() + "\n\n" + content.strip() + "\n")


def create_once(path: str, content: str) -> None:
    target = Path(path)
    if target.exists():
        raise SystemExit(f"file already exists: {path}")
    target.write_text(content)


create_once(
    "internal/jobqueue/startup_recovery.go",
    '''package jobqueue

import (
\t"context"
\t"fmt"
\t"time"
)

// RecoverInterrupted immediately requeues jobs left RUNNING by a previous
// daemon process. The process-wide instance lock guarantees that no other
// scheduler is alive when startup recovery runs.
func (q *Queue) RecoverInterrupted(ctx context.Context, now time.Time) ([]string, error) {
\trows, err := q.db.QueryContext(ctx, `
\t\tUPDATE jobs
\t\tSET status = 'QUEUED', locked_by = NULL, locked_at = NULL, updated_at = ?
\t\tWHERE status = 'RUNNING'
\t\tRETURNING id
\t`, now.UTC().UnixMilli())
\tif err != nil {
\t\treturn nil, fmt.Errorf("recover interrupted jobs: %w", err)
\t}
\tdefer rows.Close()
\tids := make([]string, 0)
\tfor rows.Next() {
\t\tvar id string
\t\tif err := rows.Scan(&id); err != nil {
\t\t\treturn nil, fmt.Errorf("scan interrupted job: %w", err)
\t\t}
\t\tids = append(ids, id)
\t}
\tif err := rows.Err(); err != nil {
\t\treturn nil, fmt.Errorf("iterate interrupted jobs: %w", err)
\t}
\treturn ids, nil
}
''',
)
create_once(
    "internal/jobqueue/startup_recovery_test.go",
    '''package jobqueue

import (
\t"context"
\t"path/filepath"
\t"testing"
\t"time"

\t"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestRecoverInterruptedRequeuesAllRunningJobsImmediately(t *testing.T) {
\tctx := context.Background()
\tdb, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tdefer db.Close()
\tif err := database.Migrate(ctx, db); err != nil {
\t\tt.Fatal(err)
\t}
\tqueue := New(db)
\tnow := time.Now().UTC().Truncate(time.Millisecond)
\tjob, err := queue.Enqueue(ctx, "workflow.execute", `{}`, 3, now)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tclaimed, err := queue.Claim(ctx, "local-daemon-old", now)
\tif err != nil || claimed == nil {
\t\tt.Fatalf("claim error=%v job=%#v", err, claimed)
\t}

\trecovered, err := queue.RecoverInterrupted(ctx, now.Add(time.Second))
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif len(recovered) != 1 || recovered[0] != job.ID {
\t\tt.Fatalf("recovered = %v", recovered)
\t}
\tnext, err := queue.Claim(ctx, "local-daemon-new", now.Add(time.Second))
\tif err != nil || next == nil || next.ID != job.ID || next.Attempts != 2 {
\t\tt.Fatalf("reclaimed error=%v job=%#v", err, next)
\t}
}
''',
)

create_once(
    "internal/workspace/startup_recovery.go",
    '''package workspace

import (
\t"context"
\t"fmt"
\t"time"
)

// RecoverDaemonLeases releases mutation leases owned by an earlier local
// daemon instance. Manual client leases are intentionally preserved.
func (r *Repository) RecoverDaemonLeases(ctx context.Context, now time.Time) ([]string, error) {
\trows, err := r.db.QueryContext(ctx, `
\t\tUPDATE workspaces
\t\tSET status = CASE WHEN status = 'WRITE_LOCKED' THEN 'READY' ELSE status END,
\t\t\tlease_owner = NULL, lease_token = NULL, lease_expires_at = NULL,
\t\t\tupdated_at = ?
\t\tWHERE lease_token IS NOT NULL
\t\t\tAND lease_owner LIKE 'local-daemon-%'
\t\tRETURNING id
\t`, now.UTC().UnixMilli())
\tif err != nil {
\t\treturn nil, fmt.Errorf("recover interrupted daemon workspace leases: %w", err)
\t}
\tdefer rows.Close()
\tids := make([]string, 0)
\tfor rows.Next() {
\t\tvar id string
\t\tif err := rows.Scan(&id); err != nil {
\t\t\treturn nil, fmt.Errorf("scan recovered daemon workspace lease: %w", err)
\t\t}
\t\tids = append(ids, id)
\t}
\tif err := rows.Err(); err != nil {
\t\treturn nil, fmt.Errorf("iterate recovered daemon workspace leases: %w", err)
\t}
\treturn ids, nil
}
''',
)
create_once(
    "internal/workspace/startup_recovery_test.go",
    '''package workspace

import (
\t"context"
\t"path/filepath"
\t"testing"
\t"time"

\t"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestRecoverDaemonLeasesPreservesManualLease(t *testing.T) {
\tctx := context.Background()
\tdb, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tdefer db.Close()
\tif err := database.Migrate(ctx, db); err != nil {
\t\tt.Fatal(err)
\t}
\tnow := time.Now().UTC().Truncate(time.Millisecond)
\t_, err = db.ExecContext(ctx, `
\t\tINSERT INTO workspaces (
\t\t\tid, workflow_job_id, project_id, repository_id, path,
\t\t\tbase_commit_sha, head_sha, status, dirty,
\t\t\tlease_owner, lease_token, lease_expires_at,
\t\t\tcreated_at, updated_at
\t\t) VALUES
\t\t\t('daemon', 'workflow-daemon', 'project', 'repository', '/tmp/daemon',
\t\t\t 'base', 'base', 'WRITE_LOCKED', 0,
\t\t\t 'local-daemon-123', 'daemon-token', ?, ?, ?),
\t\t\t('manual', 'workflow-manual', 'project', 'repository', '/tmp/manual',
\t\t\t 'base', 'base', 'WRITE_LOCKED', 0,
\t\t\t 'tui-client', 'manual-token', ?, ?, ?)
\t`, now.Add(time.Hour).UnixMilli(), now.UnixMilli(), now.UnixMilli(),
\t\tnow.Add(time.Hour).UnixMilli(), now.UnixMilli(), now.UnixMilli())
\tif err != nil {
\t\tt.Fatal(err)
\t}
\trepository, err := NewRepository(db, t.TempDir())
\tif err != nil {
\t\tt.Fatal(err)
\t}
\trecovered, err := repository.RecoverDaemonLeases(ctx, now.Add(time.Second))
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif len(recovered) != 1 || recovered[0] != "daemon" {
\t\tt.Fatalf("recovered = %v", recovered)
\t}
\tvar daemonStatus, daemonOwner, manualStatus, manualOwner string
\tif err := db.QueryRow(`SELECT status, COALESCE(lease_owner, '') FROM workspaces WHERE id = 'daemon'`).Scan(&daemonStatus, &daemonOwner); err != nil {
\t\tt.Fatal(err)
\t}
\tif err := db.QueryRow(`SELECT status, COALESCE(lease_owner, '') FROM workspaces WHERE id = 'manual'`).Scan(&manualStatus, &manualOwner); err != nil {
\t\tt.Fatal(err)
\t}
\tif daemonStatus != "READY" || daemonOwner != "" || manualStatus != "WRITE_LOCKED" || manualOwner != "tui-client" {
\t\tt.Fatalf("daemon=%s/%q manual=%s/%q", daemonStatus, daemonOwner, manualStatus, manualOwner)
\t}
}
''',
)

replace_once(
    "cmd/api/main.go",
    '''\tqueue := jobqueue.New(db)
\tworkflowJobRepository, err := workflowjob.NewRepository(db, queue, activityRecorder)''',
    '''\tqueue := jobqueue.New(db)
\trecoveredInterruptedJobs, err := queue.RecoverInterrupted(runtimeCtx, time.Now().UTC())
\tif err != nil {
\t\treturn err
\t}
\tif len(recoveredInterruptedJobs) > 0 {
\t\tlogger.Warn("recovered interrupted queue jobs", "count", len(recoveredInterruptedJobs))
\t}
\tworkflowJobRepository, err := workflowjob.NewRepository(db, queue, activityRecorder)''',
)
replace_once(
    "cmd/api/main.go",
    '''\tworkspaceRepository, err := workspace.NewRepository(db, workspaceRoot)
\tif err != nil {
\t\treturn err
\t}
\tif report, err := workspaceRepository.ReconcileOrphans(runtimeCtx); err != nil {''',
    '''\tworkspaceRepository, err := workspace.NewRepository(db, workspaceRoot)
\tif err != nil {
\t\treturn err
\t}
\trecoveredDaemonLeases, err := workspaceRepository.RecoverDaemonLeases(runtimeCtx, time.Now().UTC())
\tif err != nil {
\t\treturn err
\t}
\tif len(recoveredDaemonLeases) > 0 {
\t\tlogger.Warn("recovered interrupted daemon workspace leases", "count", len(recoveredDaemonLeases))
\t}
\tif report, err := workspaceRepository.ReconcileOrphans(runtimeCtx); err != nil {''',
)

# Completed execute jobs attached to active workflows are also terminally inconsistent.
replace_once(
    "internal/execution/recovery.go",
    '''type deadExecutionDispatch struct {
\tworkflowID string
\tdispatchID string
\tmessage    string
}''',
    '''type deadExecutionDispatch struct {
\tworkflowID string
\tdispatchID string
\tstatus     string
\tmessage    string
}''',
)
replace_once(
    "internal/execution/recovery.go",
    '''\t\tSELECT w.id, j.id,
\t\t\tCOALESCE(NULLIF(TRIM(j.last_error), ''), 'Executor dispatch exhausted all retries.')
\t\tFROM workflow_jobs w
\t\tJOIN jobs j ON j.id = w.current_dispatch_id
\t\tWHERE w.status IN ('QUEUED', 'EXECUTING')
\t\t\tAND j.type = 'workflow.execute'
\t\t\tAND j.status = 'DEAD' ''',
    '''\t\tSELECT w.id, j.id, j.status,
\t\t\tCOALESCE(
\t\t\t\tNULLIF(TRIM(j.last_error), ''),
\t\t\t\tCASE WHEN j.status = 'COMPLETED'
\t\t\t\t\tTHEN 'Executor dispatch completed without closing the workflow.'
\t\t\t\t\tELSE 'Executor dispatch exhausted all retries.' END
\t\t\t)
\t\tFROM workflow_jobs w
\t\tJOIN jobs j ON j.id = w.current_dispatch_id
\t\tWHERE w.status IN ('QUEUED', 'EXECUTING')
\t\t\tAND j.type = 'workflow.execute'
\t\t\tAND j.status IN ('DEAD', 'COMPLETED') ''',
)
replace_once(
    "internal/execution/recovery.go",
    '''\t\tif err := rows.Scan(&candidate.workflowID, &candidate.dispatchID, &candidate.message); err != nil {''',
    '''\t\tif err := rows.Scan(&candidate.workflowID, &candidate.dispatchID, &candidate.status, &candidate.message); err != nil {''',
)
replace_once(
    "internal/execution/recovery.go",
    '''\t\tvar jobType, status, message string
\t\terr := r.db.QueryRowContext(ctx, `
\t\t\tSELECT type, status,
\t\t\t\tCOALESCE(NULLIF(TRIM(last_error), ''), 'Executor dispatch exhausted all retries.')
\t\t\tFROM jobs WHERE id = ?
\t\t`, legacy.dispatchID).Scan(&jobType, &status, &message)''',
    '''\t\tvar jobType, status, message string
\t\terr := r.db.QueryRowContext(ctx, `
\t\t\tSELECT type, status,
\t\t\t\tCOALESCE(
\t\t\t\t\tNULLIF(TRIM(last_error), ''),
\t\t\t\t\tCASE WHEN status = 'COMPLETED'
\t\t\t\t\t\tTHEN 'Executor dispatch completed without closing the workflow.'
\t\t\t\t\t\tELSE 'Executor dispatch exhausted all retries.' END
\t\t\t\t)
\t\t\tFROM jobs WHERE id = ?
\t\t`, legacy.dispatchID).Scan(&jobType, &status, &message)''',
)
replace_once(
    "internal/execution/recovery.go",
    '''\t\tif jobType == "workflow.execute" && status == "DEAD" {
\t\t\tlegacy.message = message
\t\t\tcandidates[legacy.workflowID] = legacy
\t\t}''',
    '''\t\tif jobType == "workflow.execute" && (status == "DEAD" || status == "COMPLETED") {
\t\t\tlegacy.status = status
\t\t\tlegacy.message = message
\t\t\tcandidates[legacy.workflowID] = legacy
\t\t}''',
)
replace_once(
    "internal/execution/recovery.go",
    '''\t\t\tDetails: map[string]any{
\t\t\t\t"recovered":       true,
\t\t\t\t"dispatch_job_id": candidate.dispatchID,
\t\t\t\t"dispatch_dead":   true,
\t\t\t},''',
    '''\t\t\tDetails: map[string]any{
\t\t\t\t"recovered":       true,
\t\t\t\t"dispatch_job_id": candidate.dispatchID,
\t\t\t\t"dispatch_status": candidate.status,
\t\t\t},''',
)

replace_once(
    "internal/execution/failure_recovery_test.go",
    '''\t\t\t('workflow-legacy', 'EXECUTING', 7, NULL, 2),
\t\t\t('workflow-live', 'EXECUTING', 4, 'dispatch-live', 1);''',
    '''\t\t\t('workflow-legacy', 'EXECUTING', 7, NULL, 2),
\t\t\t('workflow-completed-dispatch', 'EXECUTING', 5, 'dispatch-completed', 1),
\t\t\t('workflow-live', 'EXECUTING', 4, 'dispatch-live', 1);''',
)
replace_once(
    "internal/execution/failure_recovery_test.go",
    '''\t\t\t('dispatch-legacy', 'workflow.execute', 'DEAD', 'write lease failed'),
\t\t\t('dispatch-live', 'workflow.execute', 'RUNNING', NULL);''',
    '''\t\t\t('dispatch-legacy', 'workflow.execute', 'DEAD', 'write lease failed'),
\t\t\t('dispatch-completed', 'workflow.execute', 'COMPLETED', NULL),
\t\t\t('dispatch-live', 'workflow.execute', 'RUNNING', NULL);''',
)
replace_once(
    "internal/execution/failure_recovery_test.go",
    '''\t\t"workflow-live": {
\t\t\tID: "workflow-live", Status: workflowjob.StatusExecuting, Version: 4,
\t\t\tCurrentDispatchID: "dispatch-live", ExecutionVersion: 1,
\t\t},''',
    '''\t\t"workflow-completed-dispatch": {
\t\t\tID: "workflow-completed-dispatch", Status: workflowjob.StatusExecuting, Version: 5,
\t\t\tCurrentDispatchID: "dispatch-completed", ExecutionVersion: 1,
\t\t},
\t\t"workflow-live": {
\t\t\tID: "workflow-live", Status: workflowjob.StatusExecuting, Version: 4,
\t\t\tCurrentDispatchID: "dispatch-live", ExecutionVersion: 1,
\t\t},''',
)
replace_once(
    "internal/execution/failure_recovery_test.go",
    '''\tif recovered != 2 || len(store.transitions) != 2 {
\t\tt.Fatalf("recovered=%d transitions=%#v", recovered, store.transitions)
\t}
\tif store.jobs["workflow-queued"].Status != workflowjob.StatusFailed ||
\t\tstore.jobs["workflow-legacy"].Status != workflowjob.StatusFailed ||
\t\tstore.jobs["workflow-live"].Status != workflowjob.StatusExecuting {''',
    '''\tif recovered != 3 || len(store.transitions) != 3 {
\t\tt.Fatalf("recovered=%d transitions=%#v", recovered, store.transitions)
\t}
\tif store.jobs["workflow-queued"].Status != workflowjob.StatusFailed ||
\t\tstore.jobs["workflow-legacy"].Status != workflowjob.StatusFailed ||
\t\tstore.jobs["workflow-completed-dispatch"].Status != workflowjob.StatusFailed ||
\t\tstore.jobs["workflow-live"].Status != workflowjob.StatusExecuting {''',
)

print("executor startup recovery hardening applied")
