import { describe, expect, test } from "bun:test"

import { isScreen, moveScreen, screenFromShortcut, screenLabel } from "./navigation"

describe("navigation", () => {
  test("accepts the simplified product areas", () => {
    expect(isScreen("board")).toBe(true)
    expect(isScreen("agents")).toBe(true)
    expect(isScreen("system")).toBe(true)
  })

  test("rejects removed and unknown screens", () => {
    expect(isScreen("projects")).toBe(false)
    expect(isScreen("jobs")).toBe(false)
    expect(isScreen("deployments")).toBe(false)
  })

  test("moves forward and wraps", () => {
    expect(moveScreen("board", 1)).toBe("agents")
    expect(moveScreen("system", 1)).toBe("board")
  })

  test("moves backward and wraps", () => {
    expect(moveScreen("agents", -1)).toBe("board")
    expect(moveScreen("board", -1)).toBe("system")
  })

  test("maps numeric shortcuts", () => {
    expect(screenFromShortcut("1")).toBe("board")
    expect(screenFromShortcut("2")).toBe("agents")
    expect(screenFromShortcut("4")).toBe("system")
    expect(screenFromShortcut("5")).toBeUndefined()
  })

  test("returns display labels", () => {
    expect(screenLabel("board")).toBe("Board")
    expect(screenLabel("settings")).toBe("Settings")
  })
})
