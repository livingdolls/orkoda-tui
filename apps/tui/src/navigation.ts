export const screenDefinitions = [
  { id: "board", label: "Board", description: "Work from plan to approval" },
  { id: "review", label: "Review", description: "Executor and Reviewer handoff" },
  { id: "agents", label: "Agents", description: "AI roles & limits" },
  { id: "settings", label: "Settings", description: "Providers & budgets" },
  { id: "system", label: "System", description: "Daemon health" },
] as const

export const screens = screenDefinitions.map((screen) => screen.id)
export type Screen = (typeof screenDefinitions)[number]["id"]
export function isScreen(value: string): value is Screen {
  return screens.includes(value as Screen)
}
export function moveScreen(current: Screen, offset: number): Screen {
  const currentIndex = screens.indexOf(current)
  const nextIndex = (currentIndex + offset + screens.length) % screens.length
  return screens[nextIndex] ?? screens[0]
}
export function screenFromShortcut(keyName: string): Screen | undefined {
  const index = Number.parseInt(keyName, 10) - 1
  if (!Number.isInteger(index) || index < 0 || index >= screens.length) return undefined
  return screens[index]
}
export function screenLabel(screen: Screen): string {
  return screenDefinitions.find((item) => item.id === screen)?.label ?? screen
}
