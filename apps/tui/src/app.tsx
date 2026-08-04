/** @jsxImportSource @opentui/react */

import { useKeyboard, useOnResize } from "@opentui/react"
import { useEffect, useRef, useState } from "react"

import { AgentSettingsScreen } from "./agent-settings-screen"
import {
  createDiagnosticsBundle,
  type DaemonConnection,
  type DiagnosticsSnapshot,
  getDiagnostics,
  initialDaemonConnection,
  probeDaemon,
} from "./daemon"
import { type ActivityEvent, subscribeToEvents } from "./events"
import { JobsScreen } from "./jobs-screen"
import {
  moveScreen,
  type Screen,
  screenDefinitions,
  screenFromShortcut,
  screenLabel,
} from "./navigation"
import { ProjectScreen } from "./project-screen"
import { SettingsScreen } from "./settings-screen"
import {
  BOLD,
  Card,
  Chip,
  CommandPalette,
  colors,
  Info,
  Key,
  KeyHints,
  PageHeader,
  type PaletteCommand,
  Section,
  type Shortcut,
  Toast,
} from "./ui"

const connectionTone: Record<DaemonConnection["state"], "warning" | "success" | "danger"> = {
  checking: "warning",
  connected: "success",
  disconnected: "danger",
}

const connectionLabel: Record<DaemonConnection["state"], string> = {
  checking: "connecting…",
  connected: "connected",
  disconnected: "offline",
}

