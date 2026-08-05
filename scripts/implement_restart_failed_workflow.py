from pathlib import Path


def read(path: str) -> str:
    return Path(path).read_text()


def write(path: str, content: str) -> None:
    Path(path).write_text(content)


def replace_once(path: str, old: str, new: str) -> None:
    content = read(path)
    if old not in content:
        raise SystemExit(f"expected text not found in {path}: {old[:120]!r}")
    if content.count(old) != 1:
        raise SystemExit(f"expected exactly one match in {path}, found {content.count(old)}")
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


replace_once(
    "internal/workflowjob/transition.go",
    r'''\tActionFail                 Action = "FAIL"
\tActionRetry                Action = "RETRY"
\tActionCancel               Action = "CANCEL"
\tActionContinueExecution    Action = "CONTINUE_EXECUTION"''',
    r'''\tActionFail                 Action = "FAIL"
\tActionRetry                Action = "RETRY"
\tActionRestart              Action = "RESTART"
\tActionCancel               Action = "CANCEL"
\tActionContinueExecution    Action = "CONTINUE_EXECUTION"''',
)
replace_once(
    "internal/workflowjob/transition.go",
    r'''\tif action == ActionContinueExecution {
\t\tif current != StatusFailed || (retryStatus != StatusExecuting && retryStatus != StatusQueued) {
\t\t\treturn "", invalidTransition(current, action)
\t\t}
\t\treturn StatusQueued, nil
\t}''',
    r'''\tif action == ActionRestart {
\t\tif current != StatusFailed {
\t\t\treturn "", invalidTransition(current, action)
\t\t}
\t\treturn StatusWorkspacePreparing, nil
\t}
\tif action == ActionContinueExecution {
\t\tif current != StatusFailed || (retryStatus != StatusExecuting && retryStatus != StatusQueued) {
\t\t\treturn "", invalidTransition(current, action)
\t\t}
\t\treturn StatusQueued, nil
\t}''',
)
replace_once(
    "internal/workflowjob/transition.go",
    r'''\tcase ActionStart:
\t\treturn "workflow.prepare_workspace", true''',
    r'''\tcase ActionStart, ActionRestart:
\t\treturn "workflow.prepare_workspace", true''',
)

replace_once(
    "internal/workflowjob/repository.go",
    r'''\tif input.Action == ActionRetry {
\t\tretryStatus = ""
\t\tfailureCode = ""
\t\tfailureMessage = ""
\t}
\tif input.Action == ActionContinueExecution {''',
    r'''\tif input.Action == ActionRetry || input.Action == ActionRestart {
\t\tretryStatus = ""
\t\tfailureCode = ""
\t\tfailureMessage = ""
\t}
\tif input.Action == ActionRestart {
\t\tcancellationRequested = false
\t\trevisionCount = 0
\t}
\tif input.Action == ActionContinueExecution {''',
)

