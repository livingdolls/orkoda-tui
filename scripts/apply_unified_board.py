from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def write(path: str, content: str) -> None:
    (ROOT / path).write_text(content, encoding="utf-8")


def replace(path: str, old: str, new: str) -> None:
    target = ROOT / path
    content = target.read_text(encoding="utf-8")
    if old not in content:
        raise RuntimeError(f"expected snippet not found in {path}: {old[:120]!r}")
    target.write_text(content.replace(old, new), encoding="utf-8")


write(
    "apps/tui/src/board-model.ts",
    '''import type { Plan } from "./plans"
import type { Project, ProjectRepository } from "./projects"
import type { ReviewBoardCard } from "./review-board-data"
import type { StatusTone } from "./ui"
import { workflowFailureSummary } from "./workflow-failure"
import type { WorkflowJob, WorkflowStatus } from "./workflow-jobs"

export const boardColumns = [
  "PLANNING",
  "READY",
  "EXECUTING",
  "CHECKING",
  "AWAITING_REVIEW",
  "AI_REVIEWING",
  "ISSUES_FOUND",
  "REVISION",
  "RE_REVIEW",
  "APPROVAL",
  "DONE",
] as const

export type BoardColumn = (typeof boardColumns)[number]
export type BoardReviewProjection = Pick<ReviewBoardCard, "review">

export type BoardItem = {
  id: string
  project: Project
  repository?: ProjectRepository
  plan: Plan
  workflow?: WorkflowJob
  reviewCard?: ReviewBoardCard
  column: BoardColumn
  displayStatus: string
  attentionReason?: string
  updatedAt: string
}

export type BoardActionID =
  | "prepare-plan"
  | "answer-questions"
  | "start-work"
  | "open-details"
  | "retry"
  | "cancel"

export type BoardAction = {
  id: BoardActionID
  label: string
  description: string
  tone?: StatusTone
}

export const columnLabels: Record<BoardColumn, string> = {
  PLANNING: "Planning",
  READY: "Ready",
  EXECUTING: "Executing",
  CHECKING: "Checking",
  AWAITING_REVIEW: "Awaiting Review",
  AI_REVIEWING: "AI Reviewing",
  ISSUES_FOUND: "Issues Found",
  REVISION: "Revision",
  RE_REVIEW: "Re-review",
  APPROVAL: "Approval",
  DONE: "Done",
}

export const columnDescriptions: Record<BoardColumn, string> = {
  PLANNING: "Work is being described, prepared, or waiting for planning answers.",
  READY: "The plan is ready, the workspace is being prepared, or execution is queued.",
  EXECUTING: "The configured Executor Agent is implementing the plan.",
  CHECKING: "Automated checks are validating the latest execution.",
  AWAITING_REVIEW: "Implementation evidence is ready for the Reviewer Agent.",
  AI_REVIEWING: "The configured Reviewer Agent is inspecting immutable evidence.",
  ISSUES_FOUND: "Blocking findings or a review-stage failure requires attention.",
  REVISION: "The Executor is applying requested changes or waiting to resume revision.",
  RE_REVIEW: "A revised execution is being compared with previous findings.",
  APPROVAL: "Review evidence is ready for a human decision.",
  DONE: "Approved, published, completed, rejected, cancelled, or archived work.",
}

export function createBoardItem(
  project: Project,
  plan: Plan,
  workflow?: WorkflowJob,
  reviewCard?: ReviewBoardCard,
): BoardItem {
  return {
    id: `plan:${plan.id}`,
    project,
    repository: project.repositories[0],
    plan,
    workflow,
    reviewCard,
    column: resolveBoardColumn(plan, workflow, reviewCard),
    displayStatus: resolveDisplayStatus(plan, workflow, reviewCard),
    attentionReason: resolveAttentionReason(plan, workflow, reviewCard),
    updatedAt: reviewCard?.updatedAt ?? workflow?.updated_at ?? plan.updated_at,
  }
}

export function resolveBoardColumn(
  plan: Plan,
  workflow?: WorkflowJob,
  reviewCard?: BoardReviewProjection,
): BoardColumn {
  if (!workflow) {
    if (plan.status === "READY" || plan.status === "APPROVED") return "READY"
    if (plan.status === "ARCHIVED") return "DONE"
    return "PLANNING"
  }

  if (workflow.status === "FAILED") return resolveFailedColumn(workflow, reviewCard)

  switch (workflow.status) {
    case "READY":
    case "WORKSPACE_PREPARING":
      return "READY"
    case "QUEUED":
      return isRevision(workflow) ? "REVISION" : "READY"
    case "EXECUTING":
      return isRevision(workflow) ? "REVISION" : "EXECUTING"
    case "CHECKING":
      return "CHECKING"
    case "REVIEWING":
      if (reviewCard?.review?.status === "RUNNING") return "AI_REVIEWING"
      return isRevision(workflow) ? "RE_REVIEW" : "AWAITING_REVIEW"
    case "WAITING_FOR_APPROVAL":
      return hasBlockingReview(reviewCard) ? "ISSUES_FOUND" : "APPROVAL"
    case "REVISION_REQUIRED":
      return "REVISION"
    case "APPROVED":
    case "PUBLISHING":
    case "COMPLETED":
    case "REJECTED":
    case "CANCELLED":
      return "DONE"
    default:
      return assertNever(workflow.status)
  }
}

export function resolveDisplayStatus(
  plan: Plan,
  workflow?: WorkflowJob,
  reviewCard?: BoardReviewProjection,
): string {
  if (!workflow) {
    switch (plan.status) {
      case "DRAFT":
        return "Describe and prepare this work"
      case "PLANNING":
        return "AI is preparing the implementation plan"
      case "NEEDS_INPUT":
        return "AI has questions for you"
      case "READY":
      case "APPROVED":
        return "Ready to start"
      case "ARCHIVED":
        return "Archived"
      default:
        return assertNever(plan.status)
    }
  }
  if (workflow.status === "WAITING_FOR_APPROVAL" && hasBlockingReview(reviewCard)) {
    return `${reviewCard?.review?.blocking_issues ?? 0} blocking review issue(s)`
  }
  if (workflow.status === "REVIEWING" && reviewCard?.review?.status === "RUNNING") {
    return "Reviewer Agent is checking the changes"
  }
  if (workflow.status === "REVIEWING" && isRevision(workflow)) {
    return "Reviewer Agent is verifying the revision"
  }
  return workflowStatusLabel(workflow.status)
}

export function resolveAttentionReason(
  plan: Plan,
  workflow?: WorkflowJob,
  reviewCard?: BoardReviewProjection,
): string | undefined {
  if (!workflow && plan.status === "NEEDS_INPUT") return "Answer the planning questions"
  if (!workflow) return undefined

  switch (workflow.status) {
    case "WAITING_FOR_APPROVAL":
      if (hasBlockingReview(reviewCard)) {
        return `${reviewCard?.review?.blocking_issues ?? 0} blocking finding(s) require revision`
      }
      return "Review the changes and make a decision"
    case "REVISION_REQUIRED":
      return "Revision feedback is ready for the next execution"
    case "FAILED":
      return workflowFailureSummary(workflow)
    default:
      return undefined
  }
}

export function boardActions(item: BoardItem): BoardAction[] {
  if (!item.workflow) {
    if (item.plan.status === "NEEDS_INPUT") {
      return [
        {
          id: "answer-questions",
          label: "Answer AI questions",
          description: "Provide the missing information so planning can continue.",
          tone: "warning",
        },
      ]
    }
    if (item.plan.status === "READY" || item.plan.status === "APPROVED") {
      return [
        {
          id: "start-work",
          label: "Start the work",
          description: "Create an isolated workspace and let the agents implement the plan.",
          tone: "accent",
        },
      ]
    }
    if (item.plan.status !== "ARCHIVED") {
      return [
        {
          id: "prepare-plan",
          label: "Prepare this plan",
          description:
            "Scan the repository, normalize the requirement, and run the Planning Agent.",
          tone: "accent",
        },
      ]
    }
    return []
  }

  if (item.workflow.status === "READY") {
    return [
      {
        id: "start-work",
        label: "Start the workflow",
        description: "Resume the prepared workflow and create its isolated workspace.",
        tone: "accent",
      },
      {
        id: "open-details",
        label: "Open workflow details",
        description: "Inspect the prepared workflow before starting it.",
        tone: "neutral",
      },
    ]
  }

  const actions: BoardAction[] = [
    {
      id: "open-details",
      label:
        item.workflow.status === "WAITING_FOR_APPROVAL"
          ? hasBlockingReview(item.reviewCard)
            ? "Review blocking findings"
            : "Review and decide"
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
            : "neutral",
    },
  ]

  if (item.workflow.status === "FAILED") {
    actions.push({
      id: "retry",
      label: "Retry workflow",
      description: "Retry the failed stage using the workflow's current version.",
      tone: "warning",
    })
  }

  if (isActiveWorkflow(item.workflow.status)) {
    actions.push({
      id: "cancel",
      label: "Cancel workflow",
      description: "Cooperatively stop the active work and preserve available evidence.",
      tone: "danger",
    })
  }

  return actions
}

export function workflowStatusLabel(status: WorkflowStatus): string {
  switch (status) {
    case "READY":
      return "Ready to start"
    case "WORKSPACE_PREPARING":
      return "Preparing an isolated workspace"
    case "QUEUED":
      return "Waiting for an agent"
    case "EXECUTING":
      return "Executor Agent is implementing the plan"
    case "CHECKING":
      return "Running automated checks"
    case "REVIEWING":
      return "AI Reviewer is checking the changes"
    case "WAITING_FOR_APPROVAL":
      return "Ready for your review"
    case "REVISION_REQUIRED":
      return "Revision requested"
    case "APPROVED":
      return "Approved"
    case "PUBLISHING":
      return "Creating the commit or pull request"
    case "COMPLETED":
      return "Completed"
    case "FAILED":
      return "Failed — action required"
    case "REJECTED":
      return "Rejected"
    case "CANCELLED":
      return "Cancelled"
    default:
      return assertNever(status)
  }
}

export function workflowTone(status: WorkflowStatus): StatusTone {
  switch (status) {
    case "COMPLETED":
    case "APPROVED":
      return "success"
    case "FAILED":
    case "REJECTED":
    case "CANCELLED":
      return "danger"
    case "WAITING_FOR_APPROVAL":
    case "REVISION_REQUIRED":
      return "warning"
    default:
      return "accent"
  }
}

export function isActiveWorkflow(status: WorkflowStatus): boolean {
  return !["COMPLETED", "FAILED", "REJECTED", "CANCELLED"].includes(status)
}

function isRevision(workflow: WorkflowJob): boolean {
  return workflow.revision_count > 0 || workflow.execution_version > 1
}

function hasBlockingReview(reviewCard?: BoardReviewProjection): boolean {
  return (
    reviewCard?.review?.verdict === "REQUEST_REVISION" ||
    (reviewCard?.review?.blocking_issues ?? 0) > 0
  )
}

function resolveFailedColumn(
  workflow: WorkflowJob,
  reviewCard?: BoardReviewProjection,
): BoardColumn {
  switch (workflow.retry_status) {
    case "WORKSPACE_PREPARING":
    case "READY":
      return "READY"
    case "QUEUED":
    case "EXECUTING":
      return isRevision(workflow) ? "REVISION" : "EXECUTING"
    case "CHECKING":
      return "CHECKING"
    case "REVIEWING":
      return "ISSUES_FOUND"
    case "PUBLISHING":
      return "DONE"
  }

  const failureCode = workflow.failure_code ?? ""
  if (failureCode.startsWith("WORKSPACE")) return "READY"
  if (failureCode.startsWith("CHECK")) return "CHECKING"
  if (failureCode.startsWith("REVIEW") || reviewCard?.review?.status === "FAILED") {
    return "ISSUES_FOUND"
  }
  if (failureCode.startsWith("PUBLICATION") || failureCode.startsWith("PUBLISH")) return "DONE"
  return isRevision(workflow) ? "REVISION" : workflow.execution_version > 0 ? "EXECUTING" : "READY"
}

function assertNever(value: never): never {
  throw new Error(`Unhandled board status: ${String(value)}`)
}
''',
)

