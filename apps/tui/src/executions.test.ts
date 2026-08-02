import { describe, expect, test } from "bun:test"

import { type ExecutionFetch, listCheckpoints, listExecutions, listToolRuns } from "./executions"

describe("execution API client", () => {
  test("loads executions, tool runs, and checkpoints", async () => {
    const urls: string[] = []
    const fetcher: ExecutionFetch = async (input) => {
      const url = String(input)
      urls.push(url)
      if (url.endsWith("/tool-runs")) {
        return new Response(JSON.stringify({ data: [{ id: "tool-1", sequence: 1 }] }), {
          status: 200,
        })
      }
      if (url.endsWith("/checkpoints")) {
        return new Response(
          JSON.stringify({ data: [{ id: "checkpoint-1", patch_checksum: "sha256:abc" }] }),
          { status: 200 },
        )
      }
      return new Response(JSON.stringify({ data: [{ id: "execution-1", status: "COMPLETED" }] }), {
        status: 200,
      })
    }

    const executions = await listExecutions("workflow-1", fetcher)
    const toolRuns = await listToolRuns("execution-1", fetcher)
    const checkpoints = await listCheckpoints("execution-1", fetcher)

    expect(executions[0]?.id).toBe("execution-1")
    expect(toolRuns[0]?.id).toBe("tool-1")
    expect(checkpoints[0]?.patch_checksum).toBe("sha256:abc")
    expect(urls[0]).toEndWith("/api/v1/jobs/workflow-1/executions")
    expect(urls[1]).toEndWith("/api/v1/executions/execution-1/tool-runs")
    expect(urls[2]).toEndWith("/api/v1/executions/execution-1/checkpoints")
  })

  test("surfaces daemon errors", async () => {
    const fetcher: ExecutionFetch = async () =>
      new Response(JSON.stringify({ error: { message: "execution unavailable" } }), { status: 503 })

    await expect(listExecutions("workflow-1", fetcher)).rejects.toThrow("execution unavailable")
  })
})