create_once(
    "internal/workspace/restart_lease.go",
    r'''package workspace

import (
\t"context"
\t"database/sql"
\t"errors"
\t"fmt"
\t"strings"
\t"time"
)

// AcquireRestart reserves a failed workflow workspace for destructive reset.
// Active mutation leases are never stolen, even when they belong to the same daemon.
func (r *Repository) AcquireRestart(
\tctx context.Context,
\tworkspaceID string,
\towner string,
\tttl time.Duration,
) (Lease, error) {
\tworkspaceID = strings.TrimSpace(workspaceID)
\towner = strings.TrimSpace(owner)
\tif workspaceID == "" || owner == "" || ttl <= 0 {
\t\treturn Lease{}, fmt.Errorf(
\t\t\t"%w: workspace ID, lease owner, and positive TTL are required",
\t\t\tErrInvalidWorkspace,
\t\t)
\t}

\tnow := r.now().UTC()
\texpiresAt := now.Add(ttl)
\ttoken := newID()
\trow := r.db.QueryRowContext(ctx, `
\t\tUPDATE workspaces
\t\tSET lease_owner = ?, lease_token = ?, lease_expires_at = ?,
\t\t\tstatus = 'PREPARING', head_sha = NULL, dirty = 0,
\t\t\tfailure_message = NULL, updated_at = ?
\t\tWHERE id = ?
\t\t\tAND (
\t\t\t\t(
\t\t\t\t\tstatus IN ('REQUESTED', 'PREPARING', 'READY', 'FAILED')
\t\t\t\t\tAND (lease_token IS NULL OR lease_expires_at <= ?)
\t\t\t\t)
\t\t\t\tOR (status = 'WRITE_LOCKED' AND lease_expires_at <= ?)
\t\t\t)
\t\tRETURNING `+workspaceColumns+`
\t`, owner, token, expiresAt.UnixMilli(), now.UnixMilli(), workspaceID,
\t\tnow.UnixMilli(), now.UnixMilli())
\titem, err := scanWorkspace(row)
\tif errors.Is(err, sql.ErrNoRows) {
\t\tif _, getErr := r.getByID(ctx, workspaceID); errors.Is(getErr, ErrNotFound) {
\t\t\treturn Lease{}, ErrNotFound
\t\t}
\t\treturn Lease{}, ErrLeaseUnavailable
\t}
\tif err != nil {
\t\treturn Lease{}, fmt.Errorf("acquire workspace restart lease: %w", err)
\t}
\treturn Lease{Workspace: item, Token: token}, nil
}
''',
)

