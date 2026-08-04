from pathlib import Path


def replace_once(source: str, old: str, new: str, label: str) -> str:
    count = source.count(old)
    if count != 1:
        raise SystemExit(f"expected one {label}, found {count}")
    return source.replace(old, new, 1)


board_path = Path("apps/tui/src/board-screen.tsx")
board = board_path.read_text()

board = replace_once(
    board,
    'import { loadBoardItems } from "./board-data"\n',
    'import { loadBoardItems } from "./board-data"\nimport { boardKeyRoute } from "./board-keyboard"\n',
    "Board keyboard import",
)

board = replace_once(
    board,
    '''    if (!lastEvent || mode !== "board") return
    const timeout = setTimeout(() => void reload(), 120)
    return () => clearTimeout(timeout)
  }, [lastEvent, mode, reload])''',
    '''    if (!lastEvent || mode !== "board" || actionMenuOpen) return
    const timeout = setTimeout(() => void reload(), 120)
    return () => clearTimeout(timeout)
  }, [lastEvent, mode, actionMenuOpen, reload])''',
    "SSE reload block",
)

board = replace_once(
    board,
    '''  useKeyboard((key) => {
    if (mode !== "board" || busy || loadState === "loading") return

    if (actionMenuOpen) {''',
    '''  useKeyboard((key) => {
    const keyRoute = boardKeyRoute({ mode, actionMenuOpen, busy })
    if (keyRoute === "ignore") return

    if (keyRoute === "menu") {''',
    "keyboard routing guard",
)
board_path.write_text(board)

Path("apps/tui/src/board-keyboard.ts").write_text(
    '''export type BoardKeyboardMode =
  | "board"
  | "new-project"
  | "new-plan"
  | "questions"
  | "detail"

export type BoardKeyRoute = "ignore" | "menu" | "board"

export function boardKeyRoute({
  mode,
  actionMenuOpen,
  busy,
}: {
  mode: BoardKeyboardMode
  actionMenuOpen: boolean
  busy: boolean
}): BoardKeyRoute {
  if (mode !== "board") return "ignore"
  if (actionMenuOpen) return "menu"
  if (busy) return "ignore"
  return "board"
}
'''
)

Path("apps/tui/src/board-keyboard.test.ts").write_text(
    '''import { describe, expect, test } from "bun:test"
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
'''
)
