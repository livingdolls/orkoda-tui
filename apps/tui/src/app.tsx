/** @jsxImportSource @opentui/react */

import { useKeyboard, useOnResize } from "@opentui/react"
import { useEffect, useRef, useState } from "react"

import { AgentSettingsScreen } from "./agent-settings-screen"
import { BoardScreen } from "./board-screen"
import {
  createDiagnosticsBundle,
  type DaemonConnection,
  type DiagnosticsSnapshot,
  getDiagnostics,
  initialDaemonConnection,
  probeDaemon,
} from "./daemon"
import { type ActivityEvent, subscribeToEvents } from "./events"
import {
  moveScreen,
  type Screen,
  screenDefinitions,
  screenFromShortcut,
  screenLabel,
} from "./navigation"
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
  const [activeScreen, setActiveScreen] = useState<Screen>("board")
  const [connection, setConnection] = useState<DaemonConnection>(initialDaemonConnection)
  const [terminalWidth, setTerminalWidth] = useState(0)
  const [boardInteractionActive, setBoardInteractionActive] = useState(false)
  const [showHelp, setShowHelp] = useState(false)
  const [showPalette, setShowPalette] = useState(false)
  const [lastEvent, setLastEvent] = useState<ActivityEvent | null>(null)
  const [diagnostics, setDiagnostics] = useState<DiagnosticsSnapshot | null>(null)
  const [eventStreamState, setEventStreamState] = useState<
    "connected" | "reconnecting" | "closed"
  >("closed")
  const [toast, setToast] = useState("")
  const lastSequenceRef = useRef(0)
  const renderer = useOnResize((width) => setTerminalWidth(width))
  const compactLayout = (terminalWidth || renderer.width) < 100

  useKeyboard((key) => {
    if (showPalette) return
    if (boardInteractionActive) return
    if (showHelp) {
      if (key.name === "escape" || key.name === "?") setShowHelp(false)
      return
    }
    if ((key.ctrl && key.name === "k") || key.name === "/") {
      setShowPalette(true)
      return
    }
    if (key.name === "?") {
      setShowHelp(true)
      return
    }

    const shortcut = screenFromShortcut(key.name)
    if (shortcut) {
      setActiveScreen(shortcut)
      return
    }

    if (activeScreen === "board") return

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
      const next = await probeDaemon()
      if (!disposed) setConnection(next)
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
    return subscribeToEvents({
      afterSequence: lastSequenceRef.current,
      onEvent: (event) => {
        lastSequenceRef.current = event.sequence
        setLastEvent(event)
      },
      onState: setEventStreamState,
    })
  }, [connection.state])

  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected") {
      setDiagnostics(null)
      return
    }
    const refresh = async () => {
      try {
        const next = await getDiagnostics()
        if (!disposed) setDiagnostics(next)
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

  const footerShortcuts: Shortcut[] =
    activeScreen === "board"
      ? [
          { key: "1–4", label: "switch area" },
          { key: "/", label: "search actions" },
          { key: "?", label: "help" },
        ]
      : [
          { key: "←→", label: "switch area" },
          { key: "1–4", label: "jump" },
          { key: "/", label: "search actions" },
          { key: "?", label: "help" },
        ]

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
          {eventStreamState === "reconnecting" ? (
            <Chip label="live updates reconnecting" tone="warning" />
          ) : null}
          <Chip label={connectionLabel[connection.state]} tone={connectionTone[connection.state]} />
        </box>
      </box>

      <box flexGrow={1} flexDirection="row" padding={1} gap={1}>
        {!compactLayout ? (
          <box
            width={25}
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
                    <text fg={selected ? colors.accent : colors.faint}>
                      {selected ? "▸" : " "}
                    </text>
                    <text
                      fg={selected ? colors.text : colors.muted}
                      attributes={selected ? BOLD : 0}
                    >
                      {`${index + 1} ${item.label}`}
                    </text>
                  </box>
                  <text fg={selected ? colors.muted : colors.faint}>
                    {`   ${item.description}`}
                  </text>
                </box>
              )
            })}
            <box flexGrow={1} />
            <box flexDirection="row" gap={1} alignItems="center">
              <Key>?</Key>
              <text fg={colors.faint}>keyboard guide</text>
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
            <HelpScreen screen={activeScreen} />
          ) : activeScreen === "board" ? (
            <BoardScreen
              connection={connection}
              lastEvent={lastEvent}
              onInteractionChange={setBoardInteractionActive}
            />
          ) : activeScreen === "agents" ? (
            <AgentSettingsScreen connection={connection} />
          ) : activeScreen === "settings" ? (
            <SettingsScreen connection={connection} />
          ) : (
            <SystemScreen connection={connection} diagnostics={diagnostics} />
          )}
        </box>
      </box>

      <box
        height={2}
        paddingLeft={1}
        paddingRight={1}
        flexDirection="row"
        alignItems="center"
        backgroundColor={colors.surface}
      >
        <KeyHints
          shortcuts={
            compactLayout ? footerShortcuts.filter((item) => item.key !== "?") : footerShortcuts
          }
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

function SystemScreen({
  connection,
  diagnostics,
}: {
  connection: DaemonConnection
  diagnostics: DiagnosticsSnapshot | null
}) {
  return (
    <box flexDirection="column" gap={1} flexGrow={1}>
      <PageHeader
        title="System status"
        description="Health of the local daemon that runs every board workflow."
        meta="press R to reconnect"
      />
      <Section title="Daemon">
        <Card>
          <box flexDirection="row" gap={1} alignItems="center">
            <Chip label={connectionLabel[connection.state]} tone={connectionTone[connection.state]} />
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
              <Chip
                label={diagnostics.status === "ready" ? "ready" : "degraded"}
                tone={diagnostics.status === "ready" ? "success" : "warning"}
              />
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
              <Info
                label="Database"
                value={`schema v${diagnostics.database.schema_version} · ${diagnostics.database.integrity}`}
              />
            </box>
          ) : (
            <text fg={colors.faint}>Diagnostics are temporarily unavailable.</text>
          )}
        </Card>
      </Section>
    </box>
  )
}

function HelpScreen({ screen }: { screen: Screen }) {
  const boardShortcuts: Shortcut[] = [
    { key: "←→", label: "move between kanban columns" },
    { key: "↑↓", label: "select a work card" },
    { key: "Enter", label: "run the card's next action" },
    { key: "Space", label: "open every valid action" },
    { key: "N", label: "create work for the current project" },
    { key: "Shift+N", label: "add a Git project" },
    { key: "Tab", label: "cycle the project filter" },
    { key: "F", label: "toggle active-only / all work" },
    { key: "A / V / X", label: "approve, revise, or reject inside workflow detail" },
  ]
  const general: Shortcut[] = [
    { key: "1–4", label: "jump to Board, Agents, Settings, or System" },
    { key: "/", label: "search actions" },
    { key: "? / Esc", label: "open or close this guide" },
  ]
  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title={`${screenLabel(screen)} keyboard guide`}
        description="The Board is designed around arrows, Enter, Escape, and a visible action menu. Letter shortcuts are optional."
      />
      <Section title={screen === "board" ? "Board" : "Current area"}>
        <Card>
          <KeyHints
            shortcuts={screen === "board" ? boardShortcuts : [{ key: "←→", label: "switch area" }]}
          />
        </Card>
      </Section>
      <Section title="Global">
        <Card>
          <KeyHints shortcuts={general} />
        </Card>
      </Section>
    </box>
  )
}