write(
    "apps/tui/src/board-data.ts",
    '''import { type BoardItem, createBoardItem } from "./board-model"
import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"
import { listPlans } from "./plans"
import { listProjects, type Project } from "./projects"
import { loadProjectReviewCards, type ReviewBoardCard } from "./review-board-data"
import { listWorkflowJobs, type WorkflowJob } from "./workflow-jobs"

type ProjectBoardJobs = {
  jobs: WorkflowJob[]
}

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }
type BoardFetch = (input: string | URL | Request, init?: RequestInit) => Promise<Response>

async function requestProjectBoardJobs(
  projectID: string,
  fetcher: BoardFetch = fetch,
): Promise<WorkflowJob[]> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 7000)
  try {
    const response = await requestWithDaemonAuth(
      fetcher,
      `${daemonBaseURL}/api/v1/projects/${projectID}/board`,
      {
        method: "GET",
        headers: { accept: "application/json" },
        signal: controller.signal,
      },
    )
    if (!response.ok) {
      let message = `Daemon returned HTTP ${response.status}`
      try {
        const payload = (await response.json()) as ErrorResponse
        message = payload.error?.message ?? message
      } catch {
        // Keep the status-based message for non-JSON responses.
      }
      throw new Error(message)
    }
    const payload = (await response.json()) as DataResponse<ProjectBoardJobs>
    return payload.data.jobs
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("Board request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

async function loadProjectBoard(project: Project) {
  const [plans, jobs, reviewCards] = await Promise.all([
    listPlans(project.id),
    requestProjectBoardJobs(project.id).catch(() => listWorkflowJobs(project.id)),
    loadProjectReviewCards(project).catch(() => [] as ReviewBoardCard[]),
  ])
  return { project, plans, jobs, reviewCards }
}

export async function loadBoardItems(): Promise<{
  projects: Project[]
  items: BoardItem[]
}> {
  const projects = await listProjects()
  const summaries = await Promise.all(projects.map(loadProjectBoard))
  const items = summaries.flatMap((summary) => {
    const latestJobByPlan = new Map<string, WorkflowJob>()
    for (const job of summary.jobs) {
      const current = latestJobByPlan.get(job.plan_id)
      if (!current || new Date(job.updated_at).getTime() > new Date(current.updated_at).getTime()) {
        latestJobByPlan.set(job.plan_id, job)
      }
    }
    const reviewByWorkflow = new Map(
      summary.reviewCards.map((reviewCard) => [reviewCard.workflow.id, reviewCard]),
    )
    return summary.plans.map((plan) => {
      const workflow = latestJobByPlan.get(plan.id)
      return createBoardItem(
        summary.project,
        plan,
        workflow,
        workflow ? reviewByWorkflow.get(workflow.id) : undefined,
      )
    })
  })
  items.sort(
    (left, right) => new Date(right.updatedAt).getTime() - new Date(left.updatedAt).getTime(),
  )
  return { projects, items }
}
''',
)

