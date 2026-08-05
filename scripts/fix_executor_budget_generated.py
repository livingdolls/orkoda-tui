from pathlib import Path

for file_name, marker in [
    ("internal/execution/llm_runner_test.go", "func TestNormalizeExecutorBudget"),
    ("internal/workflowjob/validation_test.go", "func TestExecutorContinuationCodes"),
]:
    path = Path(file_name)
    text = path.read_text()
    before, after = text.split(marker, 1)
    path.write_text(before + marker + after.replace(r"\t", "\t"))

path = Path("internal/workflowjob/repository_test.go")
text = path.read_text()
old = (
    "\tif created.Limits.MaxRevisions != 3 || created.Limits.MaxStageAttempts != 3 ||\n"
    "\t\tcreated.Limits.MaxToolCalls != 120 || created.Limits.WallClockSeconds != 3600 {"
)
new = (
    "\tif created.Limits.MaxRevisions != 3 || created.Limits.MaxStageAttempts != 3 ||\n"
    "\t\tcreated.Limits.MaxExecutorTurns != 32 || created.Limits.MaxToolCalls != 24 ||\n"
    "\t\tcreated.Limits.MaxConsecutiveToolErrors != 3 || created.Limits.MaxNoProgressTurns != 4 ||\n"
    "\t\tcreated.Limits.WallClockSeconds != 3600 {"
)
if old not in text:
    raise SystemExit("default workflow limit assertion not found")
path.write_text(text.replace(old, new, 1))

path = Path("apps/tui/src/board-model.test.ts")
text = path.read_text()
text = text.replace("workflowFixture({", "workflow(\"FAILED\", {")
text = text.replace("createBoardItem(projectFixture(), planFixture(), workflow)", "createBoardItem(project, plan(\"READY\"), workflow)")
path.write_text(text)
