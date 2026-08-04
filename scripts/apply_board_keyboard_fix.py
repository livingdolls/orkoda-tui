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
    if (mode !== "board") return

    // Modal input must remain responsive while an SSE refresh is in flight.
    // Escape must always provide a way out of the action menu.
    if (actionMenuOpen) {''',
    "keyboard guard",
)

board = replace_once(
    board,
    '''    if (key.shift && key.name === "n") {''',
    '''    if (busy) return

    if (key.shift && key.name === "n") {''',
    "normal input guard",
)
board_path.write_text(board)


e2e_path = Path("apps/tui/src/app.e2e.test.tsx")
e2e = e2e_path.read_text()

e2e = replace_once(
    e2e,
    '''        await act(async () => {
          await setup.mockInput.pressKey("enter")
        })
        const detailFrame = await waitForTUIFrame(''',
    '''        const emitBoardKey = (name: string, sequence: string) =>
          setup.renderer.keyInput.processParsedKey({
            name,
            ctrl: false,
            meta: false,
            shift: false,
            option: false,
            sequence,
            number: false,
            raw: sequence,
            eventType: "press" as const,
            source: "raw" as const,
          })

        await act(async () => {
          emitBoardKey("space", " ")
        })
        const actionMenuFrame = await waitForTUIFrame(
          setup,
          (frame) =>
            frame.includes("What do you want to do?") &&
            frame.includes("Kanban approval flow"),
        )
        expect(actionMenuFrame).toContain("run action")

        await act(async () => {
          emitBoardKey("escape", "\\x1b")
        })
        await waitForTUIFrame(
          setup,
          (frame) => !frame.includes("What do you want to do?"),
        )

        await act(async () => {
          emitBoardKey("space", " ")
        })
        await waitForTUIFrame(
          setup,
          (frame) => frame.includes("What do you want to do?"),
        )
        await act(async () => {
          emitBoardKey("return", "\\r")
        })
        const detailFrame = await waitForTUIFrame(''',
    "E2E detail opening block",
)
e2e_path.write_text(e2e)