replace(
    "apps/tui/src/review-board-data.ts",
    "async function loadProjectCards(project: Project): Promise<ReviewBoardCard[]> {",
    "export async function loadProjectReviewCards(project: Project): Promise<ReviewBoardCard[]> {",
)
replace(
    "apps/tui/src/review-board-data.ts",
    "projects.map(loadProjectCards)",
    "projects.map(loadProjectReviewCards)",
)

write(
    "apps/tui/src/navigation.ts",
    '''export const screenDefinitions = [
  { id: "board", label: "Board", description: "Plan, execute, review & approve" },
  { id: "agents", label: "Agents", description: "AI roles & limits" },
  { id: "settings", label: "Settings", description: "Providers & budgets" },
  { id: "system", label: "System", description: "Daemon health" },
] as const

export const screens = screenDefinitions.map((screen) => screen.id)
export type Screen = (typeof screenDefinitions)[number]["id"]
export function isScreen(value: string): value is Screen {
  return screens.includes(value as Screen)
}
export function moveScreen(current: Screen, offset: number): Screen {
  const currentIndex = screens.indexOf(current)
  const nextIndex = (currentIndex + offset + screens.length) % screens.length
  return screens[nextIndex] ?? screens[0]
}
export function screenFromShortcut(keyName: string): Screen | undefined {
  const index = Number.parseInt(keyName, 10) - 1
  if (!Number.isInteger(index) || index < 0 || index >= screens.length) return undefined
  return screens[index]
}
export function screenLabel(screen: Screen): string {
  return screenDefinitions.find((item) => item.id === screen)?.label ?? screen
}
''',
)

