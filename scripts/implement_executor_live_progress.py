from pathlib import Path


def read(path: str) -> str:
    return Path(path).read_text()


def write(path: str, content: str) -> None:
    Path(path).write_text(content)


def replace_once(path: str, old: str, new: str) -> None:
    content = read(path)
    if content.count(old) != 1:
        raise SystemExit(f"expected exactly one match in {path}, found {content.count(old)}: {old[:100]!r}")
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


# Stage wall-clock budgets must restart when a workflow enters a new stage.
replace_once(
    "internal/workflowjob/cancellation.go",
    '''// WithWallClock applies the aggregate's wall-clock budget to an active stage.
// Test doubles and legacy rows without a creation timestamp retain the parent
// context rather than receiving a year-1 deadline.
func WithWallClock(ctx context.Context, job Job) (context.Context, context.CancelFunc) {
\tif job.CreatedAt.IsZero() || job.Limits.WallClockSeconds <= 0 {
\t\treturn context.WithCancel(ctx)
\t}
\tdeadline := job.CreatedAt.Add(time.Duration(job.Limits.WallClockSeconds) * time.Second)
\treturn context.WithDeadline(ctx, deadline)
}''',
    '''// WithWallClock applies the workflow wall-clock budget to the current
// active stage. UpdatedAt is written by every durable transition, so Retry,
// Continue, Revision, and Restart receive a fresh stage deadline without
// rewriting the workflow's original creation timestamp.
func WithWallClock(ctx context.Context, job Job) (context.Context, context.CancelFunc) {
\tstartedAt := job.UpdatedAt
\tif startedAt.IsZero() {
\t\tstartedAt = job.CreatedAt
\t}
\tif startedAt.IsZero() || job.Limits.WallClockSeconds <= 0 {
\t\treturn context.WithCancel(ctx)
\t}
\tdeadline := startedAt.Add(time.Duration(job.Limits.WallClockSeconds) * time.Second)
\treturn context.WithDeadline(ctx, deadline)
}''',
)
replace_once(
    "internal/workflowjob/cancellation_test.go",
    '''func TestWithWallClockUsesAggregateCreationDeadline(t *testing.T) {
\tcreated := time.Now().Add(-2 * time.Second)
\tctx, cancel := WithWallClock(context.Background(), Job{
\t\tCreatedAt: created,
\t\tLimits:    Limits{WallClockSeconds: 1},
\t})
\tdefer cancel()
\tif err := ctx.Err(); err == nil || err != context.DeadlineExceeded {
\t\tt.Fatalf("context error = %v, want deadline exceeded", err)
\t}
}''',
    '''func TestWithWallClockUsesLatestStageTransition(t *testing.T) {
\tcreated := time.Now().Add(-2 * time.Hour)
\tupdated := time.Now()
\tctx, cancel := WithWallClock(context.Background(), Job{
\t\tCreatedAt: created,
\t\tUpdatedAt: updated,
\t\tLimits:    Limits{WallClockSeconds: 60},
\t})
\tdefer cancel()
\tif err := ctx.Err(); err != nil {
\t\tt.Fatalf("context error = %v, want active restarted stage", err)
\t}
\tdeadline, ok := ctx.Deadline()
\tif !ok || deadline.Before(updated.Add(59*time.Second)) {
\t\tt.Fatalf("deadline = %v, want based on UpdatedAt %v", deadline, updated)
\t}
}

func TestWithWallClockFallsBackToCreationTime(t *testing.T) {
\tcreated := time.Now().Add(-2 * time.Second)
\tctx, cancel := WithWallClock(context.Background(), Job{
\t\tCreatedAt: created,
\t\tLimits:    Limits{WallClockSeconds: 1},
\t})
\tdefer cancel()
\tif err := ctx.Err(); err == nil || err != context.DeadlineExceeded {
\t\tt.Fatalf("context error = %v, want deadline exceeded", err)
\t}
}''',
)

