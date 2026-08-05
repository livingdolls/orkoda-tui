import { describe, expect, test } from "bun:test"

import { compareReviewIssues, resolveReviewColumn } from "./review-board-model"
import type { ReviewIssue, ReviewRun } from "./reviews"
import type { WorkflowJob, WorkflowStatus } from "./workflow-jobs"

function workflow(status: WorkflowStatus, executionVersion = 1, revisionCount = 0): WorkflowJob {
  return {
    id: "job-1",
    project_id: "project-1",
    plan_id: "plan-1",
    plan_version_id: "version-1",
    repository_id: "repo-1",
    base_branch: "main",
    base_commit_sha: "abc123",
    status,
    version: 3,
    execution_version: executionVersion,
    revision_count: revisionCount,
    limits: {
      max_revisions: 3,
      max_stage_attempts: 3,
      max_tool_calls: 50,
      wall_clock_seconds: 3600,
    },
    cancellation_requested: false,
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-05T00:00:00Z",
  }
}

function review(verdict: ReviewRun["verdict"], blocking = 0): ReviewRun {
  return {
    id: "review-1",
    workflow_job_id: "job-1",
    execution_id: "execution-1",
    execution_version: 1,
    check_run_id: "check-1",
    checkpoint_id: "checkpoint-1",
    agent_settings_version: 2,
    provider: "openai",
    model: "review-model",
    status: "COMPLETED",
    verdict,
    summary: "reviewed",
    total_issues: blocking,
    blocking_issues: blocking,
    created_at: "2026-08-05T00:00:00Z",
    updated_at: "2026-08-05T00:00:00Z",
  }
}

function issue(key: string, severity: ReviewIssue["severity"], blocking: boolean): ReviewIssue {
  return {
    id: key,
    review_run_id: "review-1",
    position: 0,
    key,
    severity,
    category: "CORRECTNESS",
    blocking,
    title: key,
    description: key,
    criteria_refs: [],
    created_at: "2026-08-05T00:00:00Z",
  }
}

describe("review board projection", () => {
  test("maps review and revision lifecycle", () => {
    expect(resolveReviewColumn(workflow("REVIEWING"))).toBe("AWAITING_REVIEW")
    expect(
      resolveReviewColumn(workflow("REVIEWING", 2), { ...review(undefined), status: "RUNNING" }),
    ).toBe("AI_REVIEWING")
    expect(
      resolveReviewColumn(workflow("WAITING_FOR_APPROVAL"), review("REQUEST_REVISION", 1)),
    ).toBe("ISSUES_FOUND")
    expect(resolveReviewColumn(workflow("EXECUTING", 2, 1))).toBe("REVISION_IN_PROGRESS")
    expect(resolveReviewColumn(workflow("REVIEWING", 2, 1))).toBe("RE_REVIEW")
    expect(resolveReviewColumn(workflow("WAITING_FOR_APPROVAL"), review("APPROVE"))).toBe(
      "READY_FOR_APPROVAL",
    )
    expect(resolveReviewColumn(workflow("COMPLETED"), review("APPROVE"))).toBe("APPROVED")
  })

  test("compares findings across review cycles", () => {
    const comparison = compareReviewIssues(
      [
        issue("fixed", "HIGH", true),
        issue("improved", "HIGH", true),
        issue("same", "MEDIUM", false),
      ],
      [issue("improved", "LOW", false), issue("same", "MEDIUM", false), issue("new", "HIGH", true)],
    )
    expect(Object.fromEntries(comparison.map((item) => [item.key, item.status]))).toEqual({
      improved: "PARTIALLY_RESOLVED",
      same: "STILL_PRESENT",
      new: "NEW",
      fixed: "RESOLVED",
    })
  })
})
