import { describe, expect, test } from "bun:test"

import {
  generateRepositorySummary,
  getPlanningContext,
  normalizePlan,
  type PlanningFetch,
} from "./planning"

const summaryPayload = {
  id: "summary-1",
  repository_id: "repo-1",
  project_id: "project-1",
  head_sha: "abc123",
  dirty: false,
  summary: {
    root_path: "/tmp/example",
    head_sha: "abc123",
    languages: ["Go"],
    frameworks: ["Gin"],
    package_managers: ["Go Modules"],
    commands: { test: ["go test ./..."] },
    important_files: ["go.mod"],
    top_level_entries: ["cmd", "internal"],
    file_count: 20,
    skipped_files: 3,
    truncated: false,
  },
  created_at: "2026-07-31T00:00:00Z",
}

const contextPayload = {
  id: "context-1",
  plan_id: "plan-1",
  plan_version_id: "version-1",
  plan_version: 1,
  repository_summary_id: "summary-1",
  normalized_plan: {
    goal: "Add blog",
    summary: "Build a Markdown blog",
    scope: ["List articles"],
    non_goals: [],
    acceptance_criteria: ["List articles"],
    constraints: [],
    affected_areas: ["backend"],
    risks: [],
    open_questions: [],
    repository: {
      repository_id: "repo-1",
      summary_id: "summary-1",
      head_sha: "abc123",
      dirty: false,
      languages: ["Go"],
      frameworks: ["Gin"],
      package_managers: ["Go Modules"],
      commands: { test: ["go test ./..."] },
      important_files: ["go.mod"],
    },
  },
  created_at: "2026-07-31T00:00:00Z",
}

describe("planning API client", () => {
  test("generates a repository summary with the expected endpoint", async () => {
    let path = ""
    let method = ""
    const fetcher: PlanningFetch = async (input, init) => {
      path = String(input)
      method = String(init?.method)
      return new Response(JSON.stringify({ data: summaryPayload }), { status: 201 })
    }

    const summary = await generateRepositorySummary("repo-1", fetcher)
    expect(path).toEndWith("/api/v1/repositories/repo-1/summaries")
    expect(method).toBe("POST")
    expect(summary.summary.languages).toEqual(["Go"])
  })

  test("normalizes and reads a planning context", async () => {
    const methods: string[] = []
    const fetcher: PlanningFetch = async (_input, init) => {
      methods.push(String(init?.method))
      return new Response(JSON.stringify({ data: contextPayload }), { status: 200 })
    }

    const normalized = await normalizePlan("plan-1", fetcher)
    const current = await getPlanningContext("plan-1", fetcher)
    expect(normalized.normalized_plan.goal).toBe("Add blog")
    expect(current.repository_summary_id).toBe("summary-1")
    expect(methods).toEqual(["POST", "GET"])
  })

  test("surfaces the summary prerequisite message", async () => {
    const fetcher: PlanningFetch = async () =>
      new Response(
        JSON.stringify({
          error: { message: "scan the current repository HEAD before normalizing the plan" },
        }),
        { status: 409 },
      )

    await expect(normalizePlan("plan-1", fetcher)).rejects.toThrow("scan the current repository")
  })
})