# Durable inactivity watchdog.
create_once(
    "internal/execution/watchdog.go",
    '''package execution

import (
\t"context"
\t"fmt"
\t"strings"
\t"sync"
\t"time"
)

const (
\tdefaultExecutorStallTimeout = 3 * time.Minute
\tExecutorStalledCode         = "EXECUTOR_STALLED"
)

type executorProgressWatchdog struct {
\tmu           sync.Mutex
\tlastProgress time.Time
\tphase        string
\tfailure      error
\tnow          func() time.Time
}

func newExecutorProgressWatchdog() *executorProgressWatchdog {
\treturn &executorProgressWatchdog{lastProgress: time.Now().UTC(), phase: "starting", now: time.Now}
}

func (w *executorProgressWatchdog) Mark(phase string) {
\tif w == nil {
\t\treturn
\t}
\tw.mu.Lock()
\tw.lastProgress = w.now().UTC()
\tif phase = strings.TrimSpace(phase); phase != "" {
\t\tw.phase = phase
\t}
\tw.mu.Unlock()
}

func (w *executorProgressWatchdog) Start(
\tctx context.Context,
\tcancel context.CancelFunc,
\ttimeout time.Duration,
) <-chan struct{} {
\tdone := make(chan struct{})
\tif timeout <= 0 {
\t\ttimeout = defaultExecutorStallTimeout
\t}
\tinterval := timeout / 4
\tif interval > 5*time.Second {
\t\tinterval = 5 * time.Second
\t}
\tif interval < 10*time.Millisecond {
\t\tinterval = 10 * time.Millisecond
\t}
\tgo func() {
\t\tdefer close(done)
\t\tticker := time.NewTicker(interval)
\t\tdefer ticker.Stop()
\t\tfor {
\t\t\tselect {
\t\t\tcase <-ctx.Done():
\t\t\t\treturn
\t\t\tcase <-ticker.C:
\t\t\t\tw.mu.Lock()
\t\t\t\tidle := w.now().UTC().Sub(w.lastProgress)
\t\t\t\tphase := w.phase
\t\t\t\tif idle >= timeout && w.failure == nil {
\t\t\t\t\tw.failure = &persistedExecutorFailure{
\t\t\t\t\t\tcode: ExecutorStalledCode,
\t\t\t\t\t\tmessage: fmt.Sprintf(
\t\t\t\t\t\t\t"Executor made no durable progress for %s while %s.",
\t\t\t\t\t\t\ttimeout.Round(time.Second),
\t\t\t\t\t\t\tphase,
\t\t\t\t\t\t),
\t\t\t\t\t}
\t\t\t\t\tw.mu.Unlock()
\t\t\t\t\tcancel()
\t\t\t\t\treturn
\t\t\t\t}
\t\t\t\tw.mu.Unlock()
\t\t\t}
\t\t}
\t}()
\treturn done
}

func (w *executorProgressWatchdog) Failure() error {
\tif w == nil {
\t\treturn nil
\t}
\tw.mu.Lock()
\tdefer w.mu.Unlock()
\treturn w.failure
}
''',
)
create_once(
    "internal/execution/watchdog_test.go",
    '''package execution

import (
\t"context"
\t"testing"
\t"time"
)

func TestExecutorProgressWatchdogCancelsStalledStage(t *testing.T) {
\tctx, cancel := context.WithCancel(context.Background())
\twatchdog := newExecutorProgressWatchdog()
\twatchdog.Mark("waiting for model response")
\tdone := watchdog.Start(ctx, cancel, 30*time.Millisecond)
\tselect {
\tcase <-done:
\tcase <-time.After(time.Second):
\t\tt.Fatal("watchdog did not stop")
\t}
\terr := watchdog.Failure()
\tcode, message, paused := classifyExecutorError(err)
\tif code != ExecutorStalledCode || paused || message == "" {
\t\tt.Fatalf("classification = %q %q paused=%v", code, message, paused)
\t}
}

func TestExecutorProgressWatchdogResetsOnProgress(t *testing.T) {
\tctx, cancel := context.WithCancel(context.Background())
\twatchdog := newExecutorProgressWatchdog()
\tdone := watchdog.Start(ctx, cancel, 80*time.Millisecond)
\tfor range 3 {
\t\ttime.Sleep(30 * time.Millisecond)
\t\twatchdog.Mark("tool completed")
\t}
\tif err := watchdog.Failure(); err != nil {
\t\tt.Fatalf("watchdog failed during progress: %v", err)
\t}
\tcancel()
\tselect {
\tcase <-done:
\tcase <-time.After(time.Second):
\t\tt.Fatal("watchdog did not stop after cancellation")
\t}
}
''',
)

# Progress callback carried by the runner context.
replace_once(
    "internal/execution/handler.go",
    '''type RunContext struct {
\tExecution Execution
\tWorkspace workspace.Workspace
\tTools     *RecordedTools
\tBudget    ExecutorBudget
}''',
    '''type RunContext struct {
\tExecution Execution
\tWorkspace workspace.Workspace
\tTools     *RecordedTools
\tBudget    ExecutorBudget
\tProgress  func(string, map[string]any)
}''',
)
replace_once(
    "internal/execution/handler.go",
    '''\tdefaultProvider string
\tdefaultModel    string
\tartifactStore   artifact.Store
}''',
    '''\tdefaultProvider string
\tdefaultModel    string
\tartifactStore   artifact.Store
\tstallTimeout    time.Duration
}''',
)
replace_once(
    "internal/execution/handler.go",
    '''\t\tdefaultProvider: defaultProvider, defaultModel: defaultModel,
\t\tartifactStore: artifactStore,
\t}, nil''',
    '''\t\tdefaultProvider: defaultProvider, defaultModel: defaultModel,
\t\tartifactStore: artifactStore,
\t\tstallTimeout: defaultExecutorStallTimeout,
\t}, nil''',
)

