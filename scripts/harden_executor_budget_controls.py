from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    if old not in text:
        raise SystemExit(f"expected text not found in {path}: {old[:160]!r}")
    file.write_text(text.replace(old, new, 1))


def append_once(path: str, marker: str, content: str) -> None:
    file = Path(path)
    text = file.read_text()
    if marker in text:
        return
    file.write_text(text.rstrip() + "\n\n" + content.strip() + "\n")


# Progress must use the actual patch checksum, not git status, because multiple
# successful edits to the same already-dirty file leave porcelain status unchanged.
replace_once(
    "internal/execution/llm_runner.go",
    "\tworkspaceFingerprint := textFingerprint(gitStatus)\n\ttoolTurns := budget.MaxTurns - 1",
    "\tworkspaceFingerprint, err := workspaceProgressFingerprint(ctx, run)\n"
    "\tif err != nil {\n"
    "\t\treturn err\n"
    "\t}\n"
    "\ttoolTurns := budget.MaxTurns - 1",
)
replace_once(
    "internal/execution/llm_runner.go",
    "\t\tif isWriteTool(action.Tool) {\n"
    "\t\t\tstatus, statusErr := run.Tools.toolset.GitStatus(ctx)\n"
    "\t\t\tif statusErr != nil {\n"
    "\t\t\t\treturn fmt.Errorf(\"measure executor progress: %w\", statusErr)\n"
    "\t\t\t}\n"
    "\t\t\tnextFingerprint := textFingerprint(status)",
    "\t\tif isWriteTool(action.Tool) {\n"
    "\t\t\tnextFingerprint, progressErr := workspaceProgressFingerprint(ctx, run)\n"
    "\t\t\tif progressErr != nil {\n"
    "\t\t\t\treturn progressErr\n"
    "\t\t\t}",
)

# Persist the third repeated model response before pausing so usage and the
# complete timeline remain durable.
replace_once(
    "internal/execution/llm_runner.go",
    "\t\tif repeatedActionCount >= 3 {\n"
    "\t\t\treturn pauseExecutor(ExecutorRepeatedActionCode,\n"
    "\t\t\t\tfmt.Sprintf(\"Executor repeated the same %s action three times without changing strategy.\", actionLabel(action)))\n"
    "\t\t}\n\n"
    "\t\titeration, err := r.repository.BeginIteration",
    "\t\titeration, err := r.repository.BeginIteration",
)
replace_once(
    "internal/execution/llm_runner.go",
    "\t\tif iteration.Model == \"\" {\n"
    "\t\t\titeration.Model = run.Execution.Model\n"
    "\t\t}\n\n"
    "\t\tif action.Type == \"finish\" {",
    "\t\tif iteration.Model == \"\" {\n"
    "\t\t\titeration.Model = run.Execution.Model\n"
    "\t\t}\n"
    "\t\tif repeatedActionCount >= 3 {\n"
    "\t\t\tmessage := fmt.Sprintf(\"Executor repeated the same %s action three times without changing strategy.\", actionLabel(action))\n"
    "\t\t\t_ = r.repository.FailIteration(context.WithoutCancel(ctx), iteration.ID, ExecutorRepeatedActionCode, message)\n"
    "\t\t\treturn pauseExecutor(ExecutorRepeatedActionCode, message)\n"
    "\t\t}\n\n"
    "\t\tif action.Type == \"finish\" {",
)

