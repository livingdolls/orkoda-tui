/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useEffect, useState } from "react"

import {
  type DaemonConnection,
  daemonBaseURL,
  initialDaemonConnection,
  probeDaemon,
} from "./daemon"
import {
  moveScreen,
  type Screen,
  screenDefinitions,
  screenFromShortcut,
  screenLabel,
} from "./navigation"
import { ProjectScreen } from "./project-screen"

const connectionColors: Record<DaemonConnection["state"], string> = {
  checking: "#FACC15",
  connected: "#4ADE80",
  disconnected: "#F87171",
}

export function App() {
  const [activeScreen, setActiveScreen] = useState<Screen>("projects")
  const [connection, setConnection] = useState<DaemonConnection>(initialDaemonConnection)
  const [projectInteractionActive, setProjectInteractionActive] = useState(false)

  useKeyboard((key) => {
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

  let footerHelp = "↑↓/hjkl navigate • 1-4 jump • r reconnect • Ctrl+C quit"
  if (activeScreen === "projects") {
    footerHelp = projectInteractionActive
      ? "Project dialog active • use the controls shown in the panel"
      : "n project • p plan • s scan • o normalize • ↑↓/jk select • g refresh • h/l screen"
  }

  return (
    <box flexDirection="column" width="100%" height="100%" backgroundColor="#0B1020">
      <box
        height={3}
        paddingLeft={1}
        paddingRight={1}
        flexDirection="row"
        justifyContent="space-between"
        alignItems="center"
        backgroundColor="#11182B"
      >
        <text fg="#7DD3FC">ORKODA</text>
        <text fg="#94A3B8">AI software development orchestrator</text>
        <text fg={connectionColors[connection.state]}>{connection.state}</text>
      </box>

      <box flexGrow={1} flexDirection="row" padding={1} gap={1}>
        <box
          width={24}
          flexDirection="column"
          borderStyle="rounded"
          borderColor="#334155"
          padding={1}
          gap={1}
          title="Navigation"
        >
          {screenDefinitions.map((item, index) => {
            const selected = item.id === activeScreen
            return (
              <text key={item.id} fg={selected ? "#7DD3FC" : "#94A3B8"}>
                {selected ? `› ${index + 1}. ${item.label}` : `  ${index + 1}. ${item.label}`}
              </text>
            )
          })}
        </box>

        <box
          flexGrow={1}
          flexDirection="column"
          borderStyle="rounded"
          borderColor="#334155"
          padding={1}
          gap={1}
          title={screenLabel(activeScreen)}
        >
          {activeScreen === "projects" ? (
            <ProjectScreen
              connection={connection}
              onInteractionChange={setProjectInteractionActive}
            />
          ) : (
            <ScreenContent screen={activeScreen} connection={connection} />
          )}
        </box>
      </box>

      <box
        height={1}
        paddingLeft={1}
        paddingRight={1}
        flexDirection="row"
        justifyContent="space-between"
      >
        <text fg="#64748B">{footerHelp}</text>
        <text fg={connectionColors[connection.state]}>
          {`${connection.protocolVersion} • daemon ${connection.state}`}
        </text>
      </box>
    </box>
  )
}

function ScreenContent({ screen, connection }: { screen: Screen; connection: DaemonConnection }) {
  if (screen === "jobs") {
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#E2E8F0">No workflow job has been submitted.</text>
        <text fg="#94A3B8">Durable jobs are stored in the local SQLite database.</text>
      </box>
    )
  }

  if (screen === "settings") {
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#E2E8F0">Local daemon endpoint</text>
        <text fg="#7DD3FC">{daemonBaseURL}</text>
        <text fg="#94A3B8">Override with ORKODA_DAEMON_URL before running the TUI.</text>
      </box>
    )
  }

  return (
    <box flexDirection="column" gap={1}>
      <text fg="#E2E8F0">Daemon connection</text>
      <text fg={connectionColors[connection.state]}>{connection.state}</text>
      <text fg="#94A3B8">{connection.message}</text>
      {connection.state === "disconnected" ? (
        <text fg="#FACC15">Start it in another terminal with: make api</text>
      ) : null}
      <text fg="#64748B">Navigation remains available while the daemon is offline.</text>
    </box>
  )
}
