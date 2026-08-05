import { describe, expect, test } from "bun:test"

import {
  boardActions,
  createBoardItem,
  resolveBoardColumn,
  workflowStatusLabel,
} from "./board-model"
import type { Plan } from "./plans"
import type { Project } from "./projects"
import type { ReviewRun } from "./reviews"
import type { WorkflowJob, WorkflowStatus } from "./workflow-jobs"

const project: Project = {
  id: "project-1",
  name: "Example",
  repositories: [],
  created_at: "2026-08-04T00:00:00Z",
  updated_at: "2026-08-04T00:00:00Z",
}

function plan(status: Plan["status"]): Plan {
  return {
    id: "plan-1",
    project_id: project.id,
    title: "Build a kanban board",
    status,
    current_version: 1,
    versions: [],
    created_at: "2026-08-04T00:00:00Z",
    updated_at: "2026-08-04T00:00:00Z",
  }
}

function workflow(status: WorkflowStatus, overrides: Partial<WorkflowJob> = {}): WorkflowJob {
  return {
    id: "job-1",
    project_id: project.id,
    plan_id: "plan-1",
    plan_version_id: "version-1",
    repository_id: "repo-1",
    base_branch: "main",
    base_commit_sha: "abc123",
    status,
    version: 1,
    execution_version: 1,
    revision_count: 0,
    limits: {
      max_revisions: 3,
      max_stage_attempts: 3,
      max_tool_calls: 50,
      wall_clock_seconds: 3600,
    },
    cancellation_requested: false,
    created_at: "2026-08-04T00:00:00Z",
    updated_at: "2026-08-04T00:00:00Z",
    ...overrides,
  }
}

function review(
  status: ReviewRun["status"],
  verdict?: ReviewRun["verdict"],
  blockingIssues = 0,
): ReviewRun {
  return {
    id: "review-1",
    workflow_job_id: "job-1",
    execution_id: "execution-1",
    execution_version: 1,
    check_run_id: "check-1",
    checkpoint_id: "checkpoint-1",
    agent_settings_version: 1,
    provider: "openai",
    model: "reviewer-model",
    status,
    verdict,
    summary: "Review summary",
    total_issues: blockingIssues,
    blocking_issues: blockingIssues,
    created_at: "2026-08-04T00:00:00Z",
    updated_at: "2026-08-04T00:00:00Z",
  }
}

describe("unified board status mapping", () => {
  test("keeps planning questions on the planning stage", () => {
    expect(resolveBoardColumn(plan("DRAFT"))).toBe("PLANNING")
    expect(resolveBoardColumn(plan("NEEDS_INPUT"))).toBe("PLANNING")
    expect(resolveBoardColumn(plan("READY"))).toBe("READY")
    expect(resolveBoardColumn(plan("ARCHIVED"))).toBe("DONE")
  })

  test.each([
    ["READY", "READY"],
    ["WORKSPACE_PREPARING", "READY"],
    ["QUEUED", "READY"],
    ["EXECUTING", "EXECUTING"],
    ["CHECKING", "CHECKING"],
    ["REVIEWING", "AWAITING_REVIEW"],
    ["WAITING_FOR_APPROVAL", "APPROVAL"],
    ["REVISION_REQUIRED", "REVISION"],
    ["COMPLETED", "DONE"],
    ["REJECTED", "DONE"],
    ["CANCELLED", "DONE"],
  ] as const)("maps workflow %s to %s", (status, column) => {
    expect(resolveBoardColumn(plan("READY"), workflow(status))).toBe(column)
    expect(workflowStatusLabel(status).length).toBeGreaterThan(0)
  })

  test("projects active review and revision stages into the same board", () => {
    expect(
      resolveBoardColumn(plan("READY"), workflow("REVIEWING"), {
        review: review("RUNNING"),
      }),
    ).toBe("AI_REVIEWING")
    expect(
      resolveBoardColumn(plan("READY"), workflow("WAITING_FOR_APPROVAL"), {
        review: review("COMPLETED", "REQUEST_REVISION", 2),
      }),
    ).toBe("ISSUES_FOUND")
    expect(
      resolveBoardColumn(
        plan("READY"),
        workflow("REVIEWING", { execution_version: 2, revision_count: 1 }),
      ),
    ).toBe("RE_REVIEW")
    expect(
      resolveBoardColumn(
        plan("READY"),
        workflow("EXECUTING", { execution_version: 2, revision_count: 1 }),
      ),
    ).toBe("REVISION")
  })

  test("keeps failures on their failed stage", () => {
    expect(
      resolveBoardColumn(
        plan("READY"),
        workflow("FAILED", { retry_status: "CHECKING", failure_code: "CHECKS_FAILED" }),
      ),
    ).toBe("CHECKING")
    expect(
      resolveBoardColumn(
        plan("READY"),
        workflow("FAILED", { retry_status: "REVIEWING", failure_code: "REVIEW_FAILED" }),
      ),
    ).toBe("ISSUES_FOUND")
  })

  test("keeps one stable card id when a plan becomes a workflow", () => {
    expect(createBoardItem(project, plan("READY")).id).toBe("plan:plan-1")
    expect(createBoardItem(project, plan("READY"), workflow("EXECUTING")).id).toBe("plan:plan-1")
  })

  test("offers contextual failure actions", () => {
    expect(boardActions(createBoardItem(project, plan("DRAFT"))).map((item) => item.id)).toEqual([
      "prepare-plan",
    ])
    expect(boardActions(createBoardItem(project, plan("READY"))).map((item) => item.id)).toEqual([
      "start-work",
    ])
    expect(
      boardActions(createBoardItem(project, plan("READY"), workflow("FAILED"))).map(
        (item) => item.id,
      ),
    ).toEqual(["open-details", "retry", "restart"])
    const failure = createBoardItem(
      project,
      plan("READY"),
      workflow("FAILED", {
        retry_status: "WORKSPACE_PREPARING",
        failure_code: "WORKSPACE_PREPARATION_FAILED",
        failure_message: "source repository has uncommitted changes",
      }),
    )
    expect(failure.column).toBe("READY")
    expect(boardActions(failure)[0]?.label).toBe("See why it failed")
    expect(failure.attentionReason).toContain("source repository has uncommitted changes")
  })
})

test("continues a paused Executor instead of blind retry", () => {
  const pausedWorkflow = workflow("FAILED", {
    status: "FAILED",
    retry_status: "EXECUTING",
    failure_code: "EXECUTOR_BUDGET_EXHAUSTED",
  })
  const item = createBoardItem(project, plan("READY"), pausedWorkflow)
  expect(boardActions(item).map((action) => action.id)).toEqual([
    "open-details",
    "continue-8",
    "continue-16",
    "restart",
  ])
  expect(item.displayStatus).toContain("Executor paused")
})

test("offers restart from beginning for every failed workflow", () => {
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
})
