package checks

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestRepositoryPersistsResultsAndRecoversInterruptedSteps(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	seedCheckDependencies(t, db)

	repository, err := NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	run, created, err := repository.CreateOrGet(
		ctx,
		"workflow-1",
		"execution-1",
		"workspace-1",
		1,
	)
	if err != nil || !created {
		t.Fatalf("CreateOrGet() = %#v, %v, %v", run, created, err)
	}
	duplicate, created, err := repository.CreateOrGet(
		ctx,
		"workflow-1",
		"execution-1",
		"workspace-1",
		1,
	)
	if err != nil || created || duplicate.ID != run.ID {
		t.Fatalf("duplicate CreateOrGet() = %#v, %v, %v", duplicate, created, err)
	}

	profiles := []Profile{
		{Name: "go.vet", Command: []string{"go", "vet", "./..."}, Timeout: time.Minute, OutputLimit: 1024},
		{Name: "go.test", Command: []string{"go", "test", "./..."}, Timeout: time.Minute, OutputLimit: 1024},
	}
	if _, err := repository.Start(ctx, run.ID, profiles); err != nil {
		t.Fatal(err)
	}
	vetStep, err := repository.StartStep(ctx, run.ID, "go.vet")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CancelStep(ctx, vetStep.ID, "daemon shutdown"); err != nil {
		t.Fatal(err)
	}
	if err := repository.RecoverInterrupted(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	vetStep, err = repository.StartStep(ctx, run.ID, "go.vet")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteStep(ctx, vetStep.ID, Result{
		Passed: true, ExitCode: 0, Duration: 10 * time.Millisecond,
		OutputLimit: 1024,
	}); err != nil {
		t.Fatal(err)
	}
	testStep, err := repository.StartStep(ctx, run.ID, "go.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteStep(ctx, testStep.ID, Result{
		Passed: false, ExitCode: 1, Duration: 20 * time.Millisecond,
		Output: "failed output", OutputLimit: 6, Truncated: true,
		ErrorMessage: "exit status 1",
	}); err != nil {
		t.Fatal(err)
	}

	finished, err := repository.Finish(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != StatusFailed || finished.PassedSteps != 1 || finished.FailedSteps != 1 {
		t.Fatalf("finished run = %#v", finished)
	}
	steps, err := repository.ListSteps(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Status != StatusPassed || steps[1].Status != StatusFailed {
		t.Fatalf("steps = %#v", steps)
	}
	if steps[1].OutputText != "failed" || !steps[1].OutputTruncated || steps[1].ExitCode == nil || *steps[1].ExitCode != 1 {
		t.Fatalf("failed step = %#v", steps[1])
	}
}

func seedCheckDependencies(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`, []any{"project-1", "Project", now, now}},
		{`INSERT INTO repositories (id, project_id, local_path, current_branch, head_sha, remote_url, dirty, created_at, updated_at) VALUES (?, ?, ?, 'main', ?, '', 0, ?, ?)`, []any{"repository-1", "project-1", t.TempDir(), "abc123", now, now}},
		{`INSERT INTO plans (id, project_id, title, status, current_version, created_at, updated_at) VALUES (?, ?, 'Plan', 'READY', 1, ?, ?)`, []any{"plan-1", "project-1", now, now}},
		{`INSERT INTO plan_versions (id, plan_id, version, requirement, acceptance_criteria_json, constraints_json, created_at) VALUES (?, ?, 1, 'Run checks', '[]', '[]', ?)`, []any{"plan-version-1", "plan-1", now}},
		{`INSERT INTO workflow_jobs (id, project_id, plan_id, plan_version_id, repository_id, base_branch, base_commit_sha, status, version, execution_version, max_revisions, max_stage_attempts, max_tool_calls, wall_clock_seconds, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'main', 'abc123', 'CHECKING', 4, 1, 3, 3, 10, 3600, ?, ?)`, []any{"workflow-1", "project-1", "plan-1", "plan-version-1", "repository-1", now, now}},
		{`INSERT INTO workspaces (id, workflow_job_id, project_id, repository_id, path, base_commit_sha, head_sha, status, dirty, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'abc123', 'abc123', 'READY', 0, ?, ?)`, []any{"workspace-1", "workflow-1", "project-1", "repository-1", t.TempDir(), now, now}},
		{`INSERT INTO executions (id, workflow_job_id, workflow_version, execution_version, plan_version_id, workspace_id, base_commit_sha, agent_settings_version, provider, model, status, tool_calls, created_at, updated_at) VALUES (?, ?, 3, 1, ?, ?, 'abc123', 1, 'fake', 'fake-model', 'COMPLETED', 0, ?, ?)`, []any{"execution-1", "workflow-1", "plan-version-1", "workspace-1", now, now}},
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seed checks: %v", err)
		}
	}
}
