from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    if old not in text:
        raise SystemExit(f"expected text not found in {path}: {old[:180]!r}")
    file.write_text(text.replace(old, new, 1))


def append_once(path: str, marker: str, content: str) -> None:
    file = Path(path)
    text = file.read_text()
    if marker in text:
        return
    file.write_text(text.rstrip() + "\n\n" + content.strip() + "\n")


replace_once(
    "internal/execution/handler.go",
    "\t\t\tif paused {\n\t\t\t\treturn h.pauseWorkflow(persistCtx, job, queueJob, code, message)\n\t\t\t}",
    "\t\t\tif paused {\n"
    "\t\t\t\tif _, checkpointErr := h.savePausedCheckpoint(persistCtx, job, executionItem, item, policy); checkpointErr != nil {\n"
    "\t\t\t\t\th.record(persistCtx, job.ID, \"execution.partial_checkpoint_failed\", map[string]any{\n"
    "\t\t\t\t\t\t\"execution_id\": executionItem.ID,\n"
    "\t\t\t\t\t\t\"error\": checkpointErr.Error(),\n"
    "\t\t\t\t\t}, time.Now().UTC())\n"
    "\t\t\t\t}\n"
    "\t\t\t\treturn h.pauseWorkflow(persistCtx, job, queueJob, code, message)\n"
    "\t\t\t}",
)

append_once(
    "internal/execution/handler.go",
    "func (h *Handler) savePausedCheckpoint",
    '''func (h *Handler) savePausedCheckpoint(
	ctx context.Context,
	job workflowjob.Job,
	executionItem Execution,
	item workspace.Workspace,
	policy agentconfig.ToolPolicy,
) (Checkpoint, error) {
	snapshot, err := gitstate.Capture(ctx, item.Path, policy.MaxPatchBytes)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("capture paused Executor workspace: %w", err)
	}
	if job.BaseCommitSHA != "" && snapshot.Head != job.BaseCommitSHA {
		return Checkpoint{}, fmt.Errorf(
			"paused workspace HEAD %s does not match workflow base commit %s",
			snapshot.Head,
			job.BaseCommitSHA,
		)
	}

	artifactKey := ""
	if h.artifactStore != nil {
		artifactKey = fmt.Sprintf(
			"workflows/%s/executions/%d/partial-patch.diff",
			job.ID,
			executionItem.ExecutionVersion,
		)
		if err := h.artifactStore.Save(ctx, artifactKey, strings.NewReader(snapshot.Patch)); err != nil {
			return Checkpoint{}, fmt.Errorf("save paused execution patch artifact: %w", err)
		}
	}

	var checkpoint Checkpoint
	if artifactKey == "" {
		checkpoint, err = h.executions.SaveCheckpoint(
			ctx,
			executionItem.ID,
			job.BaseCommitSHA,
			snapshot.Head,
			snapshot.Patch,
			snapshot.ChangedFiles,
		)
	} else {
		checkpoint, err = h.executions.SaveCheckpointArtifact(
			ctx,
			executionItem.ID,
			job.BaseCommitSHA,
			snapshot.Head,
			snapshot.Patch,
			snapshot.ChangedFiles,
			artifactKey,
		)
	}
	if err != nil {
		return Checkpoint{}, fmt.Errorf("persist paused execution checkpoint: %w", err)
	}

	h.record(ctx, job.ID, "execution.partial_checkpoint_saved", map[string]any{
		"execution_id":       executionItem.ID,
		"checkpoint_id":      checkpoint.ID,
		"patch_checksum":     checkpoint.PatchChecksum,
		"patch_bytes":        checkpoint.PatchBytes,
		"changed_file_count": len(snapshot.ChangedFiles),
	}, time.Now().UTC())
	return checkpoint, nil
}''',
)

