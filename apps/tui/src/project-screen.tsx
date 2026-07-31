/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useEffect, useState } from "react"

import type { DaemonConnection } from "./daemon"
import {
  buildDirectoryPickerItems,
  type DirectoryListing,
  initialDirectory,
  listDirectories,
  visibleDirectoryItems,
} from "./directory-picker"
import {
  createProject,
  deleteProject,
  listProjects,
  type Project,
  refreshProject,
} from "./projects"

type ProjectMode = "list" | "name" | "picker" | "delete"
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
  const [draftName, setDraftName] = useState("")
  const [busy, setBusy] = useState(false)
  const [pickerLoading, setPickerLoading] = useState(false)
  const [pickerListing, setPickerListing] = useState<DirectoryListing | null>(null)
  const [pickerIndex, setPickerIndex] = useState(0)

  const selectedProject = projectList[selectedIndex] ?? null
  const pickerItems = pickerListing ? buildDirectoryPickerItems(pickerListing) : []

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

  const openDirectory = async (directory: string) => {
    if (pickerLoading || busy) {
      return
    }

    setMode("picker")
    setPickerLoading(true)
    setMessage("")
    try {
      const listing = await listDirectories(directory)
      setPickerListing(listing)
      setPickerIndex(0)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to read directory")
    } finally {
      setPickerLoading(false)
    }
  }

  const startDirectoryPicker = () => {
    if (!draftName.trim()) {
      setMessage("Project name is required.")
      return
    }
    void openDirectory(pickerListing?.currentPath ?? initialDirectory())
  }

  const submitProject = async (repositoryPath: string) => {
    if (busy) {
      return
    }
    if (!draftName.trim()) {
      setMessage("Project name is required.")
      setMode("name")
      return
    }

    setBusy(true)
    setMessage("Inspecting Git repository...")
    try {
      const project = await createProject(draftName, repositoryPath)
      setProjectList((current) => [project, ...current])
      setSelectedIndex(0)
      setMode("list")
      setDraftName("")
      setPickerListing(null)
      setPickerIndex(0)
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
    if (mode === "name") {
      if (key.name === "escape") {
        setMode("list")
        setMessage("Project creation cancelled.")
      }
      return
    }

    if (mode === "picker") {
      if (key.name === "escape") {
        setMode("name")
        setMessage("")
        return
      }
      if (pickerLoading || busy || !pickerListing) {
        return
      }
      if (key.name === "backspace" || key.name === "left" || key.name === "h") {
        if (pickerListing.parentPath) {
          void openDirectory(pickerListing.parentPath)
        }
        return
      }
      if (key.name === "down" || key.name === "j") {
        setPickerIndex((current) => Math.min(current + 1, pickerItems.length - 1))
        return
      }
      if (key.name === "up" || key.name === "k") {
        setPickerIndex((current) => Math.max(current - 1, 0))
        return
      }
      if (key.name === "s") {
        void submitProject(pickerListing.currentPath)
        return
      }
      if (key.name === "return" || key.name === "enter") {
        const item = pickerItems[pickerIndex]
        if (!item) {
          return
        }
        if (item.kind === "select") {
          void submitProject(item.path)
        } else {
          void openDirectory(item.path)
        }
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
      setMode("name")
      setDraftName("")
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

  if (mode === "name") {
    return (
      <box flexDirection="column" gap={1}>
        <text fg="#E2E8F0">Register a local Git repository</text>
        <text fg="#7DD3FC">Project name</text>
        <input
          value={draftName}
          placeholder="Example: Orkoda Website"
          focused
          onInput={setDraftName}
          onSubmit={startDirectoryPicker}
        />
        <text fg="#94A3B8">Press Enter to choose the repository folder.</text>
        <text fg="#64748B">Esc cancels project creation.</text>
        {message ? <text fg="#F87171">{message}</text> : null}
      </box>
    )
  }

  if (mode === "picker") {
    const visibleItems = visibleDirectoryItems(pickerItems, pickerIndex)
    return (
      <box flexDirection="column" flexGrow={1} gap={1}>
        <text fg="#E2E8F0">Choose repository folder</text>
        <text fg="#7DD3FC">{pickerListing?.currentPath ?? "Loading..."}</text>
        {pickerListing ? (
          <text fg={pickerListing.isGitRepository ? "#4ADE80" : "#94A3B8"}>
            {pickerListing.isGitRepository
              ? "Git repository detected in this folder"
              : "No .git entry detected; the daemon will validate the selected folder"}
          </text>
        ) : null}

        {pickerLoading ? <text fg="#FACC15">Reading directories...</text> : null}
        {!pickerLoading && pickerListing ? (
          <box flexDirection="column" borderStyle="rounded" borderColor="#334155" padding={1}>
            {visibleItems.map(({ item, index }) => {
              let label = item.label
              if (item.kind === "select") {
                label = `[ ${item.label} ]`
              } else if (item.kind === "directory") {
                label = `  ${item.label}`
              }
              return (
                <text
                  key={`${item.kind}:${item.path}`}
                  fg={index === pickerIndex ? "#7DD3FC" : "#94A3B8"}
                >
                  {index === pickerIndex ? `› ${label}` : `  ${label}`}
                </text>
              )
            })}
          </box>
        ) : null}

        <text fg="#64748B">
          ↑↓/jk navigate • Enter open/select • Backspace/h parent • s use current • Esc back
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
