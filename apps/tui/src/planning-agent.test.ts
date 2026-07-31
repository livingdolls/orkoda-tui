import { describe, expect, test } from "bun:test"

import {
  answerPlanningRun,
  getCurrentPlanningRun,
  type PlanningAgentFetch,
  startPlanningRun,
} from "./planning-agent"

const runPayload = {
  id: "run-1",
  plan_id: "plan-1",
  plan_version_id: "version-1",
  planning_context_id: "context-1",
  provider: "local-fake",
  model: "local-fake-planner-v1",
  status: "NEEDS_INPUT",
  questions: [
    {
      id: "question-1",
      run_id: "run-1",
      position: 0,
      question: "Which directory should store Markdown files?",
      status: "OPEN",
      created_at: "2026-07-31T00:00:00Z",
    },
  ],
  usage: {
    input_tokens: 100,
    output_tokens: 25,
    total_tokens: 125,
  },
  created_at: "2026-07-31T00:00:00Z",
  updated_at: "2026-07-31T00:00:00Z",
}

describe("planning agent API client", () => {
  test("starts and reads the current planning run", async () => {
    const paths: string[] = []
    const methods: string[] = []
    const fetcher: PlanningAgentFetch = async (input, init) => {
      paths.push(String(input))
      methods.push(String(init?.method))
      return new Response(JSON.stringify({ data: runPayload }), { status: 200 })
    }

    const started = await startPlanningRun("plan-1", fetcher)
    const current = await getCurrentPlanningRun("plan-1", fetcher)

    expect(started.status).toBe("NEEDS_INPUT")
    expect(current.questions).toHaveLength(1)
    expect(paths[0]).toEndWith("/api/v1/plans/plan-1/planning-runs")
    expect(paths[1]).toEndWith("/api/v1/plans/plan-1/planning-runs/current")
    expect(methods).toEqual(["POST", "GET"])
  })

  test("submits normalized answers", async () => {
    let requestBody = ""
    const fetcher: PlanningAgentFetch = async (_input, init) => {
      requestBody = String(init?.body)
      return new Response(
        JSON.stringify({
          data: { ...runPayload, id: "run-2", status: "COMPLETED", questions: [] },
        }),
        { status: 201 },
      )
    }

    const run = await answerPlanningRun(
      "run-1",
      [{ questionID: "question-1", answer: "content/blog" }],
      fetcher,
    )
    expect(run.status).toBe("COMPLETED")
    expect(JSON.parse(requestBody)).toEqual({
      answers: [{ question_id: "question-1", answer: "content/blog" }],
    })
  })

  test("surfaces daemon errors", async () => {
    const fetcher: PlanningAgentFetch = async () =>
      new Response(JSON.stringify({ error: { message: "normalize the current plan first" } }), {
        status: 409,
      })

    await expect(startPlanningRun("plan-1", fetcher)).rejects.toThrow(
      "normalize the current plan first",
    )
  })
})
