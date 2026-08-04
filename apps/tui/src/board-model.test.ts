import { describe, expect, test } from "bun:test"

import {
  boardActions,
  createBoardItem,
  resolveBoardColumn,
  workflowStatusLabel,
} from "./board-model"
import type { Plan } from "./plans"
import type { Project } from "./projects"
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

function workflow(status: WorkflowStatus): WorkflowJob {
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
  }
}

describe("board status mapping", () => {
  test("maps plan-only lifecycle", () => {
    expect(resolveBoardColumn(plan("DRAFT"))).toBe("PLANNING")
    expect(resolveBoardColumn(plan("NEEDS_INPUT"))).toBe("NEEDS_USER")
    expect(resolveBoardColumn(plan("READY"))).toBe("READY")
    expect(resolveBoardColumn(plan("ARCHIVED"))).toBe("DONE")
  })

  test.each([
    ["READY", "READY"],
    ["WORKSPACE_PREPARING", "READY"],
    ["QUEUED", "READY"],
    ["EXECUTING", "WORKING"],
    ["CHECKING", "WORKING"],
    ["REVIEWING", "WORKING"],
    ["WAITING_FOR_APPROVAL", "NEEDS_USER"],
    ["REVISION_REQUIRED", "NEEDS_USER"],
    ["FAILED", "NEEDS_USER"],
    ["COMPLETED", "DONE"],
    ["REJECTED", "DONE"],
    ["CANCELLED", "DONE"],
  ] as const)("maps workflow %s to %s", (status, column) => {
    expect(resolveBoardColumn(plan("READY"), workflow(status))).toBe(column)
    expect(workflowStatusLabel(status).length).toBeGreaterThan(0)
  })

  test("keeps one stable card id when a plan becomes a workflow", () => {
    expect(createBoardItem(project, plan("READY")).id).toBe("plan:plan-1")
    expect(createBoardItem(project, plan("READY"), workflow("EXECUTING")).id).toBe("plan:plan-1")
  })

  test("offers only valid contextual actions", () => {
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
    ).toEqual(["open-details", "retry"])
  })
})
