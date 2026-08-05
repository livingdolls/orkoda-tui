import type { Plan } from "./plans"
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
  | "restart"
  | "continue-8"
  | "continue-16"
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
  if (workflow.status === "FAILED" && isExecutorPaused(workflow)) {
    return `Executor paused · ${workflow.failure_code?.toLowerCase().replaceAll("_", " ")}`
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
      return isExecutorPaused(workflow)
        ? "Executor paused safely. Continue with more turns or inspect the iteration timeline."
        : workflowFailureSummary(workflow)
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
            ? isExecutorPaused(item.workflow)
              ? "Inspect Executor pause"
              : "See why it failed"
            : "Open workflow details",
      description:
        item.workflow.status === "FAILED"
          ? "Show the failing stage, daemon message, workspace, executor, checks, and reviewer evidence."
          : "See progress, checks, review findings, changed files, and diff.",
      tone:
        item.workflow.status === "WAITING_FOR_APPROVAL"
          ? "warning"
          : item.workflow.status === "FAILED"
            ? isExecutorPaused(item.workflow)
              ? "warning"
              : "danger"
            : "neutral",
    },
  ]

  if (item.workflow.status === "FAILED") {
    if (isExecutorPaused(item.workflow)) {
      actions.push(
        {
          id: "continue-8",
          label: "Continue Executor · +8 turns",
          description: "Resume from the current workspace with eight additional turns.",
          tone: "warning",
        },
        {
          id: "continue-16",
          label: "Continue Executor · +16 turns",
          description: "Resume a larger unfinished task with sixteen additional turns.",
          tone: "accent",
        },
      )
    } else {
      actions.push({
        id: "retry",
        label: "Retry workflow",
        description: "Retry only the failed stage using the current workspace.",
        tone: "warning",
      })
    }
    actions.push({
      id: "restart",
      label: "Restart from beginning",
      description:
        "Discard the current workspace changes and rerun this workflow from its pinned base commit. Existing evidence is preserved.",
      tone: "danger",
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

export function isExecutorPaused(workflow: WorkflowJob): boolean {
  return new Set([
    "EXECUTOR_BUDGET_EXHAUSTED",
    "EXECUTOR_NO_PROGRESS",
    "EXECUTOR_REPEATED_TOOL_FAILURE",
    "EXECUTOR_REPEATED_ACTION",
    "EXECUTOR_TOOL_CALL_LIMIT",
  ]).has(workflow.failure_code ?? "")
}