# Start the current-stage deadline/watchdog before setup work and expose safe progress events.
replace_once(
    "internal/execution/handler.go",
    '''\tif job.ExecutionVersion < 1 {
\t\treturn fmt.Errorf("workflow execution version is not initialized")
\t}

\titem, err := h.workspaces.GetByWorkflow(ctx, job.ID)''',
    '''\tif job.ExecutionVersion < 1 {
\t\treturn fmt.Errorf("workflow execution version is not initialized")
\t}

\trunCtx, cancel := workflowjob.WithWallClock(ctx, job)
\twatchdog := newExecutorProgressWatchdog()
\twatchdogDone := watchdog.Start(runCtx, cancel, h.stallTimeout)
\tdefer func() {
\t\tcancel()
\t\t<-watchdogDone
\t\tif failure := watchdog.Failure(); failure != nil && ctx.Err() == nil {
\t\t\tresultErr = failure
\t\t}
\t}()
\treportProgress := func(event string, payload map[string]any) {
\t\twatchdog.Mark(event)
\t\tdetails := map[string]any{
\t\t\t"execution_version": job.ExecutionVersion,
\t\t\t"dispatch_job_id":   queueJob.ID,
\t\t}
\t\tfor key, value := range payload {
\t\t\tdetails[key] = value
\t\t}
\t\th.record(runCtx, job.ID, event, details, time.Now().UTC())
\t}
\treportProgress("executor.dispatch.started", map[string]any{
\t\t"attempt": queueJob.Attempts,
\t\t"max_attempts": queueJob.MaxAttempts,
\t})

\titem, err := h.workspaces.GetByWorkflow(runCtx, job.ID)''',
)
replace_once(
    "internal/execution/handler.go",
    '''\tsettings, err := h.settings.Get(ctx, job.ProjectID)
\tif err != nil {
\t\treturn err
\t}
\tagent, policy, err := executorSnapshot(settings)''',
    '''\treportProgress("executor.workspace.ready", map[string]any{
\t\t"workspace_id": item.ID,
\t\t"workspace_status": item.Status,
\t})
\tsettings, err := h.settings.Get(runCtx, job.ProjectID)
\tif err != nil {
\t\treturn err
\t}
\tagent, policy, err := executorSnapshot(settings)''',
)
replace_once(
    "internal/execution/handler.go",
    '''\tif err != nil {
\t\treturn err
\t}
\tprovider := strings.TrimSpace(job.Executor.Provider)''',
    '''\tif err != nil {
\t\treturn err
\t}
\treportProgress("executor.settings.ready", map[string]any{
\t\t"agent_settings_version": settings.Version,
\t})
\tprovider := strings.TrimSpace(job.Executor.Provider)''',
)
replace_once(
    "internal/execution/handler.go",
    '''\texecutionItem, _, err := h.executions.CreateOrGet(ctx, CreateInput{''',
    '''\texecutionItem, _, err := h.executions.CreateOrGet(runCtx, CreateInput{''',
)
replace_once(
    "internal/execution/handler.go",
    '''\tif executionItem.Status == StatusFailed {
\t\treturn h.failWorkflow(ctx, job, queueJob, executionFailure(executionItem))
\t}

\tlease, err := h.workspaces.AcquireWrite(ctx, item.ID, h.workerID, h.leaseTTL)''',
    '''\tif executionItem.Status == StatusFailed {
\t\treturn h.failWorkflow(ctx, job, queueJob, executionFailure(executionItem))
\t}
\treportProgress("executor.execution.ready", map[string]any{
\t\t"execution_id": executionItem.ID,
\t\t"provider": executionItem.Provider,
\t\t"model": executionItem.Model,
\t})

\tlease, err := h.workspaces.AcquireWrite(runCtx, item.ID, h.workerID, h.leaseTTL)''',
)
replace_once(
    "internal/execution/handler.go",
    '''\treleased := false
\tdefer func() {''',
    '''\treportProgress("executor.lease.acquired", map[string]any{
\t\t"workspace_id": item.ID,
\t})
\treleased := false
\tdefer func() {''',
)
replace_once(
    "internal/execution/handler.go",
    '''\trunCtx, cancel := workflowjob.WithWallClock(ctx, job)
\tdefer cancel()
\tleaseErr := make(chan error, 1)''',
    '''\tleaseErr := make(chan error, 1)''',
)
replace_once(
    "internal/execution/handler.go",
    '''\texecutionItem, err = h.executions.Start(ctx, executionItem.ID)''',
    '''\texecutionItem, err = h.executions.Start(runCtx, executionItem.ID)''',
)
replace_once(
    "internal/execution/handler.go",
    '''\th.record(ctx, job.ID, "execution.started", map[string]any{
\t\t"execution_id":           executionItem.ID,
\t\t"execution_version":      executionItem.ExecutionVersion,
\t\t"agent_settings_version": executionItem.AgentSettingsVersion,
\t}, time.Now().UTC())''',
    '''\treportProgress("execution.started", map[string]any{
\t\t"execution_id":           executionItem.ID,
\t\t"agent_settings_version": executionItem.AgentSettingsVersion,
\t})''',
)
replace_once(
    "internal/execution/handler.go",
    '''\t\tBudget: ExecutorBudget{
\t\t\tMaxTurns:                 job.Limits.MaxExecutorTurns,
\t\t\tMaxConsecutiveToolErrors: job.Limits.MaxConsecutiveToolErrors,
\t\t\tMaxNoProgressTurns:       job.Limits.MaxNoProgressTurns,
\t\t},
\t})
\tdurableCancelled := false''',
    '''\t\tBudget: ExecutorBudget{
\t\t\tMaxTurns:                 job.Limits.MaxExecutorTurns,
\t\t\tMaxConsecutiveToolErrors: job.Limits.MaxConsecutiveToolErrors,
\t\t\tMaxNoProgressTurns:       job.Limits.MaxNoProgressTurns,
\t\t},
\t\tProgress: reportProgress,
\t})
\tif stalled := watchdog.Failure(); stalled != nil {
\t\trunErr = stalled
\t}
\tdurableCancelled := false''',
)
replace_once(
    "internal/execution/handler.go",
    '''func (h *Handler) settleFinalDispatchFailure(
\tctx context.Context,
\tworkflowID string,
\tqueueJob jobqueue.Job,
\tcause error,
) (bool, error) {
\tif queueJob.Attempts < queueJob.MaxAttempts {
\t\treturn false, nil
\t}
\treturn h.markWorkflowFailed(ctx, workflowID, queueJob, cause)
}''',
    '''func (h *Handler) settleFinalDispatchFailure(
\tctx context.Context,
\tworkflowID string,
\tqueueJob jobqueue.Job,
\tcause error,
) (bool, error) {
\tcode, _, _ := classifyExecutorError(cause)
\tif queueJob.Attempts < queueJob.MaxAttempts && code != ExecutorStalledCode {
\t\treturn false, nil
\t}
\treturn h.markWorkflowFailed(ctx, workflowID, queueJob, cause)
}''',
)

