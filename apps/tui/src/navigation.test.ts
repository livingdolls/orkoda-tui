import { describe, expect, test } from "bun:test"

import { isScreen, moveScreen, screenFromShortcut, screenLabel } from "./navigation"

describe("navigation", () => {
  test("accepts known screens", () => {
    expect(isScreen("projects")).toBe(true)
    expect(isScreen("agents")).toBe(true)
    expect(isScreen("diagnostics")).toBe(true)
  })

  test("rejects unknown screens", () => {
    expect(isScreen("deployments")).toBe(false)
  })

  test("moves forward and wraps", () => {
    expect(moveScreen("projects", 1)).toBe("agents")
    expect(moveScreen("diagnostics", 1)).toBe("projects")
  })

  test("moves backward and wraps", () => {
    expect(moveScreen("agents", -1)).toBe("projects")
    expect(moveScreen("projects", -1)).toBe("diagnostics")
  })

  test("maps numeric shortcuts", () => {
    expect(screenFromShortcut("1")).toBe("projects")
    expect(screenFromShortcut("2")).toBe("agents")
    expect(screenFromShortcut("5")).toBe("diagnostics")
    expect(screenFromShortcut("9")).toBeUndefined()
  })

  test("returns display labels", () => {
    expect(screenLabel("agents")).toBe("Agents")
    expect(screenLabel("settings")).toBe("Settings")
  })
})
