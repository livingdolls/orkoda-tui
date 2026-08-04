import { describe, expect, test } from "bun:test"
import { boardKeyRoute } from "./board-keyboard"

describe("Board keyboard routing", () => {
  test("keeps modal controls active while the Board is otherwise busy", () => {
    expect(boardKeyRoute({ mode: "board", actionMenuOpen: true, busy: true })).toBe("menu")
  })

  test("routes ordinary Board input when no mutation is active", () => {
    expect(boardKeyRoute({ mode: "board", actionMenuOpen: false, busy: false })).toBe("board")
  })

  test("blocks ordinary Board input during a mutation", () => {
    expect(boardKeyRoute({ mode: "board", actionMenuOpen: false, busy: true })).toBe("ignore")
  })

  test("ignores Board shortcuts while another screen owns the keyboard", () => {
    expect(boardKeyRoute({ mode: "detail", actionMenuOpen: false, busy: false })).toBe("ignore")
  })
})