replace_once(
    "internal/workspace/handler.go",
    r'''\tAcquire(context.Context, string, string, time.Duration) (Lease, error)
\tRelease(context.Context, string, string) error''',
    r'''\tAcquire(context.Context, string, string, time.Duration) (Lease, error)
\tAcquireRestart(context.Context, string, string, time.Duration) (Lease, error)
\tRelease(context.Context, string, string) error''',
)
replace_once(
    "internal/workspace/handler.go",
    r'''\tPrepare(context.Context, string, string, string) (WorktreeSnapshot, error)
\tInspect(context.Context, string, string) (WorktreeSnapshot, error)''',
    r'''\tPrepare(context.Context, string, string, string) (WorktreeSnapshot, error)
\tInspect(context.Context, string, string) (WorktreeSnapshot, error)
\tRemove(context.Context, string, string) error''',
)
replace_once(
    "internal/workspace/handler.go",
    r'''\titem, source, err := h.workspaces.EnsureForWorkflow(ctx, workflow.ID)
\tif err != nil {
\t\treturn fmt.Errorf("ensure workflow workspace: %w", err)
\t}
\tif item.Status == StatusReady {''',
    r'''\titem, source, err := h.workspaces.EnsureForWorkflow(ctx, workflow.ID)
\tif err != nil {
\t\treturn fmt.Errorf("ensure workflow workspace: %w", err)
\t}
\tif payload.Action == workflowjob.ActionRestart {
\t\treturn h.restartWorkspace(ctx, workflow, item, source, queueJob)
\t}
\tif item.Status == StatusReady {''',
)
replace_once(
    "internal/workspace/handler.go",
    r'''func (h *PrepareHandler) advanceWorkflow(
\tctx context.Context,
\tworkflow workflowjob.Job,
\titem Workspace,
\tsnapshot WorktreeSnapshot,
) error {''',
    r'''func (h *PrepareHandler) restartWorkspace(
\tctx context.Context,
\tworkflow workflowjob.Job,
\titem Workspace,
\tsource SourceRepository,
\tqueueJob jobqueue.Job,
) error {
\tlease, err := h.workspaces.AcquireRestart(ctx, item.ID, h.owner, h.leaseTTL)
\tif err != nil {
\t\treturn fmt.Errorf("acquire workspace restart lease: %w", err)
\t}
\tdefer h.releaseLease(ctx, item.ID, lease.Token)

\tnow := h.now().UTC()
\tif err := h.record(ctx, workflow.ID, "workspace.restarting", map[string]any{
\t\t"workspace_id":     item.ID,
\t\t"repository_id":    workflow.RepositoryID,
\t\t"base_commit_sha":  workflow.BaseCommitSHA,
\t\t"workflow_version": workflow.Version,
\t\t"queue_attempt":    queueJob.Attempts,
\t}, now); err != nil {
\t\treturn err
\t}

\tif err := h.worktrees.Remove(ctx, source.LocalPath, item.Path); err != nil {
\t\th.persistFailure(ctx, workflow.ID, item.ID, lease.Token, err)
\t\treturn fmt.Errorf("remove previous Git worktree: %w", err)
\t}
\tsnapshot, err := h.worktrees.Prepare(
\t\tctx,
\t\tsource.LocalPath,
\t\titem.Path,
\t\tworkflow.BaseCommitSHA,
\t)
\tif err != nil {
\t\th.persistFailure(ctx, workflow.ID, item.ID, lease.Token, err)
\t\treturn fmt.Errorf("recreate isolated Git worktree: %w", err)
\t}
\tif snapshot.Dirty {
\t\terr := fmt.Errorf("restarted workspace is unexpectedly dirty")
\t\th.persistFailure(ctx, workflow.ID, item.ID, lease.Token, err)
\t\treturn err
\t}

\tready, err := h.workspaces.MarkReady(
\t\tctx,
\t\titem.ID,
\t\tlease.Token,
\t\tsnapshot.HeadSHA,
\t\tsnapshot.Dirty,
\t)
\tif err != nil {
\t\treturn fmt.Errorf("persist restarted workspace: %w", err)
\t}
\tif err := h.record(ctx, workflow.ID, "workspace.restarted", map[string]any{
\t\t"workspace_id":     item.ID,
\t\t"head_sha":         snapshot.HeadSHA,
\t\t"workflow_version": workflow.Version,
\t}, h.now().UTC()); err != nil {
\t\treturn err
\t}
\treturn h.advanceWorkflow(ctx, workflow, ready, snapshot)
}

func (h *PrepareHandler) advanceWorkflow(
\tctx context.Context,
\tworkflow workflowjob.Job,
\titem Workspace,
\tsnapshot WorktreeSnapshot,
) error {''',
)
replace_once(
    "internal/workspace/handler.go",
    r'''\t\tpayload.TargetStatus != workflowjob.StatusWorkspacePreparing ||
\t\t(payload.Action != workflowjob.ActionStart && payload.Action != workflowjob.ActionRetry) {''',
    r'''\t\tpayload.TargetStatus != workflowjob.StatusWorkspacePreparing ||
\t\t(payload.Action != workflowjob.ActionStart &&
\t\t\tpayload.Action != workflowjob.ActionRetry &&
\t\t\tpayload.Action != workflowjob.ActionRestart) {''',
)

replace_once(
    "internal/httpapi/workflow_jobs.go",
    r'''\tregisterWorkflowAction(api, registry, "/jobs/:jobID/retry", workflowjob.ActionRetry)
\tapi.POST("/jobs/:jobID/continue", func(c *gin.Context) {''',
    r'''\tregisterWorkflowAction(api, registry, "/jobs/:jobID/retry", workflowjob.ActionRetry)
\tregisterWorkflowAction(api, registry, "/jobs/:jobID/restart", workflowjob.ActionRestart)
\tapi.POST("/jobs/:jobID/continue", func(c *gin.Context) {''',
)