write(
    "apps/tui/src/navigation.test.ts",
    '''import { describe, expect, test } from "bun:test"
import { isScreen, moveScreen, screenFromShortcut, screenLabel } from "./navigation"

describe("navigation", () => {
  test("accepts product areas", () => {
    expect(isScreen("board")).toBe(true)
    expect(isScreen("agents")).toBe(true)
    expect(isScreen("system")).toBe(true)
  })
  test("rejects removed screens", () => {
    expect(isScreen("review")).toBe(false)
    expect(isScreen("projects")).toBe(false)
    expect(isScreen("jobs")).toBe(false)
  })
  test("moves and wraps", () => {
    expect(moveScreen("board", 1)).toBe("agents")
    expect(moveScreen("system", 1)).toBe("board")
    expect(moveScreen("agents", -1)).toBe("board")
  })
  test("maps numeric shortcuts", () => {
    expect(screenFromShortcut("1")).toBe("board")
    expect(screenFromShortcut("2")).toBe("agents")
    expect(screenFromShortcut("4")).toBe("system")
    expect(screenFromShortcut("5")).toBeUndefined()
  })
  test("returns labels", () => {
    expect(screenLabel("board")).toBe("Board")
    expect(screenLabel("settings")).toBe("Settings")
  })
})
''',
)

