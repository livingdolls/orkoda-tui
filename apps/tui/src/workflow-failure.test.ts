import { describe, expect, test } from "bun:test"

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