replace_once(
    "apps/tui/src/workflow-jobs.ts",
    r'''  action: "start" | "cancel" | "retry" | "approve" | "request-revision" | "reject" | "publish",''',
    r'''  action:
    | "start"
    | "cancel"
    | "retry"
    | "restart"
    | "approve"
    | "request-revision"
    | "reject"
    | "publish",''',
)
replace_once(
    "apps/tui/src/board-model.ts",
    r'''  | "retry"
  | "continue-8"''',
    r'''  | "retry"
  | "restart"
  | "continue-8"''',
)
replace_once(
    "apps/tui/src/board-model.ts",
    r'''    } else {
      actions.push({
        id: "retry",
        label: "Retry workflow",
        description: "Retry the failed stage using the workflow's current version.",
        tone: "warning",
      })
    }
  }

  if (isActiveWorkflow(item.workflow.status)) {''',
    r'''    } else {
      actions.push({
        id: "retry",
        label: "Retry workflow",
        description: "Retry only the failed stage using the current workspace.",
        tone: "warning",
      })
    }
    actions.push({
      id: "restart",
      label: "Restart from beginning",
      description:
        "Discard the current workspace changes and rerun this workflow from its pinned base commit. Existing evidence is preserved.",
      tone: "danger",
    })
  }

  if (isActiveWorkflow(item.workflow.status)) {''',
)
replace_once(
    "apps/tui/src/board-screen.tsx",
    r'''  const transitionWorkflow = async (item: BoardItem, action: "retry" | "cancel") => {
    if (!item.workflow || busy) return
    setBusy(true)
    setMessage(`${action === "retry" ? "Retrying" : "Cancelling"} workflow...`)''',
    r'''  const transitionWorkflow = async (
    item: BoardItem,
    action: "retry" | "restart" | "cancel",
  ) => {
    if (!item.workflow || busy) return
    setBusy(true)
    const actionLabel =
      action === "retry"
        ? "Retrying failed stage"
        : action === "restart"
          ? "Restarting workflow from its pinned base commit"
          : "Cancelling workflow"
    setMessage(`${actionLabel}...`)''',
)
replace_once(
    "apps/tui/src/board-screen.tsx",
    r'''      setMessage(`Workflow is now ${workflow.status.toLowerCase().replaceAll("_", " ")}.`)
      await reload()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : `Failed to ${action} the workflow`)''',
    r'''      setMessage(
        action === "restart"
          ? "Workspace reset started. The same workflow will run again from the beginning."
          : `Workflow is now ${workflow.status.toLowerCase().replaceAll("_", " ")}.`,
      )
      await reload()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : `Failed to ${action} the workflow`)''',
)
replace_once(
    "apps/tui/src/board-screen.tsx",
    r'''      case "retry":
        await transitionWorkflow(item, "retry")
        break
      case "continue-8":''',
    r'''      case "retry":
        await transitionWorkflow(item, "retry")
        break
      case "restart":
        await transitionWorkflow(item, "restart")
        break
      case "continue-8":''',
)
replace_once(
    "apps/tui/src/board-detail.tsx",
    r'''              ? "Inspect the partial diff and iteration timeline, press Esc, then choose Continue +8 or +16 turns."
              : "Fix the cause, press Esc, then choose Retry workflow."}''',
    r'''              ? "Inspect the partial diff, then choose Continue or Restart from beginning."
              : "Fix the cause and Retry the failed stage, or choose Restart from beginning."}''',
)

