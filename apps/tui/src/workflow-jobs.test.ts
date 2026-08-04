import { describe, expect, test } from "bun:test"

import {
  createWorkflowJob,
  getWorkflowJob,
  getWorkflowWorkspace,
  listProjectWorkspaces,
  listWorkflowJobs,
  listWorkflowTransitions,
  performWorkflowAction,
  releaseWorkspace,
  takeOverWorkspace,
  type WorkflowFetch,
  type WorkflowJob,
  type Workspace,
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

function workspaceFixture(): Workspace {
  return {
    id: "workspace-1",
    workflow_job_id: "workflow-1",
    project_id: "project-1",
    repository_id: "repository-1",
    path: "/tmp/orkoda/workspaces/workflow-1",
    base_commit_sha: "abc123",
    head_sha: "abc123",
    status: "READY",
    dirty: false,
    created_at: "2026-08-02T00:00:00Z",
    updated_at: "2026-08-02T00:00:01Z",
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

  test("lists project workspaces and reads the workflow workspace", async () => {
    const urls: string[] = []
    const fetcher: WorkflowFetch = async (input) => {
      urls.push(String(input))
      const data = urls.length === 1 ? [workspaceFixture()] : workspaceFixture()
      return new Response(JSON.stringify({ data }), { status: 200 })
    }

    const workspaces = await listProjectWorkspaces("project-1", fetcher)
    const workspace = await getWorkflowWorkspace("workflow-1", fetcher)
    expect(workspaces[0]?.status).toBe("READY")
    expect(workspace.head_sha).toBe("abc123")
    expect(urls[0]).toEndWith("/api/v1/projects/project-1/workspaces")
    expect(urls[1]).toEndWith("/api/v1/jobs/workflow-1/workspace")
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

  test("takes over and releases a workspace lease", async () => {
    const urls: string[] = []
    const fetcher: WorkflowFetch = async (input, init) => {
      urls.push(String(input))
      if (urls.length === 1) {
        return new Response(
          JSON.stringify({
            data: {
              workspace: { ...workspaceFixture(), status: "WRITE_LOCKED" },
              session_token: "lease-token",
              expires_at: "2026-08-02T01:00:00Z",
            },
          }),
          { status: 200 },
        )
      }
      expect(init?.body).toBe(
        JSON.stringify({ session_token: "lease-token", head_sha: "abc123", dirty: true }),
      )
      return new Response(JSON.stringify({ data: { ...workspaceFixture(), dirty: true } }), {
        status: 200,
      })
    }

    const lease = await takeOverWorkspace("workflow-1", "tui-test", fetcher)
    const released = await releaseWorkspace(
      "workspace-1",
      lease.session_token,
      "abc123",
      true,
      fetcher,
    )
    expect(lease.workspace.status).toBe("WRITE_LOCKED")
    expect(released.dirty).toBe(true)
    expect(urls[0]).toEndWith("/api/v1/jobs/workflow-1/workspace/take-over")
    expect(urls[1]).toEndWith("/api/v1/workspaces/workspace-1/release")
  })
})