export function App() {
  const [activeScreen, setActiveScreen] = useState<Screen>("projects")
  const [connection, setConnection] = useState<DaemonConnection>(initialDaemonConnection)
  const [terminalWidth, setTerminalWidth] = useState(0)
  const [projectInteractionActive, setProjectInteractionActive] = useState(false)
  const [jobsInteractionActive, setJobsInteractionActive] = useState(false)
  const [showHelp, setShowHelp] = useState(false)
  const [showPalette, setShowPalette] = useState(false)
  const [lastEvent, setLastEvent] = useState<ActivityEvent | null>(null)
  const [diagnostics, setDiagnostics] = useState<DiagnosticsSnapshot | null>(null)
  const [sidebarWidth, setSidebarWidth] = useState(26)
  const [eventStreamState, setEventStreamState] = useState<"connected" | "reconnecting" | "closed">(
    "closed",
  )
  const lastSequenceRef = useRef(0)
  const [toast, setToast] = useState("")
  const renderer = useOnResize((width) => setTerminalWidth(width))

  const compactLayout = (terminalWidth || renderer.width) < 100

  useKeyboard((key) => {
    if (showPalette) {
      return
    }
    if (showHelp) {
      if (key.name === "escape" || key.name === "?") {
        setShowHelp(false)
      }
      return
    }

    if ((key.ctrl && key.name === "k") || key.name === "/") {
      setShowPalette(true)
      return
    }

    if (
      key.ctrl &&
      !projectInteractionActive &&
      !jobsInteractionActive &&
      (key.name === "left" || key.name === "right")
    ) {
      setSidebarWidth((current) =>
        Math.min(36, Math.max(20, current + (key.name === "right" ? 2 : -2))),
      )
      setToast("Workspace panel resized")
      return
    }

    if (key.name === "?" && !projectInteractionActive && !jobsInteractionActive) {
      setShowHelp(true)
      return
    }

    if (activeScreen === "projects") {
      if (projectInteractionActive) {
        return
      }

      const shortcut = screenFromShortcut(key.name)
      if (shortcut) {
        setActiveScreen(shortcut)
        return
      }
      if (key.name === "right" || key.name === "l") {
        setActiveScreen((current) => moveScreen(current, 1))
        return
      }
      if (key.name === "left" || key.name === "h") {
        setActiveScreen((current) => moveScreen(current, -1))
      }
      return
    }

    if (activeScreen === "agents") {
      const shortcut = screenFromShortcut(key.name)
      if (shortcut) {
        setActiveScreen(shortcut)
        return
      }
      if (key.name === "right" || key.name === "l") {
        setActiveScreen((current) => moveScreen(current, 1))
        return
      }
      if (key.name === "left" || key.name === "h") {
        setActiveScreen((current) => moveScreen(current, -1))
      }
      return
    }

    if (activeScreen === "jobs") {
      if (jobsInteractionActive) {
        return
      }
      const shortcut = screenFromShortcut(key.name)
      if (shortcut) {
        setActiveScreen(shortcut)
        return
      }
      if (key.name === "right" || key.name === "l") {
        setActiveScreen((current) => moveScreen(current, 1))
        return
      }
      if (key.name === "left" || key.name === "h") {
        setActiveScreen((current) => moveScreen(current, -1))
      }
      return
    }

    const shortcut = screenFromShortcut(key.name)
    if (shortcut) {
      setActiveScreen(shortcut)
      return
    }
    if (key.name === "right" || key.name === "down" || key.name === "j" || key.name === "l") {
      setActiveScreen((current) => moveScreen(current, 1))
      return
    }
    if (key.name === "left" || key.name === "up" || key.name === "h" || key.name === "k") {
      setActiveScreen((current) => moveScreen(current, -1))
      return
    }
    if (key.name === "r") {
      setConnection(initialDaemonConnection)
      void probeDaemon().then(setConnection)
    }
  })

  useEffect(() => {
    let disposed = false

    const refresh = async () => {
      const nextConnection = await probeDaemon()
      if (!disposed) {
        setConnection(nextConnection)
      }
    }

    void refresh()
    const interval = setInterval(() => void refresh(), 2000)

    return () => {
      disposed = true
      clearInterval(interval)
    }
  }, [])

  useEffect(() => {
    if (connection.state !== "connected") {
      setEventStreamState("closed")
      return
    }
    const unsubscribe = subscribeToEvents({
      afterSequence: lastSequenceRef.current,
      onEvent: (event) => {
        lastSequenceRef.current = event.sequence
        setLastEvent(event)
      },
      onState: setEventStreamState,
    })
    return unsubscribe
    // Reconnect only when the daemon connection changes. The stream itself
    // advances its cursor as durable events arrive.
  }, [connection.state])

  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected") {
      setDiagnostics(null)
      return
    }
    const refresh = async () => {
      try {
        const snapshot = await getDiagnostics()
        if (!disposed) setDiagnostics(snapshot)
      } catch {
        if (!disposed) setDiagnostics(null)
      }
    }
    void refresh()
    const interval = setInterval(() => void refresh(), 5000)
    return () => {
      disposed = true
      clearInterval(interval)
    }
  }, [connection.state])

  useEffect(() => {
    if (!toast) return
    const timeout = setTimeout(() => setToast(""), 3500)
    return () => clearTimeout(timeout)
  }, [toast])

  const paletteCommands: PaletteCommand[] = [
    ...screenDefinitions.map((item, index) => ({
      id: `screen:${item.id}`,
      label: `Open ${item.label}`,
      detail: item.description,
      shortcut: String(index + 1),
    })),
    {
      id: "reconnect",
      label: "Reconnect daemon",
      detail: "Refresh health and protocol status",
      shortcut: "R",
    },
    {
      id: "diagnostics-bundle",
      label: "Export diagnostics bundle",
      detail: "Save a redacted SQLite and runtime health snapshot",
      shortcut: "B",
    },
    { id: "help", label: "Keyboard guide", detail: "Show contextual shortcuts", shortcut: "?" },
  ]

  const footerShortcuts: Shortcut[] = (() => {
    if (activeScreen === "projects") {
      return projectInteractionActive
        ? [
            { key: "Esc", label: "cancel" },
            { key: "?", label: "help" },
          ]
        : [
            { key: "↑↓", label: "select" },
            { key: "N", label: "new project" },
            { key: "P", label: "new plan" },
            { key: "W", label: "start work" },
            { key: "?", label: "all shortcuts" },
          ]
    }
    if (activeScreen === "agents") {
      return [
        { key: "↑↓", label: "project" },
        { key: "Tab", label: "role" },
        { key: "E", label: "enable / disable" },
        { key: "S", label: "save" },
        { key: "?", label: "all shortcuts" },
      ]
    }
    if (activeScreen === "jobs") {
      return jobsInteractionActive
        ? [
            { key: "Ctrl+S", label: "apply" },
            { key: "Esc", label: "cancel" },
          ]
        : [
            { key: "↑↓", label: "select" },
            { key: "A", label: "approve" },
            { key: "V", label: "revise" },
            { key: "X", label: "reject" },
            { key: "?", label: "all shortcuts" },
          ]
    }
    return [
      { key: "←→", label: "switch screen" },
      { key: "R", label: "reconnect" },
      { key: "?", label: "all shortcuts" },
    ]
  })()
  const visibleFooterShortcuts = compactLayout
    ? footerShortcuts.filter((shortcut) => shortcut.key !== "?")
    : footerShortcuts

  return (
    <box flexDirection="column" width="100%" height="100%" backgroundColor={colors.canvas}>
      <box
        height={3}
        paddingLeft={1}
        paddingRight={1}
        flexDirection="row"
        justifyContent="space-between"
        alignItems="center"
        backgroundColor={colors.surface}
        border={["bottom"]}
        borderColor={colors.line}
      >
        <box flexDirection="row" gap={1} alignItems="center">
          <text fg={colors.accent} attributes={BOLD}>
            ◆ ORKODA
          </text>
          <text fg={colors.faint}>·</text>
          <text fg={colors.text} attributes={BOLD}>
            {screenLabel(activeScreen)}
          </text>
        </box>
        <box flexDirection="row" gap={1} alignItems="center">
          <text fg={colors.faint}>{connection.protocolVersion}</text>
          <Chip label={connectionLabel[connection.state]} tone={connectionTone[connection.state]} />
        </box>
      </box>

      <box flexGrow={1} flexDirection="row" padding={1} gap={1}>
        {!compactLayout ? (
          <box
            width={sidebarWidth}
            flexDirection="column"
            gap={1}
            padding={1}
            backgroundColor={colors.surface}
            borderStyle="rounded"
            borderColor={colors.line}
          >
            {screenDefinitions.map((item, index) => {
              const selected = item.id === activeScreen
              return (
                <box
                  key={item.id}
                  flexDirection="column"
                  paddingLeft={1}
                  paddingRight={1}
                  backgroundColor={selected ? colors.accentTint : colors.surface}
                >
                  <box flexDirection="row" gap={1}>
                    <text fg={selected ? colors.accent : colors.faint}>{selected ? "▸" : " "}</text>
                    <text
                      fg={selected ? colors.text : colors.muted}
                      attributes={selected ? BOLD : 0}
                    >
                      {`${index + 1} ${item.label}`}
                    </text>
                  </box>
                  <text
                    fg={selected ? colors.muted : colors.faint}
                  >{`   ${item.description}`}</text>
                </box>
              )
            })}
            <box flexGrow={1} />
            <box flexDirection="row" gap={1} alignItems="center">
              <Key>?</Key>
              <text fg={colors.faint}>all shortcuts</text>
            </box>
            <box flexDirection="row" gap={1} alignItems="center">
              <Key>/</Key>
              <text fg={colors.faint}>search actions</text>
            </box>
          </box>
        ) : null}

        <box
          flexGrow={1}
          flexDirection="column"
          backgroundColor={colors.surface}
          borderStyle="rounded"
          borderColor={colors.line}
          padding={1}
          gap={1}
        >
          {showHelp ? (
            activeScreen === "projects" ? (
              <ProjectScreen
                connection={connection}
                onInteractionChange={setProjectInteractionActive}
                helpOpen
              />
            ) : (
              <HelpScreen screen={activeScreen} />
            )
          ) : activeScreen === "projects" ? (
            <ProjectScreen
              connection={connection}
              onInteractionChange={setProjectInteractionActive}
            />
          ) : (
            <ScreenContent
              screen={activeScreen}
              connection={connection}
              onJobsInteractionChange={setJobsInteractionActive}
              lastEvent={lastEvent}
              eventStreamState={eventStreamState}
              diagnostics={diagnostics}
            />
          )}
        </box>
      </box>
      {showHelp && activeScreen === "projects" ? (
        <box
          position="absolute"
          top={4}
          left={compactLayout ? 1 : sidebarWidth + 2}
          right={1}
          bottom={2}
          padding={2}
          borderStyle="rounded"
          borderColor={colors.accent}
          backgroundColor={colors.canvas}
        >
          <HelpScreen screen={activeScreen} />
        </box>
      ) : null}
      <box
        height={2}
        paddingLeft={1}
        paddingRight={1}
        flexDirection="row"
        alignItems="center"
        backgroundColor={colors.surface}
      >
        <KeyHints shortcuts={visibleFooterShortcuts} />
      </box>
      {showPalette ? (
        <CommandPalette
          commands={paletteCommands}
          onClose={() => setShowPalette(false)}
          onSelect={(command) => {
            if (command.id.startsWith("screen:")) {
              setActiveScreen(command.id.slice("screen:".length) as Screen)
              setToast(`Opened ${command.label.replace(/^Open /, "")}`)
            } else if (command.id === "reconnect") {
              setConnection(initialDaemonConnection)
              void probeDaemon().then(setConnection)
              setToast("Daemon reconnect requested")
            } else if (command.id === "help") {
              setShowHelp(true)
            } else if (command.id === "diagnostics-bundle") {
              void createDiagnosticsBundle()
                .then((key) => setToast(`Diagnostics bundle saved: ${key}`))
                .catch((error) =>
                  setToast(error instanceof Error ? error.message : "Diagnostics export failed"),
                )
            }
            setShowPalette(false)
          }}
        />
      ) : null}
      {toast ? <Toast message={toast} tone="success" /> : null}
    </box>
  )
}

