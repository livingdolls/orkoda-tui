import { expect, test } from "bun:test"

import { formatAgentLiveEvent, isAgentLiveEvent } from "./agent-live"
import type { ActivityEvent } from "./events"

function event(type: string, payload: Record<string, unknown> = {}): ActivityEvent {
  return { sequence: 1, job_id: "workflow-1", type, payload, created_at: "2026-08-05T09:00:00Z" }
}

test("formats a waiting model turn without exposing raw prompts", () => {
  const line = formatAgentLiveEvent(
    event("executor.turn.started", { turn: 3, status: "waiting for model response" }),
  )
  expect(line.title).toBe("Turn 3 · waiting for model")
  expect(line.detail).toBe("waiting for model response")
  expect(line.tone).toBe("warning")
})

test("formats structured tool output", () => {
  const line = formatAgentLiveEvent(
    event("executor.turn.received", {
      turn: 4,
      tool: "file_patch",
      path: "internal/api.go",
      summary: "Patch the route",
      total_tokens: 120,
    }),
  )
  expect(line.title).toContain("file_patch")
  expect(line.detail).toContain("internal/api.go")
  expect(line.detail).toContain("120 tokens")
})

test("filters non-agent activity", () => {
  expect(isAgentLiveEvent(event("executor.tool.completed"))).toBe(true)
  expect(isAgentLiveEvent(event("workflow.transitioned"))).toBe(false)
})
