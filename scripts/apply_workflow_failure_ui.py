from pathlib import Path

root = Path(".")

helper = root / "apps/tui/src/workflow-failure.ts"
helper.write_text(
    '''import type { CheckStep } from "./checks"
import type { Execution } from "./executions"
import type { ReviewRun } from "./reviews"
import type { WorkflowJob, Workspace } from "./workflow-jobs"

export type WorkflowFailureEvidence = {
  source: string
  code?: string
  message: string
}

export function workflowFailureStage(code?: string): string {
  const normalized = code?.trim().toUpperCase() ?? ""
  if (normalized.startsWith("WORKSPACE")) return "Workspace preparation"
  if (normalized.startsWith("EXECUTION") || normalized.startsWith("EXECUTOR")) return "Executor"
  if (normalized.startsWith("CHECK")) return "Automated checks"
  if (normalized.startsWith("REVIEW")) return "AI review"
  if (normalized.startsWith("PUBLICATION") || normalized.startsWith("PUBLISH")) return "Publication"
  return "Workflow"
}

export function workflowFailureSummary(workflow: WorkflowJob): string {
  const stage = workflowFailureStage(workflow.failure_code)
  const message =
    cleanMessage(workflow.failure_message) || "No failure message was recorded by the daemon."
  return workflow.failure_code
    ? `${stage} (${workflow.failure_code}): ${message}`
    : `${stage}: ${message}`
}

export function collectWorkflowFailureEvidence(input: {
  workflow: WorkflowJob
  workspace?: Workspace
  execution?: Execution
  checkSteps?: CheckStep[]
  review?: ReviewRun
}): WorkflowFailureEvidence[] {
  const evidence: WorkflowFailureEvidence[] = []
  const seen = new Set<string>()
  const add = (source: string, message?: string, code?: string) => {
    const cleaned = cleanMessage(message)
    if (!cleaned) return
    const key = `${code ?? ""}\u0000${cleaned}`
    if (seen.has(key)) return
    seen.add(key)
    evidence.push({ source, code: code?.trim() || undefined, message: cleaned })
  }

  if (input.workflow.status === "FAILED") {
    add(
      workflowFailureStage(input.workflow.failure_code),
      input.workflow.failure_message,
      input.workflow.failure_code,
    )
  }
  add("Workspace", input.workspace?.failure_message)
  if (input.execution?.status === "FAILED") {
    add("Executor", input.execution.failure_message, input.execution.failure_code)
  }
  for (const step of input.checkSteps ?? []) {
    if (step.status !== "FAILED") continue
    const fallback =
      step.exit_code === undefined
        ? `Check ${step.profile} failed.`
        : `Check ${step.profile} exited with code ${step.exit_code}.`
    add(`Check · ${step.profile}`, step.error_message || tail(step.output_text) || fallback)
  }
  if (input.review?.status === "FAILED") {
    add("AI review", input.review.failure_message, input.review.failure_code)
  }
  return evidence
}

function cleanMessage(value?: string): string {
  return value?.trim().replace(/\s+/g, " ") ?? ""
}

function tail(value: string, limit = 1200): string {
  const cleaned = value.trim()
  return cleaned.length > limit ? `…${cleaned.slice(-limit)}` : cleaned
}
'''
)

