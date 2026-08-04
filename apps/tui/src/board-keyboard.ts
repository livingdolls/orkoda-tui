export type BoardKeyboardMode = "board" | "new-project" | "new-plan" | "questions" | "detail"

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