write(
    "apps/tui/src/board-model.test.ts",
    '''import { describe, expect, test } from "bun:test"

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
    ).toEqual(["open-details", "retry"])
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
''',
)

write(
    "docs/review-kanban.md",
    '''# Unified Workflow Board

Review is part of the main Board. Orkoda does not expose a separate Review screen or a second state machine.

A card follows one durable workflow through these presentation columns:

1. Planning
2. Ready
3. Executing
4. Checking
5. Awaiting Review
6. AI Reviewing
7. Issues Found
8. Revision
9. Re-review
10. Approval
11. Done

The columns are read-only projections of plan, workflow, execution, check, review, approval, and publication records. Cards cannot be dragged manually.

Failures remain in the stage that failed. For example, a failed automated check remains in Checking, while a failed Reviewer run appears in Issues Found. Enter opens the shared workflow detail and Space opens every valid action. Retry uses the durable workflow retry target.

Review cards retain immutable Executor and Reviewer provider/model snapshots, execution version, review cycle, check totals, blocking findings, and previous-review comparison. Reviewer access remains read-only, and human approval remains bound to the latest execution version, base commit, checkpoint, and patch checksum.
''',
)

# Board screen: extend indexes and preserve review evidence during optimistic workflow updates.
replace(
    "apps/tui/src/board-screen.tsx",
    '''const initialIndexes: Record<BoardColumn, number> = {
  PLANNING: 0,
  READY: 0,
  WORKING: 0,
  NEEDS_USER: 0,
  DONE: 0,
}''',
    '''const initialIndexes: Record<BoardColumn, number> = {
  PLANNING: 0,
  READY: 0,
  EXECUTING: 0,
  CHECKING: 0,
  AWAITING_REVIEW: 0,
  AI_REVIEWING: 0,
  ISSUES_FOUND: 0,
  REVISION: 0,
  RE_REVIEW: 0,
  APPROVAL: 0,
  DONE: 0,
}''',
)
replace(
    "apps/tui/src/board-screen.tsx",
    "createBoardItem(candidate.project, candidate.plan, started)",
    "createBoardItem(candidate.project, candidate.plan, started, candidate.reviewCard)",
)
replace(
    "apps/tui/src/board-screen.tsx",
    "createBoardItem(candidate.project, candidate.plan, workflow)",
    "createBoardItem(candidate.project, candidate.plan, workflow, candidate.reviewCard)",
)
replace(
    "apps/tui/src/board-screen.tsx",
    "current ? createBoardItem(current.project, current.plan, workflow) : current",
    "current ? createBoardItem(current.project, current.plan, workflow, current.reviewCard) : current",
)
replace(
    "apps/tui/src/board-screen.tsx",
    '''candidate.id === detailItem.id
                ? createBoardItem(candidate.project, candidate.plan, workflow)
                : candidate''',
    '''candidate.id === detailItem.id
                ? createBoardItem(candidate.project, candidate.plan, workflow, candidate.reviewCard)
                : candidate''',
)
replace(
    "apps/tui/src/board-screen.tsx",
    'tone={column === "NEEDS_USER" && cards.length > 0 ? "warning" : "neutral"}',
    'tone={column === "ISSUES_FOUND" && cards.length > 0 ? "warning" : "neutral"}',
)
replace(
    "apps/tui/src/board-screen.tsx",
    'meta={`${activeFilter?.name ?? "All projects"} · ${activeOnly ? "active only" : "all work"}`}',
    'meta={`${activeFilter?.name ?? "All projects"} · ${activeOnly ? "active only" : "all work"} · stage ${activeColumnIndex + 1}/${boardColumns.length}`}',
)

