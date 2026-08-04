/** @jsxImportSource @opentui/react */

import type { ReactNode } from "react"

export const colors = {
  canvas: "#080B10",
  surface: "#0E141C",
  surfaceRaised: "#141D28",
  surfaceAccent: "#102A38",
  line: "#263342",
  lineStrong: "#3A4B5E",
  text: "#E8EEF4",
  muted: "#9AA8B7",
  dim: "#647384",
  accent: "#67D8FF",
  success: "#6EE7A0",
  warning: "#F3C969",
  danger: "#FF7F88",
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

export function StatusBadge({ label, tone = "neutral" }: { label: string; tone?: StatusTone }) {
  return (
    <box
      paddingLeft={1}
      paddingRight={1}
      backgroundColor={tone === "accent" ? colors.surfaceAccent : colors.surfaceRaised}
      borderStyle="rounded"
      borderColor={toneColor(tone)}
    >
      <text fg={toneColor(tone)}>{label}</text>
    </box>
  )
}

export function PageIntro({
  kicker,
  title,
  description,
  meta,
}: {
  kicker: string
  title: string
  description: string
  meta?: string
}) {
  return (
    <box flexDirection="column" gap={1}>
      <box flexDirection="row" justifyContent="space-between" alignItems="center">
        <text fg={colors.accent}>{kicker}</text>
        {meta ? <text fg={colors.dim}>{meta}</text> : null}
      </box>
      <text fg={colors.text}>{title}</text>
      <text fg={colors.muted}>{description}</text>
    </box>
  )
}

export function SectionHeading({
  title,
  detail,
  action,
}: {
  title: string
  detail?: string
  action?: string
}) {
  return (
    <box flexDirection="row" justifyContent="space-between" alignItems="center">
      <box flexDirection="row" gap={1}>
        <text fg={colors.text}>{title}</text>
        {detail ? <text fg={colors.dim}>{detail}</text> : null}
      </box>
      {action ? <text fg={colors.accent}>{action}</text> : null}
    </box>
  )
}

export function ShortcutBar({
  shortcuts,
  subdued = false,
}: {
  shortcuts: Shortcut[]
  subdued?: boolean
}) {
  return (
    <box
      flexDirection="row"
      flexWrap="wrap"
      gap={1}
      paddingTop={1}
      borderStyle="single"
      borderColor={colors.line}
    >
      {shortcuts.map((shortcut) => (
        <box key={`${shortcut.key}-${shortcut.label}`} flexDirection="row" gap={1}>
          <text fg={subdued ? colors.dim : colors.accent}>{shortcut.key}</text>
          <text fg={subdued ? colors.dim : colors.muted}>{shortcut.label}</text>
        </box>
      ))}
    </box>
  )
}

export function Panel({
  children,
  title,
  borderColor = colors.line,
  backgroundColor = colors.surface,
  flexGrow,
  width,
}: {
  children: ReactNode
  title?: string
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
      title={title}
    >
      {children}
    </box>
  )
}

export function EmptyState({
  title,
  detail,
  action,
}: {
  title: string
  detail: string
  action: string
}) {
  return (
    <Panel borderColor={colors.lineStrong} backgroundColor={colors.surfaceRaised}>
      <text fg={colors.accent}>◇</text>
      <text fg={colors.text}>{title}</text>
      <text fg={colors.muted}>{detail}</text>
      <text fg={colors.accent}>{action}</text>
    </Panel>
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
    <box
      flexDirection="column"
      gap={1}
      flexGrow={1}
      padding={1}
      backgroundColor={colors.surfaceRaised}
    >
      <text fg={colors.dim}>{label}</text>
      <text fg={toneColor(tone)}>{value}</text>
    </box>
  )
}