test = root / "apps/tui/src/workflow-failure.test.ts"
test.write_text(
    '''import { describe, expect, test } from "bun:test"

import { collectWorkflowFailureEvidence, workflowFailureSummary } from "./workflow-failure"
import type { WorkflowJob } from "./workflow-jobs"

function failedWorkflow(): WorkflowJob {
  return {
    id: "job-1",
    project_id: "project-1",
    plan_id: "plan-1",
    plan_version_id: "plan-version-1",
    repository_id: "repo-1",
    base_branch: "main",
    base_commit_sha: "abc123",
    status: "FAILED",
    version: 4,
    execution_version: 0,
    revision_count: 0,
    limits: {
      max_revisions: 3,
      max_stage_attempts: 3,
      max_tool_calls: 50,
      wall_clock_seconds: 3600,
    },
    cancellation_requested: false,
    failure_code: "WORKSPACE_PREPARATION_FAILED",
    failure_message: "prepare isolated Git worktree: source repository has uncommitted changes",
    created_at: "2026-08-04T00:00:00Z",
    updated_at: "2026-08-04T00:00:01Z",
  }
}

describe("workflow failure presentation", () => {
  test("turns a workflow failure into an actionable summary", () => {
    expect(workflowFailureSummary(failedWorkflow())).toBe(
      "Workspace preparation (WORKSPACE_PREPARATION_FAILED): prepare isolated Git worktree: source repository has uncommitted changes",
    )
  })

  test("collects stage evidence and keeps failed check output", () => {
    const evidence = collectWorkflowFailureEvidence({
      workflow: failedWorkflow(),
      workspace: {
        id: "workspace-1",
        workflow_job_id: "job-1",
        project_id: "project-1",
        repository_id: "repo-1",
        path: "/tmp/workspace",
        base_commit_sha: "abc123",
        status: "FAILED",
        dirty: false,
        failure_message: "worktree creation was rejected",
        created_at: "2026-08-04T00:00:00Z",
        updated_at: "2026-08-04T00:00:01Z",
      },
      checkSteps: [
        {
          id: "step-1",
          check_run_id: "check-1",
          sequence: 1,
          profile: "bun-test",
          command: ["bun", "test"],
          status: "FAILED",
          exit_code: 1,
          duration_ms: 25,
          output_text: "Assertion failed in board test",
          output_truncated: false,
          created_at: "2026-08-04T00:00:00Z",
          updated_at: "2026-08-04T00:00:01Z",
        },
      ],
    })
    expect(evidence.map((item) => item.source)).toEqual([
      "Workspace preparation",
      "Workspace",
      "Check · bun-test",
    ])
    expect(evidence[2]?.message).toContain("Assertion failed")
  })
})
'''
)

model = root / "apps/tui/src/board-model.ts"
text = model.read_text()
text = text.replace(
    'import type { WorkflowJob, WorkflowStatus } from "./workflow-jobs"\n',
    'import { workflowFailureSummary } from "./workflow-failure"\nimport type { WorkflowJob, WorkflowStatus } from "./workflow-jobs"\n',
)
text = text.replace(
    'return workflow.failure_message || "The workflow failed and needs a retry or inspection"',
    "return workflowFailureSummary(workflow)",
)
old = '''      label:
        item.workflow.status === "WAITING_FOR_APPROVAL"
          ? "Review and decide"
          : "Open workflow details",
      description: "See progress, checks, review findings, changed files, and diff.",
      tone: item.workflow.status === "WAITING_FOR_APPROVAL" ? "warning" : "neutral",'''
new = '''      label:
        item.workflow.status === "WAITING_FOR_APPROVAL"
          ? "Review and decide"
          : item.workflow.status === "FAILED"
            ? "See why it failed"
            : "Open workflow details",
      description:
        item.workflow.status === "FAILED"
          ? "Show the failing stage, daemon message, workspace, executor, checks, and reviewer evidence."
          : "See progress, checks, review findings, changed files, and diff.",
      tone:
        item.workflow.status === "WAITING_FOR_APPROVAL"
          ? "warning"
          : item.workflow.status === "FAILED"
            ? "danger"
            : "neutral",'''
if old not in text:
    raise SystemExit("board-model action block not found")
model.write_text(text.replace(old, new))

screen = root / "apps/tui/src/board-screen.tsx"
text = screen.read_text()
text = text.replace(
    'import { createWorkflowJob, performWorkflowAction, type WorkflowJob } from "./workflow-jobs"\n',
    'import { workflowFailureSummary } from "./workflow-failure"\nimport { createWorkflowJob, performWorkflowAction, type WorkflowJob } from "./workflow-jobs"\n',
)
marker = '  const hasPlanningItems = items.some((item) => !item.workflow && item.plan.status === "PLANNING")\n'
replacement = (
    marker
    + '  const selectedFailure =\n'
    + '    selectedItem?.workflow?.status === "FAILED"\n'
    + '      ? workflowFailureSummary(selectedItem.workflow)\n'
    + '      : ""\n'
    + '  const visibleMessage = selectedFailure && !busy ? selectedFailure : message\n'
)
if marker not in text:
    raise SystemExit("board selected marker not found")
text = text.replace(marker, replacement)
old = '''      {message ? (
        <Banner
          tone={message.toLowerCase().includes("failed") ? "danger" : busy ? "warning" : "accent"}
        >
          <text fg={busy ? colors.warning : colors.muted}>{message}</text>
        </Banner>
      ) : null}'''