# Safe, durable progress events emitted around model and tool work.
replace_once(
    "internal/execution/llm_runner.go",
    '''func (r *LLMRunner) Run(ctx context.Context, run RunContext) error {
\tif err := r.repository.RecoverRunningIterations(ctx, run.Execution.ID); err != nil {''',
    '''func (r *LLMRunner) Run(ctx context.Context, run RunContext) error {
\treportExecutorProgress(run, "executor.context.selecting", map[string]any{
\t\t"status": "reading repository and plan context",
\t})
\tif err := r.repository.RecoverRunningIterations(ctx, run.Execution.ID); err != nil {''',
)
replace_once(
    "internal/execution/llm_runner.go",
    '''\tcontextJSON, err := json.Marshal(selected)
\tif err != nil {
\t\treturn fmt.Errorf("marshal executor context: %w", err)
\t}

\tbudget := normalizeExecutorBudget(run.Budget)''',
    '''\tcontextJSON, err := json.Marshal(selected)
\tif err != nil {
\t\treturn fmt.Errorf("marshal executor context: %w", err)
\t}
\treportExecutorProgress(run, "executor.context.ready", map[string]any{
\t\t"context_bytes": len(contextJSON),
\t})

\tbudget := normalizeExecutorBudget(run.Budget)''',
)
replace_once(
    "internal/execution/llm_runner.go",
    '''\t\tresponse, err := r.gateway.Complete(ctx, run.Execution.Provider, llm.Request{''',
    '''\t\treportExecutorProgress(run, "executor.turn.started", map[string]any{
\t\t\t"turn": index + 1,
\t\t\t"max_turns": budget.MaxTurns,
\t\t\t"status": "waiting for model response",
\t\t})
\t\tresponse, err := r.gateway.Complete(ctx, run.Execution.Provider, llm.Request{''',
)
replace_once(
    "internal/execution/llm_runner.go",
    '''\t\taction, err := decodeExecutorAction(response.Content)
\t\tif err != nil {
\t\t\treturn err
\t\t}

\t\tfingerprint := actionFingerprint(action)''',
    '''\t\taction, err := decodeExecutorAction(response.Content)
\t\tif err != nil {
\t\t\treturn err
\t\t}
\t\tactionSummary := safeActionSummary(action)
\t\treportExecutorProgress(run, "executor.turn.received", map[string]any{
\t\t\t"turn": index + 1,
\t\t\t"action_type": action.Type,
\t\t\t"tool": action.Tool,
\t\t\t"summary": actionSummary["summary"],
\t\t\t"path": actionSummary["path"],
\t\t\t"provider": firstNonEmpty(response.Usage.FinalProvider, run.Execution.Provider),
\t\t\t"model": firstNonEmpty(response.Usage.FinalModel, run.Execution.Model),
\t\t\t"total_tokens": response.Usage.TotalTokens,
\t\t})

\t\tfingerprint := actionFingerprint(action)''',
)
replace_once(
    "internal/execution/llm_runner.go",
    '''\t\tresult, summary, toolErr := executeAgentTool(ctx, run.Tools, action)
\t\tif toolErr != nil {''',
    '''\t\treportExecutorProgress(run, "executor.tool.started", map[string]any{
\t\t\t"turn": index + 1,
\t\t\t"tool": action.Tool,
\t\t\t"path": actionSummary["path"],
\t\t\t"query": actionSummary["query"],
\t\t})
\t\tresult, summary, toolErr := executeAgentTool(ctx, run.Tools, action)
\t\tif toolErr != nil {''',
)
replace_once(
    "internal/execution/llm_runner.go",
    '''\t\t\t_ = r.repository.FailIteration(context.WithoutCancel(ctx), iteration.ID, code, toolErr.Error())
\t\t\tledger = append(ledger, fmt.Sprintf("%d. %s failed: %s", index+1, actionLabel(action), boundText(toolErr.Error(), 320)))''',
    '''\t\t\t_ = r.repository.FailIteration(context.WithoutCancel(ctx), iteration.ID, code, toolErr.Error())
\t\t\treportExecutorProgress(run, "executor.tool.failed", map[string]any{
\t\t\t\t"turn": index + 1,
\t\t\t\t"tool": action.Tool,
\t\t\t\t"error_code": code,
\t\t\t\t"error": boundText(toolErr.Error(), 512),
\t\t\t})
\t\t\tledger = append(ledger, fmt.Sprintf("%d. %s failed: %s", index+1, actionLabel(action), boundText(toolErr.Error(), 320)))''',
)
replace_once(
    "internal/execution/llm_runner.go",
    '''\t\tif err := r.repository.CompleteIteration(ctx, iteration.ID, summary); err != nil {
\t\t\treturn err
\t\t}
\t\tconsecutiveToolErrors = 0''',
    '''\t\tif err := r.repository.CompleteIteration(ctx, iteration.ID, summary); err != nil {
\t\t\treturn err
\t\t}
\t\treportExecutorProgress(run, "executor.tool.completed", map[string]any{
\t\t\t"turn": index + 1,
\t\t\t"tool": action.Tool,
\t\t\t"result": summary,
\t\t})
\t\tconsecutiveToolErrors = 0''',
)
replace_once(
    "internal/execution/llm_runner.go",
    '''\tmessages := compactExecutorMessages(baseMessages, history, ledger)
\tmessages = append(messages, llm.Message{Role: llm.RoleUser, Content: "The tool budget is exhausted.''',
    '''\treportExecutorProgress(run, "executor.finalization.started", map[string]any{
\t\t"turn": turn,
\t\t"status": "waiting for final completion decision",
\t})
\tmessages := compactExecutorMessages(baseMessages, history, ledger)
\tmessages = append(messages, llm.Message{Role: llm.RoleUser, Content: "The tool budget is exhausted.''',
)
replace_once(
    "internal/execution/llm_runner.go",
    '''\tif action.Summary == "" || (action.Type != "finish" && action.Type != "needs_more_work") {
\t\treturn fmt.Errorf("executor finalization must be finish or needs_more_work with a summary")
\t}
\titeration, err := r.repository.BeginIteration''',
    '''\tif action.Summary == "" || (action.Type != "finish" && action.Type != "needs_more_work") {
\t\treturn fmt.Errorf("executor finalization must be finish or needs_more_work with a summary")
\t}
\treportExecutorProgress(run, "executor.finalization.received", map[string]any{
\t\t"turn": turn,
\t\t"decision": action.Type,
\t\t"summary": boundText(action.Summary, 512),
\t})
\titeration, err := r.repository.BeginIteration''',
)
append_once(
    "internal/execution/llm_runner.go",
    "func reportExecutorProgress(",
    '''func reportExecutorProgress(run RunContext, event string, payload map[string]any) {
\tif run.Progress == nil {
\t\treturn
\t}
\tdetails := map[string]any{
\t\t"execution_id": run.Execution.ID,
\t\t"execution_version": run.Execution.ExecutionVersion,
\t}
\tfor key, value := range payload {
\t\tif value != nil && value != "" {
\t\t\tdetails[key] = value
\t\t}
\t}
\trun.Progress(event, details)
}''',
)

