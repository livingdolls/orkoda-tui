import { describe, expect, test } from "bun:test"

import { isScreen } from "./navigation"

describe("isScreen", () => {
  test("accepts known screens", () => {
    expect(isScreen("projects")).toBe(true)
    expect(isScreen("diagnostics")).toBe(true)
  })

  test("rejects unknown screens", () => {
    expect(isScreen("deployments")).toBe(false)
  })
})
