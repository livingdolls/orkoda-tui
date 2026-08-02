package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/workspace"
)

func TestContextFileSelectionExcludesSecretsAndBuildDirectories(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", "safe")
	writeTestFile(t, root, ".env", "PASSWORD=secret")
	writeTestFile(t, root, "credentials.json", "secret")
	writeTestFile(t, root, "node_modules/pkg/index.js", "ignored")
	writeTestFile(t, root, "internal/app.go", "package internal")

	files, err := collectContextFiles(root, 20)
	if err != nil {
		t.Fatalf("collectContextFiles() error = %v", err)
	}
	joined := strings.Join(files, "\n")
	if strings.Contains(joined, ".env") || strings.Contains(joined, "credentials") || strings.Contains(joined, "node_modules") {
		t.Fatalf("unsafe files selected: %v", files)
	}
	if !strings.Contains(joined, "README.md") || !strings.Contains(joined, "internal/app.go") {
		t.Fatalf("expected files missing: %v", files)
	}
}

func TestValidateExecutorAction(t *testing.T) {
	valid := []executorAction{
		{Type: "finish", Summary: "done"},
		{Type: "tool", Tool: "git_status", Summary: "inspect"},
		{Type: "tool", Tool: "file_read", Summary: "read", Arguments: executorArguments{Path: "main.go"}},
	}
	for _, action := range valid {
		if err := validateExecutorAction(action); err != nil {
			t.Fatalf("validateExecutorAction(%#v) error = %v", action, err)
		}
	}
	invalid := []executorAction{
		{Type: "finish"},
		{Type: "tool", Tool: "file_read", Summary: "missing path"},
		{Type: "tool", Tool: "command_run", Summary: "unsafe"},
	}
	for _, action := range invalid {
		if err := validateExecutorAction(action); err == nil {
			t.Fatalf("validateExecutorAction(%#v) unexpectedly succeeded", action)
		}
	}
}

func TestLLMRunnerPersistsToolAndFinishIterations(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	writeTestFile(t, root, "README.md", "# Fixture\n")
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "initial")
	head := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	seedExecutorLoop(t, db, root, head)

	repository, err := NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	executionItem, _, err := repository.CreateOrGet(ctx, CreateInput{
		WorkflowJobID: "workflow-1", WorkflowVersion: 3, ExecutionVersion: 1,
		PlanVersionID: "plan-version-1", WorkspaceID: "workspace-1",
		BaseCommitSHA: head, AgentSettingsVersion: 1,
		Provider: "fake", Model: "fake-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	selector, err := NewContextSelector(db)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewLLMRunner(&sequenceGateway{responses: []llm.Response{
		fakeExecutorResponse(`{"type":"tool","tool":"git_status","arguments":{},"summary":"inspect status"}`),
		fakeExecutorResponse(`{"type":"finish","summary":"implementation complete"}`),
	}}, selector, repository)
	if err != nil {
		t.Fatal(err)
	}
	policy := agentconfig.ToolPolicy{
		Role:             agentconfig.RoleExecutor,
		AllowedTools:     []string{agentconfig.ToolGitStatus, agentconfig.ToolGitDiff},
		FilesystemAccess: agentconfig.FilesystemWorkspaceWrite,
		MaxFileBytes:     1024 * 1024, MaxPatchBytes: 1024 * 1024,
	}
	tools := &RecordedTools{
		repository: repository, execution: executionItem,
		toolset: Toolset{Root: root, Policy: policy}, maxCalls: 10,
	}
	if err := runner.Run(ctx, RunContext{
		Execution: executionItem,
		Workspace: workspace.Workspace{ID: "workspace-1", Path: root},
		Tools:     tools,
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	iterations, err := repository.ListIterations(ctx, executionItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(iterations) != 2 || iterations[0].Tool != "git_status" || iterations[1].ActionType != "finish" {
		t.Fatalf("iterations = %#v", iterations)
	}
	toolRuns, err := repository.ListToolRuns(ctx, executionItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(toolRuns) != 1 || toolRuns[0].Tool != agentconfig.ToolGitStatus {
		t.Fatalf("tool runs = %#v", toolRuns)
	}
}

type sequenceGateway struct {
	responses []llm.Response
	index     int
}

func (g *sequenceGateway) Complete(_ context.Context, _ string, _ llm.Request) (llm.Response, error) {
	response := g.responses[g.index]
	g.index++
	return response, nil
}

func fakeExecutorResponse(content string) llm.Response {
	return llm.Response{
		ID: "response", Model: "fake-model", Content: content,
		FinishReason: llm.FinishReasonStop,
		Usage: llm.Usage{
			InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
			FinalProvider: "fake", FinalModel: "fake-model",
		},
	}
}

func seedExecutorLoop(t *testing.T, db *sql.DB, root, head string) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`, []any{"project-1", "Project", now, now}},
		{`INSERT INTO repositories (id, project_id, local_path, current_branch, head_sha, remote_url, dirty, created_at, updated_at) VALUES (?, ?, ?, 'main', ?, '', 0, ?, ?)`, []any{"repository-1", "project-1", root, head, now, now}},
		{`INSERT INTO plans (id, project_id, title, status, current_version, created_at, updated_at) VALUES (?, ?, 'Plan', 'READY', 1, ?, ?)`, []any{"plan-1", "project-1", now, now}},
		{`INSERT INTO plan_versions (id, plan_id, version, requirement, acceptance_criteria_json, constraints_json, created_at) VALUES (?, ?, 1, ?, ?, ?, ?)`, []any{"plan-version-1", "plan-1", "Inspect the repository safely", `["Git status is inspected"]`, `["Do not modify files"]`, now}},
		{`INSERT INTO workflow_jobs (id, project_id, plan_id, plan_version_id, repository_id, base_branch, base_commit_sha, status, version, execution_version, max_revisions, max_stage_attempts, max_tool_calls, wall_clock_seconds, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'main', ?, 'EXECUTING', 3, 1, 3, 3, 10, 3600, ?, ?)`, []any{"workflow-1", "project-1", "plan-1", "plan-version-1", "repository-1", head, now, now}},
		{`INSERT INTO workspaces (id, workflow_job_id, project_id, repository_id, path, base_commit_sha, head_sha, status, dirty, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'WRITE_LOCKED', 0, ?, ?)`, []any{"workspace-1", "workflow-1", "project-1", "repository-1", root, head, head, now, now}},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(context.Background(), statement.query, statement.args...); err != nil {
			t.Fatalf("seed executor loop: %v", err)
		}
	}
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}

func writeTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSafeActionSummaryDoesNotPersistFileContent(t *testing.T) {
	summary := safeActionSummary(executorAction{
		Type: "tool", Tool: "file_create", Summary: "create file",
		Arguments: executorArguments{Path: "secret.txt", Content: "do-not-persist"},
	})
	payload, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "do-not-persist") || summary["content_bytes"] != len("do-not-persist") {
		t.Fatalf("unsafe summary = %s", payload)
	}
}
