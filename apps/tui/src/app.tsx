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
import {
  createProject,
  deleteProject,
  listProjects,
  type Project,
  refreshProject,
} from "./projects"

const connectionColors: Record<DaemonConnection["state"], string> = {
  checking: "#FACC15",
  connected: "#4ADE80",
  disconnected: "#F87171",
}

type ProjectMode = "list" | "create" | "delete"
type ProjectField = "name" | "path"
type ProjectLoadState = "idle" | "loading" | "ready" | "error"

export function App() {
  const [activeScreen, setActiveScreen] = useState<Screen>("projects")
  const [connection, setConnection] = useState<DaemonConnection>(initialDaemonConnection)
  const [projectList, setProjectList] = useState<Project[]>([])
  const [projectLoadState, setProjectLoadState] = useState<ProjectLoadState>("idle")
  const [projectMessage, setProjectMessage] = useState("")
  const [selectedProjectIndex, setSelectedProjectIndex] = useState(0)
  const [projectMode, setProjectMode] = useState<ProjectMode>("list")
  const [projectField, setProjectField] = useState<ProjectField>("name")
  const [draftName, setDraftName] = useState("")
  const [draftPath, setDraftPath] = useState("")
  const [projectBusy, setProjectBusy] = useState(false)

  const selectedProject = projectList[selectedProjectIndex] ?? null

  const reloadProjects = async () => {
    if (connection.state !== "connected") {
      setProjectLoadState("idle")
      setProjectMessage("Start the daemon before loading projects.")
      return
    }
    setProjectLoadState("loading")
    setProjectMessage("")
    try {
      const projects = await listProjects()
      setProjectList(projects)
      setSelectedProjectIndex((current) => Math.min(current, Math.max(projects.length - 1, 0)))
      setProjectLoadState("ready")
    } catch (error) {
      setProjectLoadState("error")
      setProjectMessage(error instanceof Error ? error.message : "Failed to load projects")
    }
  }

  const submitProject = async () => {
    if (projectBusy) {
      return
    }
    if (!draftName.trim() || !draftPath.trim()) {
      setProjectMessage("Project name and repository path are required.")
      return
    }

    setProjectBusy(true)
    setProjectMessage("Inspecting Git repository...")
    try {
      const project = await createProject(draftName, draftPath)
      setProjectList((current) => [project, ...current])
      setSelectedProjectIndex(0)
      setProjectMode("list")
      setDraftName("")
      setDraftPath("")
      setProjectMessage("Project created.")
      setProjectLoadState("ready")
    } catch (error) {
      setProjectMessage(error instanceof Error ? error.message : "Failed to create project")
    } finally {
      setProjectBusy(false)
    }
  }

  const removeSelectedProject = async () => {
    if (!selectedProject || projectBusy) {
      return
    }
    setProjectBusy(true)
    setProjectMessage("Deleting project...")
    try {
      await deleteProject(selectedProject.id)
      setProjectList((current) => current.filter((project) => project.id !== selectedProject.id))
      setSelectedProjectIndex((current) => Math.max(current - 1, 0))
      setProjectMode("list")
      setProjectMessage("Project deleted. Repository files were not changed.")
    } catch (error) {
      setProjectMessage(error instanceof Error ? error.message : "Failed to delete project")
    } finally {
      setProjectBusy(false)
    }
  }

  const refreshSelectedProject = async () => {
    if (!selectedProject || projectBusy) {
      return
    }
    setProjectBusy(true)
    setProjectMessage("Refreshing Git metadata...")
    try {
      const refreshed = await refreshProject(selectedProject.id)
      setProjectList((current) =>
        current.map((project) => (project.id === refreshed.id ? refreshed : project)),
      )
      setProjectMessage("Repository metadata refreshed.")
    } catch (error) {
      setProjectMessage(error instanceof Error ? error.message : "Failed to refresh project")
    } finally {
      setProjectBusy(false)
    }
  }

  useKeyboard((key) => {
    if (activeScreen === "projects") {
      if (projectMode === "create") {
        if (key.name === "escape") {
          setProjectMode("list")
          setProjectMessage("Project creation cancelled.")
        } else if (key.name === "tab") {
          setProjectField((current) => (current === "name" ? "path" : "name"))
        }
        return
      }

      if (projectMode === "delete") {
        if (key.name === "y") {
          void removeSelectedProject()
        } else if (key.name === "n" || key.name === "escape") {
          setProjectMode("list")
          setProjectMessage("Deletion cancelled.")
        }
        return
      }

      if (key.name === "n") {
        setProjectMode("create")
        setProjectField("name")
        setProjectMessage("")
        return
      }
      if (key.name === "d" && selectedProject) {
        setProjectMode("delete")
        setProjectMessage("")
        return
      }
      if (key.name === "g" && selectedProject) {
        void refreshSelectedProject()
        return
      }
      if (key.name === "r") {
        void reloadProjects()
        return
      }
      if ((key.name === "down" || key.name === "j") && projectList.length > 0) {
        setSelectedProjectIndex((current) => Math.min(current + 1, projectList.length - 1))
        return
      }
      if ((key.name === "up" || key.name === "k") && projectList.length > 0) {
        setSelectedProjectIndex((current) => Math.max(current - 1, 0))
        return
      }
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
      return
    }
    if (key.name === "down" || key.name === "j") {
      setActiveScreen((current) => moveScreen(current, 1))
      return
    }
    if (key.name === "up" || key.name === "k") {
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
    let disposed = false
    if (connection.state !== "connected") {
      setProjectLoadState("idle")
      return
    }

    setProjectLoadState("loading")
    void listProjects()
      .then((projects) => {
        if (!disposed) {
          setProjectList(projects)
          setSelectedProjectIndex((current) =>
            Math.min(current, Math.max(projects.length - 1, 0)),
          )
          setProjectLoadState("ready")
          setProjectMessage("")
        }
      })
      .catch((error) => {
        if (!disposed) {
          setProjectLoadState("error")
          setProjectMessage(error instanceof Error ? error.message : "Failed to load projects")
        }
      })

    return () => {
      disposed = true
    }
  }, [connection.state])

  const footerHelp =
    activeScreen === "projects"
      ? projectMode === "create"
        ? "Tab switch field • Enter continue/save • Esc cancel"
        : projectMode === "delete"
          ? "y confirm delete • n/Esc cancel"
          : "n new • ↑↓/jk select • g refresh Git • d delete • r reload"
      : "↑↓/hjkl navigate • 1-4 jump • r reconnect • Ctrl+C quit"

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
            <ProjectsContent
              connection={connection}
              projects={projectList}
              selectedIndex={selectedProjectIndex}
              loadState={projectLoadState}
              message={projectMessage}
              mode={projectMode}
              field={projectField}
              draftName={draftName}
              draftPath={draftPath}
              busy={projectBusy}
              onNameInput={setDraftName}
              onPathInput={setDraftPath}
              onNameSubmit={() => setProjectField("path")}
              onPathSubmit={() => void submitProject()}
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

function ProjectsContent({
  connection,
  projects,
  selectedIndex,
  loadState,
  message,
  mode,
  field,
  draftName,
  draftPath,
  busy,
  onNameInput,
  onPathInput,
  onNameSubmit,
  onPathSubmit,
}: {
  connection: DaemonConnection
  projects: Project[]
  selectedIndex: number
  loadState: ProjectLoadState
  message: string
  mode: ProjectMode
  field: ProjectField
  draftName: string
  draftPath: string
  busy: boolean
  onNameInput: (value: string) => void
  onPathInput: (value: string) => void
  onNameSubmit: () => void
  onPathSubmit: () => void
}) {
  if (mode === "create") {
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#E2E8F0">Register a local Git repository</text>
        <text fg={field === "name" ? "#7DD3FC" : "#64748B"}>Project name</text>
        <input
          value={draftName}
          placeholder="Example: Orkoda Website"
          focused={field === "name"}
          onInput={onNameInput}
          onSubmit={onNameSubmit}
        />
        <text fg={field === "path" ? "#7DD3FC" : "#64748B"}>Repository path</text>
        <input
          value={draftPath}
          placeholder="Example: /home/user/App/project"
          focused={field === "path"}
          onInput={onPathInput}
          onSubmit={onPathSubmit}
        />
        <text fg="#94A3B8">
          Orkoda validates the Git root, branch, HEAD, remote, and working tree status.
        </text>
        {message ? <text fg={busy ? "#FACC15" : "#F87171"}>{message}</text> : null}
      </box>
    )
  }

  if (mode === "delete") {
    const project = projects[selectedIndex]
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#FACC15">Delete project registration?</text>
        <text fg="#E2E8F0">{project?.name ?? "Unknown project"}</text>
        <text fg="#94A3B8">Repository files and Git history will not be modified.</text>
        <text fg="#F87171">Press y to confirm, n or Esc to cancel.</text>
        {message ? <text fg="#94A3B8">{message}</text> : null}
      </box>
    )
  }

  if (connection.state !== "connected") {
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#FACC15">Daemon is not connected.</text>
        <text fg="#94A3B8">Start it in another terminal with: make api</text>
      </box>
    )
  }
  if (loadState === "loading") {
    return <text fg="#FACC15">Loading projects...</text>
  }
  if (loadState === "error") {
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#F87171">Failed to load projects.</text>
        <text fg="#94A3B8">{message}</text>
      </box>
    )
  }
  if (projects.length === 0) {
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#E2E8F0">No project registered yet.</text>
        <text fg="#7DD3FC">Press n to register a local Git repository.</text>
        {message ? <text fg="#94A3B8">{message}</text> : null}
      </box>
    )
  }

  const selected = projects[selectedIndex] ?? projects[0]
  const repository = selected?.repositories[0]
  return (
    <box flexDirection="row" flexGrow={1} gap={2}>
      <box width="42%" flexDirection="column" gap={1}>
        {projects.map((project, index) => (
          <text key={project.id} fg={index === selectedIndex ? "#7DD3FC" : "#94A3B8"}>
            {index === selectedIndex ? `› ${project.name}` : `  ${project.name}`}
          </text>
        ))}
      </box>
      <box flexGrow={1} flexDirection="column" gap={1}>
        <text fg="#E2E8F0">{selected?.name}</text>
        <text fg="#94A3B8">{repository?.local_path ?? "No repository"}</text>
        <text fg="#94A3B8">
          {`branch: ${repository?.current_branch || "detached"} • ${repository?.dirty ? "dirty" : "clean"}`}
        </text>
        <text fg="#64748B">{`HEAD ${repository?.head_sha.slice(0, 12) ?? "unknown"}`}</text>
        {repository?.remote_url ? <text fg="#64748B">{repository.remote_url}</text> : null}
        {message ? <text fg={busy ? "#FACC15" : "#4ADE80"}>{message}</text> : null}
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
