import { describe, expect, test } from "bun:test"

import { type CheckFetch, listChecks, listCheckSteps } from "./checks"

describe("checks API client", () => {
  test("loads check runs and steps", async () => {
    const urls: string[] = []
    const fetcher: CheckFetch = async (input) => {
      const url = String(input)
      urls.push(url)
      if (url.endsWith("/steps")) {
        return new Response(
          JSON.stringify({
            data: [
              {
                id: "step-1",
                profile: "go.test",
                status: "PASSED",
                command: ["go", "test", "./..."],
              },
            ],
          }),
          { status: 200 },
        )
      }
      return new Response(
        JSON.stringify({
          data: [
            {
              id: "check-1",
              workflow_job_id: "workflow-1",
              execution_version: 1,
              status: "PASSED",
            },
          ],
        }),
        { status: 200 },
      )
    }

    const checks = await listChecks("workflow-1", fetcher)
    const steps = await listCheckSteps("check-1", fetcher)

    expect(checks[0]?.id).toBe("check-1")
    expect(steps[0]?.profile).toBe("go.test")
    expect(urls[0]).toEndWith("/api/v1/jobs/workflow-1/checks")
    expect(urls[1]).toEndWith("/api/v1/checks/check-1/steps")
  })

  test("surfaces daemon errors", async () => {
    const fetcher: CheckFetch = async () =>
      new Response(JSON.stringify({ error: { message: "checks unavailable" } }), { status: 503 })

    await expect(listChecks("workflow-1", fetcher)).rejects.toThrow("checks unavailable")
  })
})
