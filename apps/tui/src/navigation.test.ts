import { describe, expect, test } from "bun:test"
import { isScreen, moveScreen, screenFromShortcut, screenLabel } from "./navigation"

describe("navigation", () => {
  test("accepts product areas", () => {
    expect(isScreen("board")).toBe(true)
    expect(isScreen("review")).toBe(true)
    expect(isScreen("system")).toBe(true)
  })
  test("rejects removed screens", () => {
    expect(isScreen("projects")).toBe(false)
    expect(isScreen("jobs")).toBe(false)
  })
  test("moves and wraps", () => {
    expect(moveScreen("board", 1)).toBe("review")
    expect(moveScreen("system", 1)).toBe("board")
    expect(moveScreen("review", -1)).toBe("board")
  })
  test("maps numeric shortcuts", () => {
    expect(screenFromShortcut("1")).toBe("board")
    expect(screenFromShortcut("2")).toBe("review")
    expect(screenFromShortcut("5")).toBe("system")
    expect(screenFromShortcut("6")).toBeUndefined()
  })
  test("returns labels", () => {
    expect(screenLabel("review")).toBe("Review")
    expect(screenLabel("settings")).toBe("Settings")
  })
})
