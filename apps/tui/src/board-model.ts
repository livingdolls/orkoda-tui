import type { Plan } from "./plans"
import type { Project, ProjectRepository } from "./projects"
import type { StatusTone } from "./ui"
import type { WorkflowJob, WorkflowStatus } from "./workflow-jobs"

export const boardColumns = ["PLANNING", "READY", "WORKING", "NEEDS_USER", "DONE"] as const

export type BoardColumn = (typeof boardColumns)[number]

export type BoardItem = {
  id: string
  project: Project
  repository?: ProjectRepository
  plan: Plan
  workflow?: WorkflowJob
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
  WORKING: "Working",
  NEEDS_USER: "Needs You",
  DONE: "Done",
}

export const columnDescriptions: Record<BoardColumn, string> = {
  PLANNING: "Work that is still being described or prepared.",
  READY: "Prepared work that can start or is waiting for an agent.",
  WORKING: "The agents, checks, reviewer, or publisher are active.",
  NEEDS_USER: "A question, decision, failure, or revision needs your attention.",
  DONE: "Completed, rejected, or cancelled work.",
}

export function createBoardItem(project: Project, plan: Plan, workflow?: WorkflowJob): BoardItem {
  return {
    id: `plan:${plan.id}`,
    project,
    repository: project.repositories[0],
    plan,
    workflow,
    column: resolveBoardColumn(plan, workflow),
    displayStatus: resolveDisplayStatus(plan, workflow),
    attentionReason: resolveAttentionReason(plan, workflow),
    updatedAt: workflow?.updated_at ?? plan.updated_at,
  }
}

export function resolveBoardColumn(plan: Plan, workflow?: WorkflowJob): BoardColumn {
  if (!workflow) {
    if (plan.status === "NEEDS_INPUT") return "NEEDS_USER"
    if (plan.status === "READY" || plan.status === "APPROVED") return "READY"
    if (plan.status === "ARCHIVED") return "DONE"
    return "PLANNING"
  }

  switch (workflow.status) {
    case "READY":
    case "WORKSPACE_PREPARING":
    case "QUEUED":
      return "READY"
    case "EXECUTING":
    case "CHECKING":
    case "REVIEWING":
    case "APPROVED":
    case "PUBLISHING":
      return "WORKING"
    case "WAITING_FOR_APPROVAL":
    case "REVISION_REQUIRED":
    case "FAILED":
      return "NEEDS_USER"
    case "COMPLETED":
    case "REJECTED":
    case "CANCELLED":
      return "DONE"
    default:
      return assertNever(workflow.status)
  }
}

export function resolveDisplayStatus(plan: Plan, workflow?: WorkflowJob): string {
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
  return workflowStatusLabel(workflow.status)
}

export function resolveAttentionReason(plan: Plan, workflow?: WorkflowJob): string | undefined {
  if (!workflow && plan.status === "NEEDS_INPUT") return "Answer the planning questions"
  if (!workflow) return undefined

  switch (workflow.status) {
    case "WAITING_FOR_APPROVAL":
      return "Review the changes and make a decision"
    case "REVISION_REQUIRED":
      return "Revision feedback is ready for the next execution"
    case "FAILED":
      return workflow.failure_message || "The workflow failed and needs a retry or inspection"
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
          ? "Review and decide"
          : "Open workflow details",
      description: "See progress, checks, review findings, changed files, and diff.",
      tone: item.workflow.status === "WAITING_FOR_APPROVAL" ? "warning" : "neutral",
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
      return "Agent is implementing the plan"
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

function assertNever(value: never): never {
  throw new Error(`Unhandled board status: ${String(value)}`)
}