function ScreenContent({
  screen,
  connection,
  onJobsInteractionChange,
  lastEvent,
  eventStreamState,
  diagnostics,
}: {
  screen: Screen
  connection: DaemonConnection
  onJobsInteractionChange: (active: boolean) => void
  lastEvent: ActivityEvent | null
  eventStreamState: "connected" | "reconnecting" | "closed"
  diagnostics: DiagnosticsSnapshot | null
}) {
  if (screen === "agents") {
    return <AgentSettingsScreen connection={connection} />
  }

  if (screen === "jobs") {
    return <JobsScreen connection={connection} onInteractionChange={onJobsInteractionChange} />
  }

  if (screen === "settings") {
    return <SettingsScreen connection={connection} />
  }

  return (
    <box flexDirection="column" gap={1} flexGrow={1}>
      <PageHeader
        title="System status"
        description="A quick health check of the local daemon that runs your workflows."
        meta="press R to refresh"
      />
      <Section title="Daemon">
        <Card>
          <box flexDirection="row" gap={1} alignItems="center">
            <Chip
              label={connection.state === "connected" ? "connected" : connection.state}
              tone={connectionTone[connection.state]}
            />
            <text fg={colors.muted}>{connection.message}</text>
          </box>
          <Info label="Address" value="127.0.0.1:8181" />
          <Info label="Protocol" value={connection.protocolVersion} tone="accent" />
        </Card>
      </Section>
      <Section title="Background work">
        <Card>
          {diagnostics ? (
            <box flexDirection="column" gap={1}>
              <box flexDirection="row" gap={1} alignItems="center">
                <Chip
                  label={diagnostics.status === "ready" ? "ready" : "degraded"}
                  tone={diagnostics.status === "ready" ? "success" : "warning"}
                />
                <text
                  fg={colors.faint}
                >{`database schema v${diagnostics.database.schema_version} · SQLite ${diagnostics.database.integrity}`}</text>
              </box>
              <Info label="Waiting" value={`${diagnostics.queue.queued}`} />
              <Info label="Running" value={`${diagnostics.queue.running}`} tone="accent" />
              <Info
                label="Failed"
                value={`${diagnostics.queue.dead}`}
                tone={diagnostics.queue.dead > 0 ? "danger" : "success"}
              />
              <Info
                label="Workspace leases"
                value={`${diagnostics.workspaces.active_leases}/${diagnostics.workspaces.total}`}
              />
            </box>
          ) : (
            <text fg={colors.faint}>Diagnostics are temporarily unavailable.</text>
          )}
        </Card>
      </Section>
      <Section title="Live updates">
        <Card>
          <box flexDirection="row" gap={1} alignItems="center">
            <Chip
              label={eventStreamState === "connected" ? "streaming" : eventStreamState}
              tone={eventStreamState === "connected" ? "success" : "warning"}
            />
            {lastEvent ? (
              <text
                fg={colors.muted}
              >{`last update #${lastEvent.sequence} · ${lastEvent.type}`}</text>
            ) : (
              <text fg={colors.faint}>Waiting for the first live update.</text>
            )}
          </box>
          {lastEvent ? <text fg={colors.faint}>{lastEvent.created_at}</text> : null}
        </Card>
      </Section>
      {connection.state === "disconnected" ? (
        <Card tone="warning">
          <text fg={colors.warning} attributes={BOLD}>
            The daemon is offline
          </text>
          <text fg={colors.muted}>Start it in another terminal with: make api</text>
        </Card>
      ) : null}
    </box>
  )
}

