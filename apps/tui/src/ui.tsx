/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { type ReactNode, useMemo, useState } from "react"

export const BOLD = 1
export const UNDERLINE = 8

export const colors = {
  canvas: "#0F1116",
  surface: "#14171E",
  raised: "#1B1F29",
  inset: "#0A0C10",
  line: "#272C38",
  lineStrong: "#3C4354",
  text: "#EEF1F6",
  muted: "#9AA2B4",
  faint: "#6A7284",
  accent: "#82AAFF",
  accentTint: "#1C2438",
  success: "#5FDDA4",
  successTint: "#15291F",
  warning: "#F2BE54",
  warningTint: "#2B2312",
  danger: "#F97E8E",
  dangerTint: "#2D181E",
} as const

export type StatusTone = "neutral" | "accent" | "success" | "warning" | "danger"

export type Shortcut = {
  key: string
  label: string
}

export function toneColor(tone: StatusTone): string {
  switch (tone) {
    case "accent":
      return colors.accent
    case "success":
      return colors.success
    case "warning":
      return colors.warning
    case "danger":
      return colors.danger
    default:
      return colors.muted
  }
}

export function toneBg(tone: StatusTone): string {
  switch (tone) {
    case "accent":
      return colors.accentTint
    case "success":
      return colors.successTint
    case "warning":
      return colors.warningTint
    case "danger":
      return colors.dangerTint
    default:
      return colors.raised
  }
}

export function truncate(value: string, max: number): string {
  if (value.length <= max) return value
  return `${value.slice(0, Math.max(max - 1, 1))}…`
}

export function Chip({
  label,
  tone = "neutral",
  dot = true,
}: {
  label: string
  tone?: StatusTone
  dot?: boolean
}) {
  return (
    <box paddingLeft={1} paddingRight={1} backgroundColor={toneBg(tone)}>
      <text fg={toneColor(tone)}>{dot ? `${tone === "neutral" ? "○" : "●"} ${label}` : label}</text>
    </box>
  )
}

export function Key({ children }: { children: string }) {
  return (
    <box paddingLeft={1} paddingRight={1} backgroundColor={colors.raised}>
      <text fg={colors.text} attributes={BOLD}>
        {children}
      </text>
    </box>
  )
}

export function KeyHints({ shortcuts }: { shortcuts: Shortcut[] }) {
  return (
    <box flexDirection="row" flexWrap="wrap" columnGap={2} rowGap={0} alignItems="center">
      {shortcuts.map((shortcut) => (
        <box key={`${shortcut.key}-${shortcut.label}`} flexDirection="row" gap={1}>
          <Key>{shortcut.key}</Key>
          <text fg={colors.muted}>{shortcut.label}</text>
        </box>
      ))}
    </box>
  )
}

export function PageHeader({
  title,
  description,
  meta,
}: {
  title: string
  description?: string
  meta?: string
}) {
  return (
    <box flexDirection="column" gap={1}>
      <box flexDirection="row" justifyContent="space-between" alignItems="center">
        <text fg={colors.text} attributes={BOLD}>
          {title}
        </text>
        {meta ? <text fg={colors.faint}>{meta}</text> : null}
      </box>
      {description ? (
        <text fg={colors.muted} wrapMode="word">
          {description}
        </text>
      ) : null}
    </box>
  )
}

export function Section({
  title,
  action,
  children,
}: {
  title: string
  action?: string
  children?: ReactNode
}) {
  return (
    <box flexDirection="column" gap={1}>
      <box flexDirection="row" justifyContent="space-between" alignItems="center">
        <text fg={colors.text} attributes={BOLD}>
          {title}
        </text>
        {action ? <text fg={colors.faint}>{action}</text> : null}
      </box>
      {children}
    </box>
  )
}

export function Card({
  children,
  tone = "neutral",
  flexGrow,
  width,
}: {
  children: ReactNode
  tone?: StatusTone
  flexGrow?: number
  width?: number | "auto" | `${number}%`
}) {
  return (
    <box
      flexDirection="column"
      gap={1}
      padding={1}
      backgroundColor={tone === "neutral" ? colors.raised : toneBg(tone)}
      flexGrow={flexGrow}
      width={width}
    >
      {children}
    </box>
  )
}

export function Panel({
  children,
  borderColor = colors.line,
  backgroundColor = colors.surface,
  flexGrow,
  width,
}: {
  children: ReactNode
  borderColor?: string
  backgroundColor?: string
  flexGrow?: number
  width?: number | "auto" | `${number}%`
}) {
  return (
    <box
      flexDirection="column"
      gap={1}
      padding={1}
      borderStyle="rounded"
      borderColor={borderColor}
      backgroundColor={backgroundColor}
      flexGrow={flexGrow}
      width={width}
    >
      {children}
    </box>
  )
}

export function Banner({ tone = "neutral", children }: { tone?: StatusTone; children: ReactNode }) {
  return (
    <box
      flexDirection="column"
      gap={1}
      padding={1}
      backgroundColor={toneBg(tone)}
      borderStyle="rounded"
      borderColor={toneColor(tone)}
    >
      {children}
    </box>
  )
}

