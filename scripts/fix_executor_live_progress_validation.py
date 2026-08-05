from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    target = Path(path)
    content = target.read_text()
    if content.count(old) != 1:
        raise SystemExit(f"expected one match in {path}, found {content.count(old)}")
    target.write_text(content.replace(old, new, 1))


def append_once(path: str, marker: str, content: str) -> None:
    target = Path(path)
    current = target.read_text()
    if marker in current:
        return
    target.write_text(current.rstrip() + "\n\n" + content.strip() + "\n")


replace_once(
    "internal/execution/handler.go",
    '''\t\tif failure := watchdog.Failure(); failure != nil && ctx.Err() == nil {
\t\t\tresultErr = failure
\t\t}''',
    '''\t\tif failure := watchdog.Failure(); failure != nil && resultErr != nil && ctx.Err() == nil {
\t\t\tif errors.Is(resultErr, context.Canceled) || errors.Is(resultErr, context.DeadlineExceeded) {
\t\t\t\tresultErr = failure
\t\t\t}
\t\t}''',
)

append_once(
    "internal/execution/llm_runner_test.go",
    "func newLLMRunnerFixture(",
    '''func newLLMRunnerFixture(
\tt *testing.T,
\tresponses []string,
) (*LLMRunner, *Repository, RunContext) {
\tt.Helper()
\tctx := context.Background()
\tdb, err := database.Open(ctx, filepath.Join(t.TempDir(), "live-progress.db"))
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tt.Cleanup(func() { _ = db.Close() })
\tif err := database.Migrate(ctx, db); err != nil {
\t\tt.Fatal(err)
\t}

\troot := t.TempDir()
\trunGit(t, root, "init")
\trunGit(t, root, "config", "user.email", "test@example.com")
\trunGit(t, root, "config", "user.name", "Test")
\twriteTestFile(t, root, "README.md", "# Fixture\\n")
\trunGit(t, root, "add", "README.md")
\trunGit(t, root, "commit", "-m", "initial")
\thead := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
\tseedExecutorLoop(t, db, root, head)

\trepository, err := NewRepository(db)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\texecutionItem, _, err := repository.CreateOrGet(ctx, CreateInput{
\t\tWorkflowJobID: "workflow-1", WorkflowVersion: 3, ExecutionVersion: 1,
\t\tPlanVersionID: "plan-version-1", WorkspaceID: "workspace-1",
\t\tBaseCommitSHA: head, AgentSettingsVersion: 1,
\t\tProvider: "fake", Model: "fake-model",
\t})
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tselector, err := NewContextSelector(db)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tgatewayResponses := make([]llm.Response, 0, len(responses))
\tfor _, response := range responses {
\t\tgatewayResponses = append(gatewayResponses, fakeExecutorResponse(response))
\t}
\trunner, err := NewLLMRunner(&sequenceGateway{responses: gatewayResponses}, selector, repository)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tpolicy := agentconfig.ToolPolicy{
\t\tRole:             agentconfig.RoleExecutor,
\t\tAllowedTools:     []string{agentconfig.ToolGitStatus, agentconfig.ToolGitDiff},
\t\tFilesystemAccess: agentconfig.FilesystemWorkspaceWrite,
\t\tMaxFileBytes:     1024 * 1024,
\t\tMaxPatchBytes:    1024 * 1024,
\t}
\ttools := &RecordedTools{
\t\trepository: repository,
\t\texecution:  executionItem,
\t\ttoolset:    Toolset{Root: root, Policy: policy},
\t\tmaxCalls:   10,
\t}
\treturn runner, repository, RunContext{
\t\tExecution: executionItem,
\t\tWorkspace: workspace.Workspace{ID: "workspace-1", Path: root},
\t\tTools:     tools,
\t}
}''',
)

print("executor live progress validation fixes applied")
