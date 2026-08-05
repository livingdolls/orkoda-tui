import type { ReviewIssue, ReviewRun } from "./reviews"
import type { WorkflowJob } from "./workflow-jobs"

export const reviewColumns = [
  "AWAITING_REVIEW",
  "AI_REVIEWING",
  "ISSUES_FOUND",
  "REVISION_IN_PROGRESS",
  "RE_REVIEW",
  "READY_FOR_APPROVAL",
  "APPROVED",
] as const

export type ReviewColumn = (typeof reviewColumns)[number]

export const reviewColumnLabels: Record<ReviewColumn, string> = {
  AWAITING_REVIEW: "Awaiting Review",
  AI_REVIEWING: "AI Reviewing",
  ISSUES_FOUND: "Issues Found",
  REVISION_IN_PROGRESS: "Revision",
  RE_REVIEW: "Re-review",
  READY_FOR_APPROVAL: "Approval",
  APPROVED: "Approved",
}

export const reviewColumnDescriptions: Record<ReviewColumn, string> = {
  AWAITING_REVIEW: "Implementation or checks are finishing before review starts.",
  AI_REVIEWING: "The configured Reviewer Agent is inspecting immutable evidence.",
  ISSUES_FOUND: "Blocking findings or a review-stage failure needs attention.",
  REVISION_IN_PROGRESS: "The Executor is applying requested changes.",
  RE_REVIEW: "A newer execution version is being checked against prior findings.",
  READY_FOR_APPROVAL: "Review evidence is ready for a human decision.",
  APPROVED: "The reviewed execution was approved or published.",
}

export function isReviewRelevant(workflow: WorkflowJob): boolean {
  if (["REJECTED", "CANCELLED"].includes(workflow.status)) return false
  if (["READY", "WORKSPACE_PREPARING"].includes(workflow.status)) return false
  return (
    workflow.execution_version > 0 ||
    [
      "CHECKING",
      "REVIEWING",
      "WAITING_FOR_APPROVAL",
      "APPROVED",
      "PUBLISHING",
      "COMPLETED",
      "FAILED",
    ].includes(workflow.status)
  )
}

export function resolveReviewColumn(workflow: WorkflowJob, review?: ReviewRun): ReviewColumn {
  if (["APPROVED", "PUBLISHING", "COMPLETED"].includes(workflow.status)) return "APPROVED"
  if (workflow.status === "WAITING_FOR_APPROVAL") {
    return review?.verdict === "REQUEST_REVISION" || (review?.blocking_issues ?? 0) > 0
      ? "ISSUES_FOUND"
      : "READY_FOR_APPROVAL"
  }
  if (workflow.status === "REVISION_REQUIRED") return "REVISION_IN_PROGRESS"
  if (["QUEUED", "EXECUTING", "CHECKING"].includes(workflow.status)) {
    return workflow.revision_count > 0 || workflow.execution_version > 1
      ? "REVISION_IN_PROGRESS"
      : "AWAITING_REVIEW"
  }
  if (workflow.status === "REVIEWING") {
    if (review?.status === "RUNNING") return "AI_REVIEWING"
    if (workflow.execution_version > 1 || workflow.revision_count > 0) return "RE_REVIEW"
    return "AWAITING_REVIEW"
  }
  if (workflow.status === "FAILED") return "ISSUES_FOUND"
  return "AWAITING_REVIEW"
}

export function reviewCardStatus(workflow: WorkflowJob, review?: ReviewRun): string {
  if (workflow.status === "FAILED") {
    return workflow.failure_message || "Review pipeline failed"
  }
  if (review?.status === "FAILED") return review.failure_message || "Reviewer failed"
  if (review?.status === "RUNNING") return "Reviewer Agent is checking the latest execution"
  if (review?.verdict === "REQUEST_REVISION") {
    return `${review.blocking_issues} blocking issue(s) require revision`
  }
  if (review?.verdict === "APPROVE") return "Reviewer recommends approval"
  if (workflow.status === "REVISION_REQUIRED") return "Revision feedback is queued for Executor"
  return reviewColumnDescriptions[resolveReviewColumn(workflow, review)]
}

export type IssueResolution = "NEW" | "STILL_PRESENT" | "PARTIALLY_RESOLVED" | "RESOLVED"

export type ReviewIssueComparison = {
  key: string
  status: IssueResolution
  current?: ReviewIssue
  previous?: ReviewIssue
}

const severityRank: Record<ReviewIssue["severity"], number> = {
  LOW: 1,
  MEDIUM: 2,
  HIGH: 3,
  CRITICAL: 4,
}

export function compareReviewIssues(
  previous: ReviewIssue[],
  current: ReviewIssue[],
): ReviewIssueComparison[] {
  const currentByKey = new Map(current.map((issue) => [issue.key, issue]))
  const previousByKey = new Map(previous.map((issue) => [issue.key, issue]))
  const result: ReviewIssueComparison[] = []
  for (const issue of current) {
    const before = previousByKey.get(issue.key)
    if (!before) {
      result.push({ key: issue.key, status: "NEW", current: issue })
      continue
    }
    const improved =
      (before.blocking && !issue.blocking) ||
      severityRank[issue.severity] < severityRank[before.severity]
    result.push({
      key: issue.key,
      status: improved ? "PARTIALLY_RESOLVED" : "STILL_PRESENT",
      current: issue,
      previous: before,
    })
  }
  for (const issue of previous) {
    if (!currentByKey.has(issue.key)) {
      result.push({ key: issue.key, status: "RESOLVED", previous: issue })
    }
  }
  return result
}
