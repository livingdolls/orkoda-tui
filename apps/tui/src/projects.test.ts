import { describe, expect, test } from "bun:test"

import {
  createProject,
  deleteProject,
  listProjects,
  refreshProject,
  type ProjectFetch,
} from "./projects"

const projectPayload = {
  id: "project-1",
  name: "Example",
  repositories: [
    {
      id: "repository-1",
      project_id: "project-1",
      local_path: "/tmp/example",
      current_branch: "main",
      head_sha: "abc123",
      dirty: false,
      created_at: "2026-07-31T00:00:00Z",
      updated_at: "2026-07-31T00:00:00Z",
    },
  ],
  created_at: "2026-07-31T00:00:00Z",
  updated_at: "2026-07-31T00:00:00Z",
}

describe("project API client", () => {
  test("lists projects", async () => {
    const fetcher: ProjectFetch = async () =>
      new Response(JSON.stringify({ data: [projectPayload] }), { status: 200 })

    const projects = await listProjects(fetcher)
    expect(projects).toHaveLength(1)
    expect(projects[0]?.repositories[0]?.head_sha).toBe("abc123")
  })

  test("creates a project with repository path", async () => {
    let requestBody = ""
    const fetcher: ProjectFetch = async (_input, init) => {
      requestBody = String(init?.body)
      return new Response(JSON.stringify({ data: projectPayload }), { status: 201 })
    }

    const project = await createProject("Example", "/tmp/example", fetcher)
    expect(project.id).toBe("project-1")
    expect(JSON.parse(requestBody)).toEqual({
      name: "Example",
      repository_path: "/tmp/example",
    })
  })

  test("surfaces API errors", async () => {
    const fetcher: ProjectFetch = async () =>
      new Response(JSON.stringify({ error: { message: "repository is already registered" } }), {
        status: 409,
      })

    await expect(createProject("Example", "/tmp/example", fetcher)).rejects.toThrow(
      "repository is already registered",
    )
  })

  test("deletes and refreshes a project", async () => {
    const requests: string[] = []
    const fetcher: ProjectFetch = async (input, init) => {
      requests.push(`${init?.method} ${String(input)}`)
      if (init?.method === "DELETE") {
        return new Response(null, { status: 204 })
      }
      return new Response(JSON.stringify({ data: projectPayload }), { status: 200 })
    }

    await deleteProject("project-1", fetcher)
    const refreshed = await refreshProject("project-1", fetcher)

    expect(refreshed.name).toBe("Example")
    expect(requests[0]).toContain("DELETE")
    expect(requests[1]).toContain("POST")
  })
})