function HelpScreen({ screen }: { screen: Screen }) {
  const screenShortcuts: Record<Screen, Array<{ key: string; label: string }>> = {
    projects: [
      { key: "↑↓", label: "select project" },
      { key: "N", label: "new project" },
      { key: "P", label: "new plan" },
      { key: "S", label: "scan HEAD" },
      { key: "O", label: "normalize plan" },
      { key: "A", label: "run planning agent" },
      { key: "Q", label: "answer questions" },
      { key: "W", label: "create workflow" },
      { key: "B", label: "choose base branch" },
      { key: "T", label: "toggle repository trust" },
      { key: "I", label: "edit ignore policy" },
      { key: "G", label: "refresh selected" },
      { key: "D", label: "delete registration" },
      { key: "R", label: "reload list" },
    ],
    agents: [
      { key: "↑↓", label: "select project" },
      { key: "Tab", label: "switch role" },
      { key: "E", label: "toggle agent" },
      { key: "N", label: "cycle network access" },
      { key: "F", label: "cycle filesystem access" },
      { key: "S", label: "save settings" },
      { key: "R", label: "reload" },
    ],
    jobs: [
      { key: "↑↓", label: "select job" },
      { key: "A", label: "approve" },
      { key: "V", label: "request revision" },
      { key: "X", label: "reject" },
      { key: "E", label: "take over or release workspace" },
      { key: "R", label: "reload jobs" },
    ],
    settings: [{ key: "R", label: "reconnect" }],
    diagnostics: [{ key: "R", label: "reconnect" }],
  }

  return (
    <box flexDirection="column" gap={1} flexGrow={1}>
      <PageHeader
        title="Everything you can do from here"
        description="Arrow keys move. Letter keys act. The current screen's shortcuts are listed below, and every key is shown next to where it works."
        meta="press ? or Esc to close"
      />
      <Section title="GLOBAL">
        <Card tone="accent">
          <KeyHints
            shortcuts={[
              { key: "←→", label: "previous / next screen" },
              { key: "1–5", label: "jump to screen" },
              { key: "/", label: "search actions" },
              { key: "?", label: "toggle this guide" },
              { key: "Ctrl+C", label: "quit" },
            ]}
          />
        </Card>
      </Section>
      <Section title={`${screenLabel(screen)} shortcuts`}>
        <Card>
          <box flexDirection="row" flexWrap="wrap" columnGap={2} rowGap={0}>
            {screenShortcuts[screen].map((shortcut) => (
              <box key={`${shortcut.key}-${shortcut.label}`} flexDirection="row" gap={1}>
                <Key>{shortcut.key}</Key>
                <text fg={colors.muted}>{shortcut.label}</text>
              </box>
            ))}
          </box>
        </Card>
      </Section>
      <text fg={colors.faint}>
        Inside text fields: Tab moves to the next field, Esc cancels, Ctrl+S saves or submits.
      </text>
    </box>
  )
}