replace_once(
    "internal/workspace/handler_test.go",
    r'''\tacquireCalls int
\treadyCalls   int''',
    r'''\tacquireCalls int
\trestartCalls int
\treadyCalls   int''',
)
replace_once(
    "internal/workspace/handler_test.go",
    r'''func (f *fakeWorkspaceStore) Release(context.Context, string, string) error {''',
    r'''func (f *fakeWorkspaceStore) AcquireRestart(context.Context, string, string, time.Duration) (Lease, error) {
\tf.restartCalls++
\tif f.acquireErr != nil {
\t\treturn Lease{}, f.acquireErr
\t}
\treturn f.lease, nil
}

func (f *fakeWorkspaceStore) Release(context.Context, string, string) error {''',
)
replace_once(
    "internal/workspace/handler_test.go",
    r'''\tprepareCalls int
\tinspectCalls int
\tsnapshot     WorktreeSnapshot''',
    r'''\tprepareCalls int
\tinspectCalls int
\tremoveCalls  int
\tsnapshot     WorktreeSnapshot''',
)
replace_once(
    "internal/workspace/handler_test.go",
    r'''func (f *fakeWorktree) Inspect(context.Context, string, string) (WorktreeSnapshot, error) {
\tf.inspectCalls++
\treturn f.snapshot, f.err
}''',
    r'''func (f *fakeWorktree) Inspect(context.Context, string, string) (WorktreeSnapshot, error) {
\tf.inspectCalls++
\treturn f.snapshot, f.err
}

func (f *fakeWorktree) Remove(context.Context, string, string) error {
\tf.removeCalls++
\treturn f.err
}''',
)
append_once(
    "internal/workspace/handler_test.go",
    "func TestPrepareHandlerRestartsWorkspaceFromPinnedBase",
    r'''func TestPrepareHandlerRestartsWorkspaceFromPinnedBase(t *testing.T) {
\tworkflow := workflowjob.Job{
\t\tID: "workflow-1", RepositoryID: "repository-1", BaseCommitSHA: "abc123",
\t\tStatus: workflowjob.StatusWorkspacePreparing, Version: 8,
\t}
\tworkflows := &fakeWorkflowStore{job: workflow}
\tworkspaces := &fakeWorkspaceStore{
\t\titem: Workspace{
\t\t\tID: "workspace-1", WorkflowJobID: workflow.ID,
\t\t\tPath: "/tmp/workspace", Status: StatusReady, Dirty: true,
\t\t},
\t\tsource: SourceRepository{ID: "repository-1", LocalPath: "/tmp/source"},
\t\tlease: Lease{
\t\t\tWorkspace: Workspace{ID: "workspace-1", Status: StatusPreparing},
\t\t\tToken: "restart-token",
\t\t},
\t}
\tworktrees := &fakeWorktree{snapshot: WorktreeSnapshot{
\t\tPath: "/tmp/workspace", HeadSHA: "abc123", Dirty: false,
\t}}
\tactivities := &fakeWorkspaceActivities{}
\thandler, err := NewPrepareHandler(
\t\tworkflows,
\t\tworkspaces,
\t\tworktrees,
\t\tactivities,
\t\t"worker-1",
\t\ttime.Minute,
\t)
\tif err != nil {
\t\tt.Fatal(err)
\t}

\tif err := handler.Handle(context.Background(), restartQueueJob()); err != nil {
\t\tt.Fatalf("Handle() error = %v", err)
\t}
\tif workspaces.restartCalls != 1 || worktrees.removeCalls != 1 ||
\t\tworktrees.prepareCalls != 1 || workspaces.readyCalls != 1 {
\t\tt.Fatalf(
\t\t\t"calls restart=%d remove=%d prepare=%d ready=%d",
\t\t\tworkspaces.restartCalls,
\t\t\tworktrees.removeCalls,
\t\t\tworktrees.prepareCalls,
\t\t\tworkspaces.readyCalls,
\t\t)
\t}
\tif len(workflows.transitionCalls) != 1 ||
\t\tworkflows.transitionCalls[0].Action != workflowjob.ActionWorkspaceReady {
\t\tt.Fatalf("transition calls = %#v", workflows.transitionCalls)
\t}
\tif len(activities.items) != 3 ||
\t\tactivities.items[0].eventType != "workspace.restarting" ||
\t\tactivities.items[1].eventType != "workspace.restarted" ||
\t\tactivities.items[2].eventType != "workspace.ready" {
\t\tt.Fatalf("activities = %#v", activities.items)
\t}
}

func restartQueueJob() jobqueue.Job {
\treturn jobqueue.Job{
\t\tID: "queue-restart", Type: "workflow.prepare_workspace", Attempts: 1,
\t\tPayloadJSON: `{"workflow_job_id":"workflow-1","workflow_version":8,"action":"RESTART","target_status":"WORKSPACE_PREPARING"}`,
\t}
}''',
)