new = '''      {visibleMessage ? (
        <Banner
          tone={
            selectedFailure && !busy
              ? "danger"
              : visibleMessage.toLowerCase().includes("failed")
                ? "danger"
                : busy
                  ? "warning"
                  : "accent"
          }
        >
          <text
            fg={selectedFailure && !busy ? colors.danger : busy ? colors.warning : colors.muted}
            wrapMode="word"
          >
            {visibleMessage}
          </text>
        </Banner>
      ) : null}'''
if old not in text:
    raise SystemExit("board message banner not found")
screen.write_text(text.replace(old, new))

detail = root / "apps/tui/src/board-detail.tsx"
text = detail.read_text()
text = text.replace(
    'import { workflowStatusLabel, workflowTone } from "./board-model"\n',
    'import { workflowStatusLabel, workflowTone } from "./board-model"\nimport { collectWorkflowFailureEvidence, workflowFailureSummary } from "./workflow-failure"\n',
)
marker = '''  const canDecide =
    workflow?.status === "WAITING_FOR_APPROVAL" &&'''
replacement = '''  const failureEvidence = workflow
    ? collectWorkflowFailureEvidence({
        workflow,
        workspace: snapshot.workspace,
        execution: snapshot.execution,
        checkSteps: snapshot.checkSteps,
        review: snapshot.review,
      })
    : []

  const canDecide =
    workflow?.status === "WAITING_FOR_APPROVAL" &&'''
if marker not in text:
    raise SystemExit("detail canDecide marker not found")
text = text.replace(marker, replacement)
chips = '''      </box>

      {state === "loading" ? ('''
chips_replacement = '''      </box>

      {workflow.status === "FAILED" ? (
        <Banner tone="danger">
          <text fg={colors.danger} attributes={BOLD}>
            Why the workflow stopped
          </text>
          <text fg={colors.text} wrapMode="word">
            {workflowFailureSummary(workflow)}
          </text>
          <text fg={colors.muted}>Fix the cause, press Esc, then choose Retry workflow.</text>
        </Banner>
      ) : null}

      {state === "loading" ? ('''
if chips not in text:
    raise SystemExit("detail chip marker not found")
text = text.replace(chips, chips_replacement)
progress = '''          <box flexDirection="column" gap={1}>
            <Section title="Progress" action="the card moves automatically as stages finish">'''
progress_replacement = '''          <box flexDirection="column" gap={1}>
            {failureEvidence.length > 0 ? (
              <Section title="Failure evidence" action="fix the cause, then retry from Board">
                <Card tone="danger">
                  {failureEvidence.map((failure, index) => (
                    <box key={`${failure.source}:${index}`} flexDirection="column" gap={0}>
                      <text fg={colors.danger} attributes={BOLD}>
                        {failure.code ? `${failure.source} · ${failure.code}` : failure.source}
                      </text>
                      <text fg={colors.text} wrapMode="word">
                        {failure.message}
                      </text>
                    </box>
                  ))}
                </Card>
              </Section>
            ) : null}

            <Section title="Progress" action="the card moves automatically as stages finish">'''
if progress not in text:
    raise SystemExit("detail progress marker not found")
detail.write_text(text.replace(progress, progress_replacement))

model_test = root / "apps/tui/src/board-model.test.ts"
text = model_test.read_text()
old = ''').toEqual(["open-details", "retry"])
  })
})
'''
new = ''').toEqual(["open-details", "retry"])
    const failureActions = boardActions(
      createBoardItem(project, plan("READY"), {
        ...workflow("FAILED"),
        failure_code: "WORKSPACE_PREPARATION_FAILED",
        failure_message: "source repository has uncommitted changes",
      }),
    )
    expect(failureActions[0]?.label).toBe("See why it failed")
    expect(
      createBoardItem(project, plan("READY"), {
        ...workflow("FAILED"),
        failure_code: "WORKSPACE_PREPARATION_FAILED",
        failure_message: "source repository has uncommitted changes",
      }).attentionReason,
    ).toContain("source repository has uncommitted changes")
  })
})
'''
if old not in text:
    raise SystemExit("board model test marker not found")
model_test.write_text(text.replace(old, new))