# Finalization is always a durable iteration, including needs_more_work, so
# token usage and the final decision are never lost.
replace_once(
    "internal/execution/llm_runner.go",
    "\tif action.Type == \"needs_more_work\" {\n"
    "\t\treturn pauseExecutor(ExecutorBudgetExhaustedCode,\n"
    "\t\t\tfmt.Sprintf(\"Executor used all %d turns and reported remaining work: %s\", turn, boundText(action.Summary, 768)))\n"
    "\t}\n"
    "\titeration, err := r.repository.BeginIteration(ctx, run.Execution.ID, IterationInput{\n"
    "\t\tProvider: response.Usage.FinalProvider, Model: response.Usage.FinalModel,\n"
    "\t\tActionType: \"finish\", ActionSummary: map[string]any{\"type\": \"finish\", \"summary\": boundText(action.Summary, 512)}, Usage: response.Usage,\n"
    "\t})\n"
    "\tif err != nil {\n"
    "\t\treturn err\n"
    "\t}\n"
    "\tif err := validateExecutorCompletion(ctx, run); err != nil {",
    "\titeration, err := r.repository.BeginIteration(ctx, run.Execution.ID, IterationInput{\n"
    "\t\tProvider: firstNonEmpty(response.Usage.FinalProvider, run.Execution.Provider),\n"
    "\t\tModel: firstNonEmpty(response.Usage.FinalModel, run.Execution.Model),\n"
    "\t\tActionType: \"finish\",\n"
    "\t\tActionSummary: map[string]any{\"type\": action.Type, \"summary\": boundText(action.Summary, 512), \"finalization_only\": true},\n"
    "\t\tUsage: response.Usage,\n"
    "\t})\n"
    "\tif err != nil {\n"
    "\t\treturn err\n"
    "\t}\n"
    "\tif action.Type == \"needs_more_work\" {\n"
    "\t\tmessage := fmt.Sprintf(\"Executor used all %d turns and reported remaining work: %s\", turn, boundText(action.Summary, 768))\n"
    "\t\t_ = r.repository.FailIteration(context.WithoutCancel(ctx), iteration.ID, ExecutorBudgetExhaustedCode, message)\n"
    "\t\treturn pauseExecutor(ExecutorBudgetExhaustedCode, message)\n"
    "\t}\n"
    "\tif err := validateExecutorCompletion(ctx, run); err != nil {",
)

# Use execution snapshots when a gateway omits final provider/model metadata.
replace_once(
    "internal/execution/llm_runner.go",
    "\t\titeration, err := r.repository.BeginIteration(ctx, run.Execution.ID, IterationInput{\n"
    "\t\t\tProvider: response.Usage.FinalProvider, Model: response.Usage.FinalModel,",
    "\t\titeration, err := r.repository.BeginIteration(ctx, run.Execution.ID, IterationInput{\n"
    "\t\t\tProvider: firstNonEmpty(response.Usage.FinalProvider, run.Execution.Provider),\n"
    "\t\t\tModel: firstNonEmpty(response.Usage.FinalModel, run.Execution.Model),",
)
replace_once(
    "internal/execution/llm_runner.go",
    "func validateExecutorCompletion(ctx context.Context, run RunContext) error {",
    '''func workspaceProgressFingerprint(ctx context.Context, run RunContext) (string, error) {
\tsnapshot, err := gitstate.Capture(ctx, run.Workspace.Path, run.Tools.toolset.Policy.MaxPatchBytes)
\tif err != nil {
\t\treturn "", fmt.Errorf("measure executor progress: %w", err)
\t}
\treturn snapshot.Checksum, nil
}

func firstNonEmpty(values ...string) string {
\tfor _, value := range values {
\t\tif trimmed := strings.TrimSpace(value); trimmed != "" {
\t\t\treturn trimmed
\t\t}
\t}
\treturn ""
}

func validateExecutorCompletion(ctx context.Context, run RunContext) error {''',
)

# Make paused work read as a pause, not a generic failure, in the action menu.
replace_once(
    "apps/tui/src/board-model.ts",
    '''          : item.workflow.status === "FAILED"
            ? "See why it failed"
            : "Open workflow details",''',
    '''          : item.workflow.status === "FAILED"
            ? isExecutorPaused(item.workflow)
              ? "Inspect Executor pause"
              : "See why it failed"
            : "Open workflow details",''',
)
replace_once(
    "apps/tui/src/board-model.ts",
    '''          : item.workflow.status === "FAILED"
            ? "danger"
            : "neutral",''',
    '''          : item.workflow.status === "FAILED"
            ? isExecutorPaused(item.workflow)
              ? "warning"
              : "danger"
            : "neutral",''',
)