append_once(
    "internal/workspace/write_lease_test.go",
    "func TestRestartLeaseDoesNotStealActiveWriter",
    r'''func TestRestartLeaseDoesNotStealActiveWriter(t *testing.T) {
\tctx := context.Background()
\tdb, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tdefer db.Close()
\tif err := database.Migrate(ctx, db); err != nil {
\t\tt.Fatal(err)
\t}
\tseedWorkspaceWorkflow(t, db)

\trepository, err := NewRepository(db, filepath.Join(t.TempDir(), "workspaces"))
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tcurrent := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
\trepository.now = func() time.Time { return current }
\titem, _, err := repository.EnsureForWorkflow(ctx, "workflow-1")
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tprepare, err := repository.Acquire(ctx, item.ID, "prepare", time.Minute)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif _, err := repository.MarkReady(ctx, item.ID, prepare.Token, "abc123", true); err != nil {
\t\tt.Fatal(err)
\t}
\tif err := repository.Release(ctx, item.ID, prepare.Token); err != nil {
\t\tt.Fatal(err)
\t}

\twriter, err := repository.AcquireWrite(ctx, item.ID, "daemon", time.Minute)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif _, err := repository.AcquireRestart(ctx, item.ID, "daemon", time.Minute); !errors.Is(err, ErrLeaseUnavailable) {
\t\tt.Fatalf("active writer restart error = %v", err)
\t}

\tcurrent = current.Add(2 * time.Minute)
\trestart, err := repository.AcquireRestart(ctx, item.ID, "daemon", time.Minute)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif restart.Token == writer.Token || restart.Workspace.Status != StatusPreparing ||
\t\trestart.Workspace.HeadSHA != "" || restart.Workspace.Dirty {
\t\tt.Fatalf("restart lease = %#v", restart)
\t}
}''',
)

append_once(
    "internal/workflowjob/repository_test.go",
    "func TestRestartFailedWorkflowFromBeginning",
    r'''func TestRestartFailedWorkflowFromBeginning(t *testing.T) {
\trepository, _, db, _, input := openWorkflowRepository(t, "READY")
\tdefer db.Close()
\tctx := context.Background()

\tjob, err := repository.Create(ctx, input)
\tif err != nil {
\t\tt.Fatalf("Create() error = %v", err)
\t}
\tfor _, action := range []Action{
\t\tActionStart,
\t\tActionWorkspaceReady,
\t\tActionExecutionStarted,
\t\tActionExecutionCompleted,
\t\tActionChecksCompleted,
\t\tActionReviewCompleted,
\t\tActionRequestRevision,
\t\tActionQueueRevision,
\t\tActionExecutionStarted,
\t} {
\t\tjob, err = repository.Transition(ctx, job.ID, TransitionInput{
\t\t\tExpectedVersion: job.Version,
\t\t\tAction:          action,
\t\t})
\t\tif err != nil {
\t\t\tt.Fatalf("Transition(%s) error = %v", action, err)
\t\t}
\t}
\tfailed, err := repository.Transition(ctx, job.ID, TransitionInput{
\t\tExpectedVersion: job.Version,
\t\tAction:          ActionFail,
\t\tFailureCode:     "EXECUTOR_FAILED",
\t\tFailureMessage:  "implementation failed",
\t})
\tif err != nil {
\t\tt.Fatalf("fail error = %v", err)
\t}
\tif failed.ExecutionVersion != 2 || failed.RevisionCount != 1 {
\t\tt.Fatalf("failed = %#v", failed)
\t}

\trestarted, err := repository.Transition(ctx, failed.ID, TransitionInput{
\t\tExpectedVersion: failed.Version,
\t\tAction:          ActionRestart,
\t\tDetails:         map[string]any{"requested_by": "test"},
\t})
\tif err != nil {
\t\tt.Fatalf("restart error = %v", err)
\t}
\tif restarted.Status != StatusWorkspacePreparing || restarted.CurrentDispatchID == "" ||
\t\trestarted.RetryStatus != "" || restarted.FailureCode != "" ||
\t\trestarted.FailureMessage != "" || restarted.RevisionCount != 0 ||
\t\trestarted.ExecutionVersion != 2 || restarted.CancellationRequested {
\t\tt.Fatalf("restarted = %#v", restarted)
\t}

\tvar jobType, payloadJSON string
\tif err := db.QueryRowContext(ctx, `
\t\tSELECT type, payload_json FROM jobs WHERE id = ?
\t`, restarted.CurrentDispatchID).Scan(&jobType, &payloadJSON); err != nil {
\t\tt.Fatal(err)
\t}
\tif jobType != "workflow.prepare_workspace" ||
\t\t!strings.Contains(payloadJSON, `"action":"RESTART"`) {
\t\tt.Fatalf("dispatch type=%q payload=%s", jobType, payloadJSON)
\t}
}''',
)

