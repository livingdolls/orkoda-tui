export const screenDefinitions = [
  { id: "projects", label: "Projects", description: "Repositories & plans" },
  { id: "agents", label: "Agents", description: "Roles & guardrails" },
  { id: "jobs", label: "Jobs", description: "Runs & approvals" },
  { id: "settings", label: "Settings", description: "Providers & policy" },
  { id: "diagnostics", label: "Diagnostics", description: "Daemon health" },
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
