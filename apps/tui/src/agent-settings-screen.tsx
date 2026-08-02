/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useEffect, useState } from "react"

import {
  cloneAgentSettings,
  type AgentRole,
  type AgentSettings,
  getAgentSettings,
  updateAgentSettings,
} from "./agent-settings"
import type { DaemonConnection } from "./daemon"
import { listProjects, type Project } from "./projects"

const roles: AgentRole[] = ["PLANNER", "EXECUTOR", "REVIEWER"]

export function AgentSettingsScreen({ connection }: { connection: DaemonConnection }) {
  const [projects, setProjects] = useState<Project[]>([])
  const [projectIndex, setProjectIndex] = useState(0)
  const [roleIndex, setRoleIndex] = useState(0)
  const [settings, setSettings] = useState<AgentSettings | null>(null)
  const [message, setMessage] = useState("")
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  const selectedProject = projects[projectIndex] ?? null
  const selectedRole = roles[roleIndex] ?? "PLANNER"
  const selectedAgent = settings?.agents.find((agent) => agent.role === selectedRole) ?? null
  const selectedPolicy =
    settings?.tool_policies.find((policy) => policy.role === selectedRole) ?? null

  const loadProjects = async () => {
    if (connection.state !== "connected") {
      setProjects([])
      setSettings(null)
      setMessage("Start the daemon before loading agent settings.")
      return
    }
    setLoading(true)
    setMessage("")
    try {
      const items = await listProjects()
      setProjects(items)
      setProjectIndex((current) => Math.min(current, Math.max(items.length - 1, 0)))
      if (items.length === 0) {
        setSettings(null)
        setMessage("Create a project before configuring agents.")
      }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to load projects")
    } finally {
      setLoading(false)
    }
  }

  const saveSettings = async () => {
    if (!settings || saving) {
      return
    }
    setSaving(true)
    setMessage("Saving versioned agent settings...")
    try {
      const updated = await updateAgentSettings(settings)
      setSettings(updated)
      setMessage(`Agent settings saved as version ${updated.version}.`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to save agent settings")
    } finally {
      setSaving(false)
    }
  }

  const mutateSettings = (mutate: (draft: AgentSettings) => void) => {
    setSettings((current) => {
      if (!current) {
        return current
      }
      const draft = cloneAgentSettings(current)
      mutate(draft)
      return draft
    })
    setMessage("Unsaved changes. Press s to persist them.")
  }

  useKeyboard((key) => {
    if (loading || saving) {
      return
    }
    if (key.name === "r") {
      void loadProjects()
      return
    }
    if ((key.name === "down" || key.name === "j") && projects.length > 0) {
      setProjectIndex((current) => Math.min(current + 1, projects.length - 1))
      return
    }
    if ((key.name === "up" || key.name === "k") && projects.length > 0) {
      setProjectIndex((current) => Math.max(current - 1, 0))
      return
    }
    if (key.name === "tab") {
      setRoleIndex((current) =>
        key.shift ? (current - 1 + roles.length) % roles.length : (current + 1) % roles.length,
      )
      return
    }
    if (!settings || !selectedAgent || !selectedPolicy) {
      return
    }
    if (key.name === "e") {
      mutateSettings((draft) => {
        const agent = draft.agents.find((item) => item.role === selectedRole)
        if (agent) {
          agent.enabled = !agent.enabled
        }
      })
      return
    }
    if (key.name === "n" && selectedRole === "EXECUTOR") {
      mutateSettings((draft) => {
        const policy = draft.tool_policies.find((item) => item.role === selectedRole)
        if (!policy) {
          return
        }
        policy.network_access =
          policy.network_access === "DISABLED"
            ? "LOOPBACK"
            : policy.network_access === "LOOPBACK"
              ? "OUTBOUND"
              : "DISABLED"
      })
      return
    }
    if (key.name === "f" && selectedRole === "EXECUTOR") {
      mutateSettings((draft) => {
        const policy = draft.tool_policies.find((item) => item.role === selectedRole)
        if (policy) {
          policy.filesystem_access =
            policy.filesystem_access === "READ_ONLY" ? "WORKSPACE_WRITE" : "READ_ONLY"
        }
      })
      return
    }
    if (key.name === "s") {
      void saveSettings()
    }
  })

  useEffect(() => {
    void loadProjects()
  }, [connection.state])

  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected" || !selectedProject) {
      setSettings(null)
      return
    }
    setLoading(true)
    setMessage("")
    void getAgentSettings(selectedProject.id)
      .then((nextSettings) => {
        if (!disposed) {
          setSettings(nextSettings)
        }
      })
      .catch((error) => {
        if (!disposed) {
          setSettings(null)
          setMessage(error instanceof Error ? error.message : "Failed to load agent settings")
        }
      })
      .finally(() => {
        if (!disposed) {
          setLoading(false)
        }
      })
    return () => {
      disposed = true
    }
  }, [connection.state, selectedProject?.id])

  return (
    <box flexDirection="row" gap={1} flexGrow={1}>
      <box width={28} flexDirection="column" borderStyle="rounded" borderColor="#334155" padding={1}>
        <text fg="#E2E8F0">Projects</text>
        {projects.length === 0 ? <text fg="#94A3B8">No projects available.</text> : null}
        {projects.map((project, index) => (
          <text key={project.id} fg={index === projectIndex ? "#7DD3FC" : "#94A3B8"}>
            {index === projectIndex ? `› ${project.name}` : `  ${project.name}`}
          </text>
        ))}
      </box>

      <box flexGrow={1} flexDirection="column" gap={1}>
        <text fg="#E2E8F0">
          {selectedProject ? `${selectedProject.name} • settings v${settings?.version ?? "-"}` : "Agent settings"}
        </text>
        <box flexDirection="row" gap={2}>
          {roles.map((role, index) => (
            <text key={role} fg={index === roleIndex ? "#7DD3FC" : "#64748B"}>
              {index === roleIndex ? `[${role}]` : role}
            </text>
          ))}
        </box>

        {loading ? <text fg="#FACC15">Loading persisted agent settings...</text> : null}
        {selectedAgent && selectedPolicy ? (
          <box flexDirection="column" gap={1}>
            <text fg={selectedAgent.enabled ? "#4ADE80" : "#F87171"}>
              {`Enabled: ${selectedAgent.enabled ? "yes" : "no"}`}
            </text>
            <text fg="#94A3B8">
              {`Provider: ${selectedAgent.provider || "daemon default"} • model: ${selectedAgent.model || "daemon default"}`}
            </text>
            <text fg="#94A3B8">
              {`Temperature: ${selectedAgent.temperature} • max output: ${selectedAgent.max_output_tokens.toLocaleString("en-US")} tokens`}
            </text>
            <text fg={selectedPolicy.network_access === "DISABLED" ? "#4ADE80" : "#FACC15"}>
              {`Network: ${selectedPolicy.network_access}`}
            </text>
            <text fg="#94A3B8">{`Filesystem: ${selectedPolicy.filesystem_access}`}</text>
            <text fg="#94A3B8">
              {`Tools: ${selectedPolicy.allowed_tools.length > 0 ? selectedPolicy.allowed_tools.join(", ") : "none"}`}
            </text>
            <text fg="#94A3B8">
              {`Command profiles: ${selectedPolicy.allowed_command_profiles.length > 0 ? selectedPolicy.allowed_command_profiles.join(", ") : "none"}`}
            </text>
            <text fg="#64748B">
              {`Limits: ${formatBytes(selectedPolicy.max_file_bytes)} file • ${formatBytes(selectedPolicy.max_patch_bytes)} patch • ${formatDuration(selectedPolicy.command_timeout_ms)} command`}
            </text>
          </box>
        ) : null}

        {message ? <text fg="#FACC15">{message}</text> : null}
        <text fg="#64748B">
          Tab role • e enabled • n executor network • f executor filesystem • s save • r reload
        </text>
        <text fg="#64748B">
          Provider, model, tools, command profiles, and numeric limits remain fully editable through the local API.
        </text>
      </box>
    </box>
  )
}

function formatBytes(value: number): string {
  if (value >= 1024 * 1024 && value % (1024 * 1024) === 0) {
    return `${value / (1024 * 1024)} MiB`
  }
  if (value >= 1024 && value % 1024 === 0) {
    return `${value / 1024} KiB`
  }
  return `${value} B`
}

function formatDuration(milliseconds: number): string {
  if (milliseconds >= 60000 && milliseconds % 60000 === 0) {
    return `${milliseconds / 60000}m`
  }
  if (milliseconds >= 1000 && milliseconds % 1000 === 0) {
    return `${milliseconds / 1000}s`
  }
  return `${milliseconds}ms`
}