replace_once(
    "apps/tui/src/board-detail.tsx",
    "{`${iteration.sequence}. ${iteration.action_type === \"finish\" ? \"finish\" : (iteration.tool ?? \"tool\")}`}",
    "{`${iteration.sequence}. ${String(iteration.action_summary.type ?? (iteration.action_type === \"finish\" ? \"finish\" : (iteration.tool ?? \"tool\")))}`}",
)

replace_once(
    "apps/tui/src/board-detail.tsx",
    'import { workflowStatusLabel, workflowTone } from "./board-model"',
    'import { isExecutorPaused, workflowStatusLabel, workflowTone } from "./board-model"',
)
replace_once(
    "apps/tui/src/board-detail.tsx",
    '''      {workflow.status === "FAILED" ? (
        <Banner tone="danger">
          <text fg={colors.danger} attributes={BOLD}>
            Why the workflow stopped
          </text>
          <text fg={colors.text} wrapMode="word">
            {workflowFailureSummary(workflow)}
          </text>
          <text fg={colors.muted}>Fix the cause, press Esc, then choose Retry workflow.</text>
        </Banner>
      ) : null}''',
    '''      {workflow.status === "FAILED" ? (
        <Banner tone={isExecutorPaused(workflow) ? "warning" : "danger"}>
          <text
            fg={isExecutorPaused(workflow) ? colors.warning : colors.danger}
            attributes={BOLD}
          >
            {isExecutorPaused(workflow) ? "Executor paused" : "Why the workflow stopped"}
          </text>
          <text fg={colors.text} wrapMode="word">
            {workflowFailureSummary(workflow)}
          </text>
          <text fg={colors.muted}>
            {isExecutorPaused(workflow)
              ? "Inspect the partial diff and iteration timeline, press Esc, then choose Continue +8 or +16 turns."
              : "Fix the cause, press Esc, then choose Retry workflow."}
          </text>
        </Banner>
      ) : null}''',
)

append_once(
    "internal/execution/llm_runner_test.go",
    "func TestSavePausedCheckpointPersistsPartialDiff",
    '''func TestSavePausedCheckpointPersistsPartialDiff(t *testing.T) {
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
		WorkflowJobID: "workflow-1",
		WorkflowVersion: 3,
		ExecutionVersion: 1,
		PlanVersionID: "plan-version-1",
		WorkspaceID: "workspace-1",
		BaseCommitSHA: head,
		AgentSettingsVersion: 1,
		Provider: "fake",
		Model: "fake-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "README.md", "# Fixture\n\npartial implementation\n")

	handler := &Handler{executions: repository}
	checkpoint, err := handler.savePausedCheckpoint(
		ctx,
		workflowjob.Job{ID: "workflow-1", BaseCommitSHA: head},
		executionItem,
		workspace.Workspace{ID: "workspace-1", Path: root},
		agentconfig.ToolPolicy{MaxPatchBytes: 1024 * 1024},
	)
	if err != nil {
		t.Fatalf("savePausedCheckpoint() error = %v", err)
	}
	if checkpoint.PatchBytes == 0 || checkpoint.PatchChecksum == "" {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}
	var changedFiles []string
	if err := json.Unmarshal(checkpoint.ChangedFilesJSON, &changedFiles); err != nil {
		t.Fatal(err)
	}
	if len(changedFiles) != 1 || changedFiles[0] != "README.md" {
		t.Fatalf("changed files = %#v", changedFiles)
	}
	items, err := repository.ListCheckpoints(ctx, executionItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].PatchChecksum != checkpoint.PatchChecksum {
		t.Fatalf("stored checkpoints = %#v", items)
	}
}''',
)

replace_once(
    "internal/execution/llm_runner_test.go",
    '"github.com/livingdolls/orkoda-tui/internal/workspace"',
    '"github.com/livingdolls/orkoda-tui/internal/workflowjob"\n\t"github.com/livingdolls/orkoda-tui/internal/workspace"',
)

print("paused Executor checkpoint persistence applied")
