/** @jsxImportSource @opentui/react */

import { useKeyboard, useOnResize } from "@opentui/react"
import { useEffect, useState } from "react"

import { AgentSettingsScreen } from "./agent-settings-screen"
import { type DaemonConnection, initialDaemonConnection, probeDaemon } from "./daemon"
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
import { colors, Metric, PageIntro, Panel, ShortcutBar, StatusBadge, toneColor } from "./ui"

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
  const renderer = useOnResize((width) => setTerminalWidth(width))

  const compactLayout = (terminalWidth || renderer.width) < 100

  useKeyboard((key) => {
    if (showHelp) {
      if (key.name === "escape" || key.name === "?") {
        setShowHelp(false)
      }
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

  let footerHelp = "←→ screen • 1–5 jump • ? help • Ctrl+C quit"
  if (activeScreen === "projects") {
    footerHelp = projectInteractionActive
      ? "Dialog active • use the action bar • Esc cancel"
      : "↑↓ select • N new • P plan • S scan • R reload • ? help"
  } else if (activeScreen === "agents") {
    footerHelp = "↑↓ project • Tab role • E toggle • N network • F filesystem • S save • ? help"
  } else if (activeScreen === "jobs") {
    footerHelp = jobsInteractionActive
      ? "Composer active • Ctrl+S apply • Esc cancel • O override"
      : "↑↓ select • A approve • V revise • X reject • R reload • ? help"
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
          <Panel width={29} title="WORKSPACE" borderColor={colors.lineStrong}>
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
            <HelpScreen screen={activeScreen} />
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
            />
          )}
        </box>
      </box>

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
            { key: "?", label: "help" },
            { key: "Ctrl+C", label: "quit" },
          ]}
        />
      </box>
    </box>
  )
}

function ScreenContent({
  screen,
  connection,
  onJobsInteractionChange,
}: {
  screen: Screen
  connection: DaemonConnection
  onJobsInteractionChange: (active: boolean) => void
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