append_once(
    "internal/httpapi/workflow_jobs_test.go",
    "func TestWorkflowRestartRoute",
    r'''func TestWorkflowRestartRoute(t *testing.T) {
\tregistry := &fakeWorkflowJobRegistry{job: testWorkflowJob()}
\trouter := workflowRouter(registry)

\trequest := httptest.NewRequest(
\t\thttp.MethodPost,
\t\t"/api/v1/jobs/workflow-1/restart",
\t\tstrings.NewReader(`{"expected_version":1,"details":{"requested_by":"board"}}`),
\t)
\trequest.Header.Set("content-type", "application/json")
\tresponse := httptest.NewRecorder()
\trouter.ServeHTTP(response, request)
\tif response.Code != http.StatusOK {
\t\tt.Fatalf("restart status = %d body = %s", response.Code, response.Body.String())
\t}
\tif registry.transitionJobID != "workflow-1" ||
\t\tregistry.transitionInput.Action != workflowjob.ActionRestart ||
\t\tregistry.transitionInput.ExpectedVersion != 1 ||
\t\tregistry.transitionInput.Details["requested_by"] != "board" {
\t\tt.Fatalf("transition input = %#v", registry.transitionInput)
\t}
}''',
)

replace_once(
    "apps/tui/src/board-model.test.ts",
    r''').toEqual(["open-details", "retry"])''',
    r''').toEqual(["open-details", "retry", "restart"])''',
)
replace_once(
    "apps/tui/src/board-model.test.ts",
    r'''    "continue-8",
    "continue-16",
  ])''',
    r'''    "continue-8",
    "continue-16",
    "restart",
  ])''',
)
append_once(
    "apps/tui/src/board-model.test.ts",
    'test("offers restart from beginning for every failed workflow"',
    r'''test("offers restart from beginning for every failed workflow", () => {
  const failed = createBoardItem(
    project,
    plan("READY"),
    workflow("FAILED", {
      retry_status: "CHECKING",
      failure_code: "CHECKS_FAILED",
    }),
  )
  const restart = boardActions(failed).find((action) => action.id === "restart")
  expect(restart?.label).toBe("Restart from beginning")
  expect(restart?.description).toContain("pinned base commit")
})''',
)

create_once(
    "docs/restart-failed-workflow.md",
    r'''# Restarting a failed workflow

Every failed workflow exposes **Restart from beginning** on the Board.

Restart keeps the existing workflow, plan version, provider/model snapshots, transitions, executions, checks, reviews, iteration history, and checkpoint evidence. It does not create another workflow record.

The restart transition performs these steps:

1. move the failed workflow back to `WORKSPACE_PREPARING`;
2. acquire a dedicated restart lease without stealing an active writer;
3. force-remove the old isolated worktree;
4. recreate the worktree at the workflow's pinned `base_commit_sha`;
5. reset the revision counter and clear failure state;
6. queue the Executor and create a new execution version when execution begins.

The current workspace changes are discarded. Previously persisted checkpoints and artifacts remain available for audit.

**Retry workflow** continues to retry only the failed stage with the current workspace. **Continue Executor** continues a paused Executor with extra turns. **Restart from beginning** is the explicit clean-slate option for the same workflow.
''',
)

print("restart failed workflow implementation applied")