append_once(
    "internal/workflowjob/repository_test.go",
    "func TestContinuePausedExecutor",
    r'''func TestContinuePausedExecutor(t *testing.T) {
\trepository, _, db, _, input := openWorkflowRepository(t, "READY")
\tdefer db.Close()
\tctx := context.Background()

\tjob, err := repository.Create(ctx, input)
\tif err != nil {
\t\tt.Fatalf("Create() error = %v", err)
\t}
\tfor _, action := range []Action{ActionStart, ActionWorkspaceReady, ActionExecutionStarted} {
\t\tjob, err = repository.Transition(ctx, job.ID, TransitionInput{
\t\t\tExpectedVersion: job.Version,
\t\t\tAction: action,
\t\t})
\t\tif err != nil {
\t\t\tt.Fatalf("Transition(%s) error = %v", action, err)
\t\t}
\t}
\tfailed, err := repository.Transition(ctx, job.ID, TransitionInput{
\t\tExpectedVersion: job.Version,
\t\tAction: ActionFail,
\t\tFailureCode: "EXECUTOR_BUDGET_EXHAUSTED",
\t\tFailureMessage: "implementation still needs two files",
\t})
\tif err != nil {
\t\tt.Fatalf("pause Executor error = %v", err)
\t}
\tif failed.Status != StatusFailed || failed.RetryStatus != StatusExecuting {
\t\tt.Fatalf("failed = %#v", failed)
\t}

\tcontinued, err := repository.Transition(ctx, failed.ID, TransitionInput{
\t\tExpectedVersion: failed.Version,
\t\tAction: ActionContinueExecution,
\t\tAdditionalExecutorTurns: 8,
\t\tDetails: map[string]any{"requested_by": "test"},
\t})
\tif err != nil {
\t\tt.Fatalf("continue error = %v", err)
\t}
\tif continued.Status != StatusQueued || continued.Limits.MaxExecutorTurns != 40 ||
\t\tcontinued.FailureCode != "" || continued.FailureMessage != "" ||
\t\tcontinued.RetryStatus != "" || continued.CurrentDispatchID == "" {
\t\tt.Fatalf("continued = %#v", continued)
\t}
\tif continued.ExecutionVersion != 1 {
\t\tt.Fatalf("execution version changed before dispatch: %d", continued.ExecutionVersion)
\t}

\texecuting, err := repository.Transition(ctx, continued.ID, TransitionInput{
\t\tExpectedVersion: continued.Version,
\t\tAction: ActionExecutionStarted,
\t})
\tif err != nil {
\t\tt.Fatalf("execution started error = %v", err)
\t}
\tif executing.Status != StatusExecuting || executing.ExecutionVersion != 2 {
\t\tt.Fatalf("executing = %#v", executing)
\t}
}''',
)

append_once(
    "internal/httpapi/workflow_jobs_test.go",
    "func TestWorkflowContinueRoute",
    r'''func TestWorkflowContinueRoute(t *testing.T) {
\tregistry := &fakeWorkflowJobRegistry{job: testWorkflowJob()}
\trouter := workflowRouter(registry)
\trequest := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/workflow-1/continue", strings.NewReader(`{
\t\t"expected_version":7,
\t\t"additional_executor_turns":16,
\t\t"details":{"requested_by":"board"}
\t}`))
\trequest.Header.Set("content-type", "application/json")
\tresponse := httptest.NewRecorder()
\trouter.ServeHTTP(response, request)
\tif response.Code != http.StatusOK {
\t\tt.Fatalf("continue status = %d body = %s", response.Code, response.Body.String())
\t}
\tif registry.transitionInput.Action != workflowjob.ActionContinueExecution ||
\t\tregistry.transitionInput.ExpectedVersion != 7 ||
\t\tregistry.transitionInput.AdditionalExecutorTurns != 16 ||
\t\tregistry.transitionInput.Details["requested_by"] != "board" {
\t\tt.Fatalf("continue input = %#v", registry.transitionInput)
\t}
}''',
)

append_once(
    "internal/execution/llm_runner_test.go",
    "func TestWorkspaceProgressFingerprintTracksPatchContent",
    r'''func TestWorkspaceProgressFingerprintTracksPatchContent(t *testing.T) {
\tctx := context.Background()
\troot := t.TempDir()
\trunGit(t, root, "init")
\trunGit(t, root, "config", "user.email", "test@example.com")
\trunGit(t, root, "config", "user.name", "Test")
\twriteTestFile(t, root, "main.go", "package main\n")
\trunGit(t, root, "add", "main.go")
\trunGit(t, root, "commit", "-m", "initial")
\trun := RunContext{
\t\tWorkspace: workspace.Workspace{Path: root},
\t\tTools: &RecordedTools{toolset: Toolset{Root: root, Policy: agentconfig.ToolPolicy{MaxPatchBytes: 1024 * 1024}}},
\t}
\tinitial, err := workspaceProgressFingerprint(ctx, run)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\twriteTestFile(t, root, "main.go", "package main\n\nfunc one() {}\n")
\tfirst, err := workspaceProgressFingerprint(ctx, run)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\twriteTestFile(t, root, "main.go", "package main\n\nfunc one() {}\nfunc two() {}\n")
\tsecond, err := workspaceProgressFingerprint(ctx, run)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif initial == first || first == second || initial == second {
\t\tt.Fatalf("fingerprints did not track patch changes: %q %q %q", initial, first, second)
\t}
}''',
)

print("executor budget hardening applied")
