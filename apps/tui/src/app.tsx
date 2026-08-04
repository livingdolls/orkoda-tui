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
  CommandPalette,
  colors,
  Metric,
  PageIntro,
  type PaletteCommand,
  Panel,
  ShortcutBar,
  StatusBadge,
  Toast,
  toneColor,
} from "./ui"

const connectionTones: Record<DaemonConnection["state"], "warning" | "success" | "danger"> = {
  checking: "warning",
  connected: "success",
  disconnected: "danger",
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
  const [sidebarWidth, setSidebarWidth] = useState(29)
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
        Math.min(42, Math.max(22, current + (key.name === "right" ? 2 : -2))),
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

  let footerHelp = "←→ screen • 1–5 jump • ? help • Ctrl+←→ resize • Ctrl+C quit"
  if (activeScreen === "projects") {
    footerHelp = projectInteractionActive
      ? "Dialog active • use the action bar • Esc cancel"
      : "↑↓ select • N new • P plan • S scan • R reload • ? help"
  } else if (activeScreen === "agents") {
    footerHelp = "↑↓ project • Tab role • E toggle • N network • F filesystem • S save • ? help"
  } else if (activeScreen === "jobs") {
    footerHelp = jobsInteractionActive
      ? "Composer active • Ctrl+S apply • Esc cancel • O override"
      : "↑↓ select • A approve • V revise • X reject • E edit • R reload • ? help"
  }

  return (
    <box flexDirection="column" width="100%" height="100%" backgroundColor={colors.canvas}>
      <box
        height={4}
        paddingLeft={1}
        paddingRight={1}
        flexDirection="row"
        justifyContent="space-between"
        alignItems="center"
        backgroundColor={colors.surface}
      >
        <box flexDirection="row" gap={1} alignItems="center">
          <text fg={colors.accent}>ORKODA</text>
          <text fg={colors.dim}>/</text>
          <text fg={colors.text}>{screenLabel(activeScreen)}</text>
        </box>
        <box flexDirection="row" gap={2} alignItems="center">
          <text fg={colors.dim}>local control room</text>
          <StatusBadge label={connection.state} tone={connectionTones[connection.state]} />
        </box>
      </box>

      <box flexGrow={1} flexDirection="row" padding={1} gap={1}>
        {!compactLayout ? (
          <Panel width={sidebarWidth} title="WORKSPACE" borderColor={colors.lineStrong}>
            <text fg={colors.dim}>SCREENS</text>
            {screenDefinitions.map((item, index) => {
              const selected = item.id === activeScreen
              return (
                <box
                  key={item.id}
                  flexDirection="column"
                  gap={1}
                  padding={1}
                  backgroundColor={selected ? colors.surfaceAccent : colors.surface}
                  borderStyle="rounded"
                  borderColor={selected ? colors.accent : colors.line}
                >
                  <text fg={selected ? colors.accent : colors.muted}>
                    {`${selected ? "›" : " "}  ${index + 1}  ${item.label}`}
                  </text>
                  <text fg={selected ? colors.text : colors.dim}>{item.description}</text>
                </box>
              )
            })}
          </Panel>
        ) : null}

        <box
          flexGrow={1}
          flexDirection="column"
          borderStyle="rounded"
          borderColor={colors.lineStrong}
          backgroundColor={colors.surface}
          padding={1}
          gap={1}
        >
          {showHelp ? (
            activeScreen === "projects" ? (
              <ProjectScreen
                connection={connection}
                onInteractionChange={setProjectInteractionActive}
                helpOpen={showHelp}
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
          top={5}
          left="8%"
          width="84%"
          height="80%"
          padding={2}
          borderStyle="rounded"
          borderColor={colors.accent}
          backgroundColor={colors.canvas}
        >
          <HelpScreen screen={activeScreen} />
        </box>
      ) : null}

      <box
        height={3}
        paddingLeft={1}
        paddingRight={1}
        flexDirection="column"
        justifyContent="space-between"
        backgroundColor={colors.surface}
      >
        <box flexDirection="row" justifyContent="space-between">
          <text fg={colors.muted}>{footerHelp}</text>
          <text fg={toneColor(connectionTones[connection.state])}>
            {`${connection.protocolVersion} • ${connection.message}`}
          </text>
        </box>
        <ShortcutBar
          subdued
          shortcuts={[
            { key: "←→", label: "screen" },
            { key: "1–5", label: "jump" },
            { key: "Ctrl+←→", label: "resize" },
            { key: "?", label: "help" },
            { key: "Ctrl+C", label: "quit" },
          ]}
        />
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
      <PageIntro
        kicker="DIAGNOSTICS"
        title="Daemon connection"
        description="A quick read on the local control plane and protocol handshake."
      />
      <Panel title="LIVE CONNECTION" borderColor={toneColor(connectionTones[connection.state])}>
        <StatusBadge label={connection.state} tone={connectionTones[connection.state]} />
        <text fg={colors.muted}>{connection.message}</text>
        <box flexDirection="row" gap={1}>
          <Metric label="Protocol" value={connection.protocolVersion} tone="accent" />
          <Metric label="Endpoint" value="127.0.0.1:8181" />
        </box>
      </Panel>
      <Panel
        title="DAEMON DIAGNOSTICS"
        borderColor={diagnostics?.status === "ready" ? colors.lineStrong : colors.warning}
      >
        {diagnostics ? (
          <box flexDirection="column" gap={1}>
            <box flexDirection="row" gap={1}>
              <StatusBadge
                label={diagnostics.status}
                tone={diagnostics.status === "ready" ? "success" : "warning"}
              />
              <text fg={colors.dim}>{`schema v${diagnostics.database.schema_version}`}</text>
              <text fg={diagnostics.database.integrity === "ok" ? colors.success : colors.danger}>
                {`SQLite ${diagnostics.database.integrity}`}
              </text>
            </box>
            <box flexDirection="row" gap={1}>
              <Metric label="Queue" value={`${diagnostics.queue.queued} queued`} />
              <Metric label="Running" value={`${diagnostics.queue.running}`} />
              <Metric
                label="Dead"
                value={`${diagnostics.queue.dead}`}
                tone={diagnostics.queue.dead > 0 ? "danger" : "success"}
              />
              <Metric
                label="Leases"
                value={`${diagnostics.workspaces.active_leases}/${diagnostics.workspaces.total}`}
              />
            </box>
          </box>
        ) : (
          <text fg={colors.dim}>Diagnostics are temporarily unavailable.</text>
        )}
      </Panel>
      <Panel
        title="EVENT STREAM"
        borderColor={eventStreamState === "connected" ? colors.success : colors.warning}
      >
        <box flexDirection="row" gap={1}>
          <StatusBadge
            label={eventStreamState}
            tone={eventStreamState === "connected" ? "success" : "warning"}
          />
          {lastEvent ? (
            <text fg={colors.muted}>{`#${lastEvent.sequence} ${lastEvent.type}`}</text>
          ) : (
            <text fg={colors.dim}>Waiting for a durable activity event.</text>
          )}
        </box>
        {lastEvent ? <text fg={colors.dim}>{lastEvent.created_at}</text> : null}
      </Panel>
      {connection.state === "disconnected" ? (
        <Panel borderColor={colors.warning} backgroundColor="#211E14">
          <text fg={colors.warning}>Daemon is offline</text>
          <text fg={colors.muted}>Start it in another terminal with: make api</text>
        </Panel>
      ) : null}
      <ShortcutBar
        shortcuts={[
          { key: "R", label: "reconnect" },
          { key: "?", label: "keyboard guide" },
        ]}
      />
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
      { key: "D", label: "delete registration" },
      { key: "G", label: "refresh selected" },
      { key: "R", label: "reload list" },
    ],
    agents: [
      { key: "↑↓", label: "select project" },
      { key: "Tab", label: "switch role" },
      { key: "E", label: "toggle agent" },
      { key: "S", label: "save settings" },
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
      <PageIntro
        kicker="KEYBOARD GUIDE"
        title="Everything you can do from here"
        description="Use the arrow keys first. Letter shortcuts are shown in cyan and are always contextual to the current screen."
        meta="Press ? or Esc to close"
      />
      <Panel title="GLOBAL" borderColor={colors.accent} backgroundColor={colors.surfaceAccent}>
        <ShortcutBar
          shortcuts={[
            { key: "←→", label: "previous / next screen" },
            { key: "1–5", label: "jump to screen" },
            { key: "R", label: "reconnect or reload" },
            { key: "?", label: "toggle this guide" },
            { key: "Ctrl+C", label: "quit" },
          ]}
        />
      </Panel>
      <Panel title={`${screenLabel(screen).toUpperCase()} ACTIONS`}>
        {screenShortcuts[screen].map((shortcut) => (
          <box key={`${shortcut.key}-${shortcut.label}`} flexDirection="row" gap={2}>
            <text fg={colors.accent}>{shortcut.key}</text>
            <text fg={colors.text}>{shortcut.label}</text>
          </box>
        ))}
      </Panel>
      <text fg={colors.dim}>
        Focused inputs use Tab to move and Esc to cancel. Ctrl+S saves or submits.
      </text>
    </box>
  )
}