board_screen = (ROOT / "apps/tui/src/board-screen.tsx").read_text(encoding="utf-8")
card_marker = "function BoardCard({ item, selected }: { item: BoardItem; selected: boolean }) {"
if card_marker not in board_screen:
    raise RuntimeError("BoardCard marker not found")
board_screen = board_screen.split(card_marker, 1)[0] + '''function BoardCard({ item, selected }: { item: BoardItem; selected: boolean }) {
  const tone = item.workflow
    ? workflowTone(item.workflow.status)
    : item.column === "PLANNING" && item.plan.status === "NEEDS_INPUT"
      ? "warning"
      : item.column === "READY"
        ? "accent"
        : item.column === "DONE"
          ? "success"
          : "neutral"
  const reviewCard = item.reviewCard
  const showExecutor =
    item.column === "EXECUTING" || item.column === "CHECKING" || item.column === "REVISION"
  const agent = reviewCard ? (showExecutor ? reviewCard.executor : reviewCard.reviewer) : undefined
  const agentLabel = showExecutor ? "Executor" : "Reviewer"
  return (
    <box
      flexDirection="column"
      gap={1}
      padding={1}
      borderStyle="rounded"
      borderColor={selected ? colors.accent : colors.line}
      backgroundColor={selected ? colors.accentTint : colors.raised}
    >
      <box flexDirection="row" gap={1}>
        <text fg={selected ? colors.accent : colors.faint}>{selected ? "▸" : " "}</text>
        <text fg={colors.text} attributes={BOLD} wrapMode="word">
          {truncate(item.plan.title, 42)}
        </text>
      </box>
      <text fg={colors.faint}>{truncate(item.project.name, 34)}</text>
      <Chip label={truncate(item.displayStatus, 38)} tone={tone} />
      {agent ? (
        <text fg={colors.muted} wrapMode="word">
          {`${agentLabel}: ${truncate(agent.provider, 14)} / ${truncate(agent.model, 22)}`}
        </text>
      ) : null}
      {reviewCard?.check ? (
        <text fg={reviewCard.check.failed_steps > 0 ? colors.warning : colors.faint}>
          {`Checks: ${reviewCard.check.passed_steps}/${reviewCard.check.total_steps} passed`}
        </text>
      ) : null}
      {reviewCard && reviewCard.issues.length > 0 ? (
        <text fg={reviewCard.review?.blocking_issues ? colors.warning : colors.faint}>
          {`${reviewCard.issues.length} finding(s) · ${reviewCard.review?.blocking_issues ?? 0} blocking`}
        </text>
      ) : null}
      {item.attentionReason ? (
        <text fg={colors.warning} wrapMode="word">
          {truncate(item.attentionReason, 64)}
        </text>
      ) : null}
      <box flexDirection="row" justifyContent="space-between">
        <text fg={colors.faint}>{item.repository?.current_branch || "no branch"}</text>
        {item.workflow ? (
          <text fg={colors.faint}>{`execution v${item.workflow.execution_version}`}</text>
        ) : (
          <Key>{item.plan.status === "NEEDS_INPUT" ? "Enter" : "Space"}</Key>
        )}
      </box>
    </box>
  )
}
'''
(ROOT / "apps/tui/src/board-screen.tsx").write_text(board_screen, encoding="utf-8")