export function EmptyState({
  icon = "◇",
  title,
  detail,
  shortcut,
}: {
  icon?: string
  title: string
  detail: string
  shortcut?: Shortcut
}) {
  return (
    <box
      flexDirection="column"
      gap={1}
      padding={2}
      alignItems="center"
      justifyContent="center"
      flexGrow={1}
      backgroundColor={colors.surface}
      borderStyle="rounded"
      borderColor={colors.line}
    >
      <text fg={colors.accent}>{icon}</text>
      <text fg={colors.text} attributes={BOLD}>
        {title}
      </text>
      <text fg={colors.muted} wrapMode="word">
        {detail}
      </text>
      {shortcut ? (
        <box flexDirection="row" gap={1} alignItems="center">
          <Key>{shortcut.key}</Key>
          <text fg={colors.muted}>{shortcut.label}</text>
        </box>
      ) : null}
    </box>
  )
}

export function Metric({
  label,
  value,
  tone = "neutral",
}: {
  label: string
  value: string
  tone?: StatusTone
}) {
  return (
    <box flexDirection="column" gap={0} flexGrow={1} padding={1} backgroundColor={colors.raised}>
      <text fg={colors.faint}>{label}</text>
      <text fg={tone === "neutral" ? colors.text : toneColor(tone)} attributes={BOLD}>
        {value}
      </text>
    </box>
  )
}

export function KeyValue({
  label,
  value,
  tone = "neutral",
}: {
  label: string
  value: string
  tone?: StatusTone
}) {
  return (
    <box flexDirection="row" gap={1}>
      <text fg={colors.faint}>{label}</text>
      <text fg={tone === "neutral" ? colors.text : toneColor(tone)}>{value}</text>
    </box>
  )
}

export function Info({
  label,
  value,
  tone = "neutral",
}: {
  label: string
  value: string
  tone?: StatusTone
}) {
  return (
    <box flexDirection="row" gap={1}>
      <box width={18}>
        <text fg={colors.faint}>{label}</text>
      </box>
      <text
        fg={tone === "neutral" ? colors.text : toneColor(tone)}
        attributes={BOLD}
        wrapMode="word"
      >
        {value}
      </text>
    </box>
  )
}

export type PaletteCommand = {
  id: string
  label: string
  detail: string
  shortcut?: string
}

export function CommandPalette({
  commands,
  onSelect,
  onClose,
}: {
  commands: PaletteCommand[]
  onSelect: (command: PaletteCommand) => void
  onClose: () => void
}) {
  const [query, setQuery] = useState("")
  const [selectedIndex, setSelectedIndex] = useState(0)
  const filtered = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    if (!normalized) return commands
    return commands.filter((command) =>
      `${command.label} ${command.detail} ${command.shortcut ?? ""}`
        .toLowerCase()
        .includes(normalized),
    )
  }, [commands, query])

  useKeyboard((key) => {
    if (key.name === "escape") {
      onClose()
      return
    }
    if (key.name === "down" || key.name === "tab") {
      setSelectedIndex((current) => Math.min(current + 1, Math.max(filtered.length - 1, 0)))
      return
    }
    if (key.name === "up") {
      setSelectedIndex((current) => Math.max(current - 1, 0))
      return
    }
    if (key.name === "return" || key.name === "enter") {
      const command = filtered[selectedIndex]
      if (command) onSelect(command)
    }
  })

  return (
    <box
      position="absolute"
      top={2}
      left="15%"
      width="70%"
      padding={1}
      gap={0}
      flexDirection="column"
      borderStyle="rounded"
      borderColor={colors.accent}
      backgroundColor={colors.surface}
      title="SEARCH"
      titleColor={colors.accent}
    >
      <input
        value={query}
        placeholder="Type to find a screen or action..."
        focused
        onInput={(value) => {
          setQuery(value)
          setSelectedIndex(0)
        }}
      />
      {filtered.length === 0 ? (
        <text fg={colors.faint}>Nothing matches. Try a different word.</text>
      ) : null}
      {filtered.slice(0, 10).map((command, index) => {
        const selected = index === selectedIndex
        return (
          <box
            key={command.id}
            flexDirection="column"
            paddingLeft={1}
            paddingRight={1}
            backgroundColor={selected ? colors.accentTint : colors.surface}
          >
            <box flexDirection="row" justifyContent="space-between">
              <box flexDirection="row" gap={1}>
                <text fg={selected ? colors.accent : colors.faint}>{selected ? "▸" : " "}</text>
                <text fg={selected ? colors.text : colors.muted} attributes={selected ? BOLD : 0}>
                  {command.label}
                </text>
              </box>
              {command.shortcut ? <text fg={colors.accent}>{command.shortcut}</text> : null}
            </box>
            <text fg={colors.faint}>{`  ${truncate(command.detail, 44)}`}</text>
          </box>
        )
      })}
      <text fg={colors.faint}>↑↓ select · Enter run · Esc close</text>
    </box>
  )
}

export function Toast({ message, tone = "neutral" }: { message: string; tone?: StatusTone }) {
  return (
    <box
      position="absolute"
      right={2}
      bottom={3}
      paddingLeft={1}
      paddingRight={1}
      borderStyle="rounded"
      borderColor={toneColor(tone)}
      backgroundColor={colors.surface}
    >
      <text fg={toneColor(tone)}>{message}</text>
    </box>
  )
}
