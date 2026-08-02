import { describe, expect, test } from "bun:test"

import {
  createWorkflowJob,
  getWorkflowJob,
  listWorkflowJobs,
  listWorkflowTransitions,
  performWorkflowAction,
  type WorkflowFetch,
  type WorkflowJob,
} from "./workflow-jobs"

function jobFixture(): WorkflowJob {
  return {
    id: "workflow-1",
    project_id: "project-1",
    plan_id: "plan-1",
    plan_version_id: "plan-version-1",
    repository_id: "repository-1",
    base_branch: "main",
    base_commit_sha: "abc123",
    status: "READY",
    version: 1,
    execution_version: 0,
    revision_count: 0,
    limits: {
      max_revisions: 3,
      max_stage_attempts: 3,
      max_tool_calls: 120,
      wall_clock_seconds: 3600,
    },
    cancellation_requested: false,
    created_at: "2026-08-02T00:00:00Z",
    updated_at: "2026-08-02T00:00:00Z",
  }
}

describe("workflow jobs API client", () => {
  test("lists project jobs and reads a job", async () => {
    const urls: string[] = []
    const fetcher: WorkflowFetch = async (input) => {
      urls.push(String(input))
      const data = urls.length === 1 ? [jobFixture()] : jobFixture()
      return new Response(JSON.stringify({ data }), { status: 200 })
    }

    const jobs = await listWorkflowJobs("project-1", fetcher)
    const job = await getWorkflowJob("workflow-1", fetcher)
    expect(jobs[0]?.id).toBe("workflow-1")
    expect(job.status).toBe("READY")
    expect(urls[0]).toEndWith("/api/v1/projects/project-1/jobs")
    expect(urls[1]).toEndWith("/api/v1/jobs/workflow-1")
  })

  test("creates a workflow with explicit limits", async () => {
    let payload: unknown
    const fetcher: WorkflowFetch = async (_input, init) => {
      payload = JSON.parse(String(init?.body))
      return new Response(JSON.stringify({ data: jobFixture() }), { status: 201 })
    }

    await createWorkflowJob(
      "project-1",
      {
        plan_id: "plan-1",
        repository_id: "repository-1",
        limits: { max_revisions: 4 },
      },
      fetcher,
    )
    expect(payload).toEqual({
      plan_id: "plan-1",
      repository_id: "repository-1",
      limits: { max_revisions: 4 },
    })
  })

  test("performs a versioned explicit action", async () => {
    let url = ""
    let payload: unknown
    const fetcher: WorkflowFetch = async (input, init) => {
      url = String(input)
      payload = JSON.parse(String(init?.body))
      return new Response(
        JSON.stringify({ data: { ...jobFixture(), status: "WORKSPACE_PREPARING", version: 2 } }),
        { status: 200 },
      )
    }

    const started = await performWorkflowAction(
      "workflow-1",
      "start",
      1,
      { requested_by: "local-user" },
      fetcher,
    )
    expect(started.status).toBe("WORKSPACE_PREPARING")
    expect(url).toEndWith("/api/v1/jobs/workflow-1/start")
    expect(payload).toEqual({
      expected_version: 1,
      details: { requested_by: "local-user" },
    })
  })

  test("loads immutable transition history", async () => {
    const fetcher: WorkflowFetch = async () =>
      new Response(
        JSON.stringify({
          data: [
            {
              sequence: 1,
              workflow_job_id: "workflow-1",
              action: "CREATE",
              to_status: "READY",
              workflow_version: 1,
              details: {},
              created_at: "2026-08-02T00:00:00Z",
            },
          ],
        }),
        { status: 200 },
      )

    const transitions = await listWorkflowTransitions("workflow-1", fetcher)
    expect(transitions[0]?.action).toBe("CREATE")
  })

  test("surfaces daemon version conflicts", async () => {
    const fetcher: WorkflowFetch = async () =>
      new Response(JSON.stringify({ error: { message: "workflow job version conflict" } }), {
        status: 409,
      })

    await expect(performWorkflowAction("workflow-1", "start", 1, {}, fetcher)).rejects.toThrow(
      "workflow job version conflict",
    )
  })
})
