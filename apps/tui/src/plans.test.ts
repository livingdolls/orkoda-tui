import { describe, expect, test } from "bun:test"

import { createPlan, listPlans, type PlanFetch, splitPlanLines } from "./plans"

const planPayload = {
  id: "plan-1",
  project_id: "project-1",
  title: "Blog",
  status: "DRAFT",
  current_version: 1,
  versions: [
    {
      id: "version-1",
      plan_id: "plan-1",
      version: 1,
      requirement: "Build a blog",
      acceptance_criteria: ["Lists articles"],
      constraints: ["Use existing stack"],
      created_at: "2026-07-31T00:00:00Z",
    },
  ],
  created_at: "2026-07-31T00:00:00Z",
  updated_at: "2026-07-31T00:00:00Z",
}

describe("plan API client", () => {
  test("lists project plans", async () => {
    const fetcher: PlanFetch = async () =>
      new Response(JSON.stringify({ data: [planPayload] }), { status: 200 })

    const plans = await listPlans("project-1", fetcher)
    expect(plans).toHaveLength(1)
    expect(plans[0]?.versions[0]?.requirement).toBe("Build a blog")
  })

  test("creates a structured plan", async () => {
    let requestBody = ""
    const fetcher: PlanFetch = async (_input, init) => {
      requestBody = String(init?.body)
      return new Response(JSON.stringify({ data: planPayload }), { status: 201 })
    }

    await createPlan(
      "project-1",
      {
        title: "Blog",
        requirement: "Build a blog",
        acceptanceCriteria: ["Lists articles"],
        constraints: ["Use existing stack"],
      },
      fetcher,
    )

    expect(JSON.parse(requestBody)).toEqual({
      title: "Blog",
      requirement: "Build a blog",
      acceptance_criteria: ["Lists articles"],
      constraints: ["Use existing stack"],
    })
  })

  test("surfaces daemon errors", async () => {
    const fetcher: PlanFetch = async () =>
      new Response(JSON.stringify({ error: { message: "invalid plan: requirement is required" } }), {
        status: 400,
      })

    await expect(
      createPlan(
        "project-1",
        { title: "Blog", requirement: "", acceptanceCriteria: [], constraints: [] },
        fetcher,
      ),
    ).rejects.toThrow("invalid plan")
  })
})

describe("splitPlanLines", () => {
  test("normalizes list markers and empty lines", () => {
    expect(splitPlanLines("- First\n\n* Second\n Third ")).toEqual(["First", "Second", "Third"])
  })
})