# Runner progress regression test.
append_once(
    "internal/execution/llm_runner_test.go",
    "func TestLLMRunnerReportsLiveProgress",
    '''func TestLLMRunnerReportsLiveProgress(t *testing.T) {
\trunner, repository, run := newLLMRunnerFixture(t, []string{
\t\t`{"type":"finish","summary":"done"}`,
\t})
\tevents := make([]string, 0)
\trun.Progress = func(event string, _ map[string]any) {
\t\tevents = append(events, event)
\t}
\tif err := runner.Run(context.Background(), run); err != nil {
\t\tt.Fatalf("Run() error = %v", err)
\t}
\t_ = repository
\twant := []string{
\t\t"executor.context.selecting",
\t\t"executor.context.ready",
\t\t"executor.turn.started",
\t\t"executor.turn.received",
\t}
\tfor _, expected := range want {
\t\tfound := false
\t\tfor _, event := range events {
\t\t\tif event == expected {
\t\t\t\tfound = true
\t\t\t\tbreak
\t\t\t}
\t\t}
\t\tif !found {
\t\t\tt.Fatalf("events %v do not contain %q", events, expected)
\t\t}
\t}
}''',
)

# TUI formatting for safe structured live output.
create_once(
    "apps/tui/src/agent-live.ts",
    '''import type { ActivityEvent } from "./events"

export type AgentLiveLine = {
  title: string
  detail: string
  tone: "neutral" | "warning" | "success" | "danger"
}

export function isAgentLiveEvent(event: ActivityEvent): boolean {
  return event.type.startsWith("executor.") || event.type.startsWith("execution.")
}

export function formatAgentLiveEvent(event: ActivityEvent): AgentLiveLine {
  const payload = event.payload ?? {}
  const turn = numberValue(payload.turn)
  const tool = textValue(payload.tool)
  const path = textValue(payload.path)
  const summary = textValue(payload.summary)
  const status = textValue(payload.status)
  const error = textValue(payload.error) || textValue(payload.failure_message)

  switch (event.type) {
    case "executor.dispatch.started":
      return { title: "Executor dispatch started", detail: attemptDetail(payload), tone: "neutral" }
    case "executor.workspace.ready":
      return { title: "Workspace ready", detail: textValue(payload.workspace_status) || "isolated workspace loaded", tone: "success" }
    case "executor.settings.ready":
      return { title: "Agent settings loaded", detail: versionDetail(payload), tone: "success" }
    case "executor.execution.ready":
      return { title: "Execution initialized", detail: providerDetail(payload), tone: "success" }
    case "executor.lease.acquired":
      return { title: "Workspace lease acquired", detail: "Executor has exclusive write access", tone: "success" }
    case "executor.context.selecting":
      return { title: "Preparing context", detail: status || "reading repository and plan context", tone: "neutral" }
    case "executor.context.ready":
      return { title: "Context ready", detail: byteDetail(payload.context_bytes), tone: "success" }
    case "executor.turn.started":
      return { title: `Turn ${turn || "?"} · waiting for model`, detail: status || "request sent", tone: "warning" }
    case "executor.turn.received":
      return {
        title: `Turn ${turn || "?"} · ${tool || textValue(payload.action_type) || "response"}`,
        detail: [path, summary, tokenDetail(payload)].filter(Boolean).join(" · ") || "structured action received",
        tone: "neutral",
      }
    case "executor.tool.started":
      return { title: `Running ${tool || "tool"}`, detail: path || textValue(payload.query) || `turn ${turn || "?"}`, tone: "warning" }
    case "executor.tool.completed":
      return { title: `${tool || "Tool"} completed`, detail: resultDetail(payload.result), tone: "success" }
    case "executor.tool.failed":
      return { title: `${tool || "Tool"} failed`, detail: [textValue(payload.error_code), error].filter(Boolean).join(": "), tone: "danger" }
    case "executor.finalization.started":
      return { title: "Finalizing implementation", detail: status || "waiting for completion decision", tone: "warning" }
    case "executor.finalization.received":
      return { title: `Final decision · ${textValue(payload.decision) || "received"}`, detail: summary, tone: textValue(payload.decision) === "finish" ? "success" : "warning" }
    case "execution.started":
      return { title: "Executor running", detail: providerDetail(payload), tone: "warning" }
    case "execution.completed":
      return { title: "Implementation completed", detail: changedFilesDetail(payload), tone: "success" }
    case "execution.failed":
      return { title: "Executor failed", detail: error || textValue(payload.failure_code), tone: "danger" }
    case "execution.paused":
      return { title: "Executor paused", detail: error || textValue(payload.failure_code), tone: "warning" }
    default:
      return { title: event.type.replaceAll(".", " "), detail: summary || status || error || "progress recorded", tone: "neutral" }
  }
}

function textValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : ""
}

function numberValue(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined
}

function attemptDetail(payload: Record<string, unknown>): string {
  const attempt = numberValue(payload.attempt)
  const max = numberValue(payload.max_attempts)
  return attempt && max ? `attempt ${attempt}/${max}` : "dispatch accepted"
}

function versionDetail(payload: Record<string, unknown>): string {
  const version = numberValue(payload.agent_settings_version)
  return version ? `settings v${version}` : "Executor profile resolved"
}

function providerDetail(payload: Record<string, unknown>): string {
  return [textValue(payload.provider), textValue(payload.model)].filter(Boolean).join(" / ") || "execution record ready"
}

function byteDetail(value: unknown): string {
  const bytes = numberValue(value)
  return bytes === undefined ? "repository context selected" : `${bytes.toLocaleString()} context bytes`
}

function tokenDetail(payload: Record<string, unknown>): string {
  const tokens = numberValue(payload.total_tokens)
  return tokens === undefined ? "" : `${tokens.toLocaleString()} tokens`
}

function resultDetail(value: unknown): string {
  if (!value || typeof value !== "object") return "tool result persisted"
  const record = value as Record<string, unknown>
  const path = textValue(record.path)
  const matches = numberValue(record.matches)
  const bytes = numberValue(record.bytes)
  if (path) return path
  if (matches !== undefined) return `${matches} matches`
  if (bytes !== undefined) return `${bytes.toLocaleString()} bytes`
  return "tool result persisted"
}

function changedFilesDetail(payload: Record<string, unknown>): string {
  const count = numberValue(payload.changed_file_count)
  return count === undefined ? "checkpoint saved" : `${count} changed file(s)`
}
''',
)
create_once(
    "apps/tui/src/agent-live.test.ts",
    '''import { expect, test } from "bun:test"

import { formatAgentLiveEvent, isAgentLiveEvent } from "./agent-live"
import type { ActivityEvent } from "./events"

function event(type: string, payload: Record<string, unknown> = {}): ActivityEvent {
  return { sequence: 1, job_id: "workflow-1", type, payload, created_at: "2026-08-05T09:00:00Z" }
}

test("formats a waiting model turn without exposing raw prompts", () => {
  const line = formatAgentLiveEvent(event("executor.turn.started", { turn: 3, status: "waiting for model response" }))
  expect(line.title).toBe("Turn 3 · waiting for model")
  expect(line.detail).toBe("waiting for model response")
  expect(line.tone).toBe("warning")
})

test("formats structured tool output", () => {
  const line = formatAgentLiveEvent(event("executor.turn.received", {
    turn: 4,
    tool: "file_patch",
    path: "internal/api.go",
    summary: "Patch the route",
    total_tokens: 120,
  }))
  expect(line.title).toContain("file_patch")
  expect(line.detail).toContain("internal/api.go")
  expect(line.detail).toContain("120 tokens")
})

test("filters non-agent activity", () => {
  expect(isAgentLiveEvent(event("executor.tool.completed"))).toBe(true)
  expect(isAgentLiveEvent(event("workflow.transitioned"))).toBe(false)
})
''',
)

