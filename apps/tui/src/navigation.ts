export const screenDefinitions = [
  { id: "projects", label: "Projects", description: "Code & plans" },
  { id: "agents", label: "Agents", description: "AI roles & limits" },
  { id: "jobs", label: "Jobs", description: "Runs & approvals" },
  { id: "settings", label: "Settings", description: "Providers & budgets" },
  { id: "diagnostics", label: "System", description: "Daemon health" },
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
  if (!Number.isInteger(index) || index < 0 || index >= screens.length) {
    return undefined
  }

  return screens[index]
}

export function screenLabel(screen: Screen): string {
  return screenDefinitions.find((item) => item.id === screen)?.label ?? screen
}
