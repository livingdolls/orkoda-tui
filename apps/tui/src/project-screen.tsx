/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useEffect, useState } from "react"

import type { DaemonConnection } from "./daemon"
import {
  createProject,
  deleteProject,
  listProjects,
  type Project,
  refreshProject,
} from "./projects"

type ProjectMode = "list" | "create" | "delete"
type ProjectField = "name" | "path"
type ProjectLoadState = "idle" | "loading" | "ready" | "error"

export function ProjectScreen({
  connection,
  onInteractionChange,
}: {
  connection: DaemonConnection
  onInteractionChange: (active: boolean) => void
}) {
  const [projectList, setProjectList] = useState<Project[]>([])
  const [loadState, setLoadState] = useState<ProjectLoadState>("idle")
  const [message, setMessage] = useState("")
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [mode, setMode] = useState<ProjectMode>("list")
  const [field, setField] = useState<ProjectField>("name")
  const [draftName, setDraftName] = useState("")
  const [draftPath, setDraftPath] = useState("")
  const [busy, setBusy] = useState(false)

  const selectedProject = projectList[selectedIndex] ?? null

  const reloadProjects = async () => {
    if (connection.state !== "connected") {
      setLoadState("idle")
      setMessage("Start the daemon before loading projects.")
      return
    }
    setLoadState("loading")
    setMessage("")
    try {
      const projects = await listProjects()
      setProjectList(projects)
      setSelectedIndex((current) => Math.min(current, Math.max(projects.length - 1, 0)))
      setLoadState("ready")
    } catch (error) {
      setLoadState("error")
      setMessage(error instanceof Error ? error.message : "Failed to load projects")
    }
  }

  const submitProject = async () => {
    if (busy) {
      return
    }
    if (!draftName.trim() || !draftPath.trim()) {
      setMessage("Project name and repository path are required.")
      return
    }

    setBusy(true)
    setMessage("Inspecting Git repository...")
    try {
      const project = await createProject(draftName, draftPath)
      setProjectList((current) => [project, ...current])
      setSelectedIndex(0)
      setMode("list")
      setDraftName("")
      setDraftPath("")
      setMessage("Project created.")
      setLoadState("ready")
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to create project")
    } finally {
      setBusy(false)
    }
  }

  const removeSelectedProject = async () => {
    if (!selectedProject || busy) {
      return
    }
    setBusy(true)
    setMessage("Deleting project...")
    try {
      await deleteProject(selectedProject.id)
      setProjectList((current) => current.filter((project) => project.id !== selectedProject.id))
      setSelectedIndex((current) => Math.max(current - 1, 0))
      setMode("list")
      setMessage("Project deleted. Repository files were not changed.")
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to delete project")
    } finally {
      setBusy(false)
    }
  }

  const refreshSelectedProject = async () => {
    if (!selectedProject || busy) {
      return
    }
    setBusy(true)
    setMessage("Refreshing Git metadata...")
    try {
      const refreshed = await refreshProject(selectedProject.id)
      setProjectList((current) =>
        current.map((project) => (project.id === refreshed.id ? refreshed : project)),
      )
      setMessage("Repository metadata refreshed.")
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to refresh project")
    } finally {
      setBusy(false)
    }
  }

  useKeyboard((key) => {
    if (mode === "create") {
      if (key.name === "escape") {
        setMode("list")
        setMessage("Project creation cancelled.")
      } else if (key.name === "tab") {
        setField((current) => (current === "name" ? "path" : "name"))
      }
      return
    }

    if (mode === "delete") {
      if (key.name === "y") {
        void removeSelectedProject()
      } else if (key.name === "n" || key.name === "escape") {
        setMode("list")
        setMessage("Deletion cancelled.")
      }
      return
    }

    if (key.name === "n") {
      setMode("create")
      setField("name")
      setMessage("")
      return
    }
    if (key.name === "d" && selectedProject) {
      setMode("delete")
      setMessage("")
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
      setSelectedIndex((current) => Math.min(current + 1, projectList.length - 1))
      return
    }
    if ((key.name === "up" || key.name === "k") && projectList.length > 0) {
      setSelectedIndex((current) => Math.max(current - 1, 0))
    }
  })

  useEffect(() => {
    onInteractionChange(mode !== "list")
  }, [mode, onInteractionChange])

  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected") {
      setLoadState("idle")
      return
    }

    setLoadState("loading")
    void listProjects()
      .then((projects) => {
        if (!disposed) {
          setProjectList(projects)
          setSelectedIndex((current) => Math.min(current, Math.max(projects.length - 1, 0)))
          setLoadState("ready")
          setMessage("")
        }
      })
      .catch((error) => {
        if (!disposed) {
          setLoadState("error")
          setMessage(error instanceof Error ? error.message : "Failed to load projects")
        }
      })

    return () => {
      disposed = true
    }
  }, [connection.state])

  if (mode === "create") {
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#E2E8F0">Register a local Git repository</text>
        <text fg={field === "name" ? "#7DD3FC" : "#64748B"}>Project name</text>
        <input
          value={draftName}
          placeholder="Example: Orkoda Website"
          focused={field === "name"}
          onInput={setDraftName}
          onSubmit={() => setField("path")}
        />
        <text fg={field === "path" ? "#7DD3FC" : "#64748B"}>Repository path</text>
        <input
          value={draftPath}
          placeholder="Example: /home/user/App/project"
          focused={field === "path"}
          onInput={setDraftPath}
          onSubmit={() => void submitProject()}
        />
        <text fg="#94A3B8">
          Orkoda validates the Git root, branch, HEAD, remote, and working tree status.
        </text>
        {message ? <text fg={busy ? "#FACC15" : "#F87171"}>{message}</text> : null}
      </box>
    )
  }

  if (mode === "delete") {
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#FACC15">Delete project registration?</text>
        <text fg="#E2E8F0">{selectedProject?.name ?? "Unknown project"}</text>
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
  if (projectList.length === 0) {
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#E2E8F0">No project registered yet.</text>
        <text fg="#7DD3FC">Press n to register a local Git repository.</text>
        {message ? <text fg="#94A3B8">{message}</text> : null}
      </box>
    )
  }

  const repository = selectedProject?.repositories[0]
  return (
    <box flexDirection="row" flexGrow={1} gap={2}>
      <box width="42%" flexDirection="column" gap={1}>
        {projectList.map((project, index) => (
          <text key={project.id} fg={index === selectedIndex ? "#7DD3FC" : "#94A3B8"}>
            {index === selectedIndex ? `› ${project.name}` : `  ${project.name}`}
          </text>
        ))}
      </box>
      <box flexGrow={1} flexDirection="column" gap={1}>
        <text fg="#E2E8F0">{selectedProject?.name}</text>
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