# Board detail subscribes to the existing durable SSE stream and refreshes status/evidence.
replace_once(
    "apps/tui/src/board-detail.tsx",
    '''import { type ApprovalKind, submitApprovalDecision } from "./approvals"
import type { BoardItem } from "./board-model"''',
    '''import { formatAgentLiveEvent, isAgentLiveEvent } from "./agent-live"
import { type ApprovalKind, submitApprovalDecision } from "./approvals"
import type { BoardItem } from "./board-model"''',
)
replace_once(
    "apps/tui/src/board-detail.tsx",
    '''import { compareReviewIssues } from "./review-board-model"''',
    '''import { type ActivityEvent, subscribeToEvents } from "./events"
import { compareReviewIssues } from "./review-board-model"''',
)
replace_once(
    "apps/tui/src/board-detail.tsx",
    '''  getWorkflowWorkspace,
  releaseWorkspace,''',
    '''  getWorkflowJob,
  getWorkflowWorkspace,
  releaseWorkspace,''',
)
replace_once(
    "apps/tui/src/board-detail.tsx",
    '''  const [leaseBusy, setLeaseBusy] = useState(false)
  const noteRef = useRef<TextareaRenderable>(null)

  const load = useCallback(async () => {
    if (!workflow) return
    setState("loading")
    try {
      const [executions, checks, reviews] = await Promise.all([
        listExecutions(workflow.id),
        listChecks(workflow.id),
        listReviews(workflow.id),
      ])''',
    '''  const [leaseBusy, setLeaseBusy] = useState(false)
  const [liveEvents, setLiveEvents] = useState<ActivityEvent[]>([])
  const [streamState, setStreamState] = useState<"connected" | "reconnecting" | "closed">("closed")
  const [clock, setClock] = useState(() => Date.now())
  const noteRef = useRef<TextareaRenderable>(null)
  const refreshInFlight = useRef(false)
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const workflowID = workflow?.id

  const load = useCallback(async (background = false) => {
    if (!workflowID) return
    if (background && refreshInFlight.current) return
    refreshInFlight.current = true
    if (!background) setState("loading")
    try {
      const currentWorkflow = await getWorkflowJob(workflowID)
      const [executions, checks, reviews] = await Promise.all([
        listExecutions(currentWorkflow.id),
        listChecks(currentWorkflow.id),
        listReviews(currentWorkflow.id),
      ])''',
)
replace_once(
    "apps/tui/src/board-detail.tsx",
    '''          getWorkflowWorkspace(workflow.id).catch(() => undefined),''',
    '''          getWorkflowWorkspace(currentWorkflow.id).catch(() => undefined),''',
)
replace_once(
    "apps/tui/src/board-detail.tsx",
    '''      setSnapshot({
        execution,''',
    '''      setWorkflow(currentWorkflow)
      setSnapshot({
        execution,''',
)
replace_once(
    "apps/tui/src/board-detail.tsx",
    '''      setState("ready")
    } catch (error) {
      setState("error")
      setMessage(error instanceof Error ? error.message : "Failed to load workflow details")
    }
  }, [workflow])

  useEffect(() => {
    void load()
  }, [load])''',
    '''      setState("ready")
    } catch (error) {
      if (!background) setState("error")
      setMessage(error instanceof Error ? error.message : "Failed to load workflow details")
    } finally {
      refreshInFlight.current = false
    }
  }, [workflowID])

  useEffect(() => {
    void load(false)
  }, [load])

  useEffect(() => {
    if (!workflowID) return
    const scheduleRefresh = () => {
      if (refreshTimer.current) clearTimeout(refreshTimer.current)
      refreshTimer.current = setTimeout(() => void load(true), 120)
    }
    const unsubscribe = subscribeToEvents({
      jobID: workflowID,
      onState: setStreamState,
      onEvent: (event) => {
        if (isAgentLiveEvent(event)) {
          setLiveEvents((current) => {
            if (current.some((item) => item.sequence === event.sequence)) return current
            return [...current, event].slice(-40)
          })
        }
        scheduleRefresh()
      },
    })
    const poll = setInterval(() => void load(true), 5000)
    const timer = setInterval(() => setClock(Date.now()), 1000)
    return () => {
      unsubscribe()
      clearInterval(poll)
      clearInterval(timer)
      if (refreshTimer.current) clearTimeout(refreshTimer.current)
    }
  }, [workflowID, load])''',
)
replace_once(
    "apps/tui/src/board-detail.tsx",
    '''  const reviewComparison = compareReviewIssues(snapshot.previousReviewIssues, snapshot.reviewIssues)

  const canDecide =''',
    '''  const reviewComparison = compareReviewIssues(snapshot.previousReviewIssues, snapshot.reviewIssues)
  const currentLiveEvents = liveEvents.filter((event) => {
    const version = event.payload.execution_version
    return typeof version !== "number" || version === workflow?.execution_version
  })
  const lastLiveAt = currentLiveEvents.at(-1)?.created_at ?? snapshot.execution?.updated_at ?? workflow?.updated_at
  const idleSeconds = lastLiveAt
    ? Math.max(0, Math.floor((clock - new Date(lastLiveAt).getTime()) / 1000))
    : 0

  const canDecide =''',
)
replace_once(
    "apps/tui/src/board-detail.tsx",
    '''            {snapshot.iterations.length > 0 ? (
              <Section title="Executor iteration timeline" action="latest 12 durable turns">''',
    '''            {workflow.status === "EXECUTING" || currentLiveEvents.length > 0 ? (
              <Section
                title="Live Executor output"
                action={`${streamState} · refreshes automatically`}
              >
                <Card>
                  <box flexDirection="row" justifyContent="space-between" gap={1}>
                    <text fg={streamState === "connected" ? colors.success : colors.warning} attributes={BOLD}>
                      {streamState === "connected" ? "LIVE" : "RECONNECTING"}
                    </text>
                    <text fg={idleSeconds >= 90 ? colors.warning : colors.faint}>
                      {`last progress ${idleSeconds}s ago`}
                    </text>
                  </box>
                  {workflow.status === "EXECUTING" && idleSeconds >= 90 ? (
                    <text fg={colors.warning} wrapMode="word">
                      {`No durable progress has been recorded recently. The Executor watchdog will stop the stage after 180 seconds without progress and expose Restart from beginning.`}
                    </text>
                  ) : null}
                  {currentLiveEvents.length === 0 ? (
                    <text fg={colors.muted}>Waiting for the first Executor progress event...</text>
                  ) : (
                    currentLiveEvents.slice(-20).map((event) => {
                      const line = formatAgentLiveEvent(event)
                      const lineColor =
                        line.tone === "danger"
                          ? colors.danger
                          : line.tone === "warning"
                            ? colors.warning
                            : line.tone === "success"
                              ? colors.success
                              : colors.text
                      return (
                        <box key={event.sequence} flexDirection="column" gap={0}>
                          <box flexDirection="row" justifyContent="space-between" gap={1}>
                            <text fg={lineColor}>{line.title}</text>
                            <text fg={colors.faint}>
                              {new Date(event.created_at).toLocaleTimeString()}
                            </text>
                          </box>
                          {line.detail ? (
                            <text fg={colors.muted} wrapMode="word">
                              {truncate(line.detail, 240)}
                            </text>
                          ) : null}
                        </box>
                      )
                    })
                  )}
                </Card>
              </Section>
            ) : null}

            {snapshot.iterations.length > 0 ? (
              <Section title="Executor iteration timeline" action="latest 12 durable turns">''',
)

print("executor live progress implementation applied")
