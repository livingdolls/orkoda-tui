import { describe, expect, test } from "bun:test"

import { probeDaemon } from "./daemon"

describe("probeDaemon", () => {
  test("reports a healthy daemon as connected", async () => {
    const fetcher = async () =>
      new Response(JSON.stringify({ status: "ok", protocol_version: "v1" }), {
        status: 200,
        headers: { "content-type": "application/json" },
      })

    const result = await probeDaemon("http://127.0.0.1:8181", fetcher)

    expect(result.state).toBe("connected")
    expect(result.protocolVersion).toBe("v1")
  })

  test("reports non-success responses as disconnected", async () => {
    const fetcher = async () => new Response("unavailable", { status: 503 })

    const result = await probeDaemon("http://127.0.0.1:8181", fetcher)

    expect(result.state).toBe("disconnected")
    expect(result.message).toContain("503")
  })

  test("reports connection failures as disconnected", async () => {
    const fetcher = async () => {
      throw new Error("connection refused")
    }

    const result = await probeDaemon("http://127.0.0.1:8181", fetcher)

    expect(result.state).toBe("disconnected")
    expect(result.message).toBe("Daemon is not running")
  })
})
