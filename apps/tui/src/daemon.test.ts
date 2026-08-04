import { describe, expect, test } from "bun:test"

import { probeDaemon, requestWithDaemonAuth } from "./daemon"

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

describe("requestWithDaemonAuth", () => {
  test("adds the configured bearer token to production requests", async () => {
    const token = "test-token-012345678901234567890123456789"
    const previousToken = process.env.ORKODA_API_TOKEN
    const previousFetch = globalThis.fetch
    let authorization = ""

    process.env.ORKODA_API_TOKEN = token
    globalThis.fetch = (async (_input, init) => {
      authorization = new Headers(init?.headers).get("authorization") ?? ""
      return new Response(null, { status: 204 })
    }) as typeof fetch

    try {
      await requestWithDaemonAuth(fetch, "http://127.0.0.1:8181/api/v1/projects")
    } finally {
      globalThis.fetch = previousFetch
      if (previousToken === undefined) {
        delete process.env.ORKODA_API_TOKEN
      } else {
        process.env.ORKODA_API_TOKEN = previousToken
      }
    }

    expect(authorization).toBe(`Bearer ${token}`)
  })
})
