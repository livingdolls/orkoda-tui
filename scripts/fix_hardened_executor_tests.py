from pathlib import Path

for file_name, marker in [
    ("internal/execution/llm_runner_test.go", "func TestWorkspaceProgressFingerprintTracksPatchContent"),
    ("internal/workflowjob/repository_test.go", "func TestContinuePausedExecutor"),
    ("internal/httpapi/workflow_jobs_test.go", "func TestWorkflowContinueRoute"),
]:
    path = Path(file_name)
    text = path.read_text()
    before, after = text.split(marker, 1)
    path.write_text(before + marker + after.replace(r"\t", "\t"))