# App navigation: remove the standalone Review area and fold its help into Board.
replace("apps/tui/src/app.tsx", 'import { ReviewBoardScreen } from "./review-board-screen"\n', "")
replace(
    "apps/tui/src/app.tsx",
    '  const [reviewInteractionActive, setReviewInteractionActive] = useState(false)\n',
    "",
)
replace(
    "apps/tui/src/app.tsx",
    "    if (boardInteractionActive || reviewInteractionActive) return",
    "    if (boardInteractionActive) return",
)
replace(
    "apps/tui/src/app.tsx",
    '    if (activeScreen === "board" || activeScreen === "review") return',
    '    if (activeScreen === "board") return',
)
replace(
    "apps/tui/src/app.tsx",
    '    activeScreen === "board" || activeScreen === "review"',
    '    activeScreen === "board"',
)
replace("apps/tui/src/app.tsx", '{ key: "1–5", label: "switch area" }', '{ key: "1–4", label: "switch area" }')
replace("apps/tui/src/app.tsx", '{ key: "1–5", label: "jump" }', '{ key: "1–4", label: "jump" }')
replace(
    "apps/tui/src/app.tsx",
    '''          ) : activeScreen === "review" ? (
            <ReviewBoardScreen
              connection={connection}
              lastEvent={lastEvent}
              onInteractionChange={setReviewInteractionActive}
            />
          ) : activeScreen === "agents" ? (''',
    '''          ) : activeScreen === "agents" ? (''',
)
replace(
    "apps/tui/src/app.tsx",
    '''  const reviewShortcuts: Shortcut[] = [
    { key: "←→", label: "move between review stages" },
    { key: "↑↓", label: "select a review card" },
    { key: "Enter", label: "open evidence and human decision" },
    { key: "T", label: "retry a failed review stage" },
    { key: "Tab", label: "cycle project filter" },
    { key: "F", label: "toggle active-only / history" },
  ]
''',
    "",
)
replace(
    "apps/tui/src/app.tsx",
    '{ key: "1–5", label: "jump to Board, Review, Agents, Settings, or System" }',
    '{ key: "1–4", label: "jump to Board, Agents, Settings, or System" }',
)
replace(
    "apps/tui/src/app.tsx",
    '''      <Section
        title={screen === "board" ? "Board" : screen === "review" ? "Review" : "Current area"}
      >
        <Card>
          <KeyHints
            shortcuts={
              screen === "board"
                ? boardShortcuts
                : screen === "review"
                  ? reviewShortcuts
                  : [{ key: "←→", label: "switch area" }]
            }
          />
        </Card>
      </Section>''',
    '''      <Section title={screen === "board" ? "Unified Board" : "Current area"}>
        <Card>
          <KeyHints
            shortcuts={screen === "board" ? boardShortcuts : [{ key: "←→", label: "switch area" }]}
          />
        </Card>
      </Section>''',
)
replace(
    "apps/tui/src/app.tsx",
    '{ key: "A / V / X", label: "approve, revise, or reject inside workflow detail" },',
    '''{ key: "A / V / X", label: "approve, revise, or reject inside workflow detail" },
    { key: "R", label: "refresh all execution and review stages" },''',
)

replace(
    "apps/tui/src/app.e2e.test.tsx",
    'frame.includes("Needs You") &&',
    'frame.includes("Approval") &&',
)

# The old screen is intentionally removed; its data/model modules remain shared by the unified Board.
review_screen = ROOT / "apps/tui/src/review-board-screen.tsx"
if review_screen.exists():
    review_screen.unlink()
