export const screens = ["projects", "jobs", "settings", "diagnostics"] as const

export type Screen = (typeof screens)[number]

export function isScreen(value: string): value is Screen {
  return screens.includes(value as Screen)
}
