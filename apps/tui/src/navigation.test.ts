import { describe, expect, test } from "bun:test"
import { isScreen, moveScreen, screenFromShortcut, screenLabel } from "./navigation"

describe("navigation", () => {
  test("accepts product areas", () => {
    expect(isScreen("board")).toBe(true)
    expect(isScreen("agents")).toBe(true)
    expect(isScreen("system")).toBe(true)
  })
  test("rejects removed screens", () => {
    expect(isScreen("review")).toBe(false)
    expect(isScreen("projects")).toBe(false)
    expect(isScreen("jobs")).toBe(false)
  })
  test("moves and wraps", () => {
    expect(moveScreen("board", 1)).toBe("agents")
    expect(moveScreen("system", 1)).toBe("board")
    expect(moveScreen("agents", -1)).toBe("board")
  })
  test("maps numeric shortcuts", () => {
    expect(screenFromShortcut("1")).toBe("board")
    expect(screenFromShortcut("2")).toBe("agents")
    expect(screenFromShortcut("4")).toBe("system")
    expect(screenFromShortcut("5")).toBeUndefined()
  })
  test("returns labels", () => {
    expect(screenLabel("board")).toBe("Board")
    expect(screenLabel("settings")).toBe("Settings")
  })
})
