/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useCallback, useEffect, useState } from "react"

import {
  type AgentRole,
  type AgentSettings,
  cloneAgentSettings,
  getAgentSettings,
  updateAgentSettings,
} from "./agent-settings"
import type { DaemonConnection } from "./daemon"
import { listProjects, type Project } from "./projects"
import { colors, Metric, PageIntro, Panel, ShortcutBar, StatusBadge } from "./ui"

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
  const selectedProjectID = selectedProject?.id ?? ""
  const selectedRole = roles[roleIndex] ?? "PLANNER"
  const selectedAgent = settings?.agents.find((agent) => agent.role === selectedRole) ?? null
  const selectedPolicy =
    settings?.tool_policies.find((policy) => policy.role === selectedRole) ?? null

  const loadProjects = useCallback(async () => {
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
  }, [connection.state])

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
  }, [loadProjects])

  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected" || selectedProjectID === "") {
      setSettings(null)
      return
    }
    setLoading(true)
    setMessage("")
    void getAgentSettings(selectedProjectID)
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
  }, [connection.state, selectedProjectID])

  return (
    <box flexDirection="column" gap={1} flexGrow={1}>
      <PageIntro
        kicker="AGENT CONTROL"
        title={selectedProject?.name ?? "Agent settings"}
        description="Tune each role without losing the guardrails that keep execution bounded and reviewable."
        meta={settings ? `settings v${settings.version}` : "no project selected"}
      />
      <box flexDirection="row" gap={1} flexGrow={1}>
        <Panel width={29} title={`PROJECTS  ${projects.length}`} borderColor={colors.lineStrong}>
          {projects.length === 0 ? <text fg={colors.dim}>No projects available.</text> : null}
          {projects.map((project, index) => (
            <box
              key={project.id}
              flexDirection="column"
              gap={1}
              padding={1}
              backgroundColor={index === projectIndex ? colors.surfaceAccent : colors.surface}
              borderStyle="rounded"
              borderColor={index === projectIndex ? colors.accent : colors.line}
            >
              <text fg={index === projectIndex ? colors.accent : colors.text}>
                {`${index === projectIndex ? "›" : " "} ${project.name}`}
              </text>
            </box>
          ))}
        </Panel>

        <box flexGrow={1} flexDirection="column" gap={1}>
          <Panel title="ROLE" borderColor={colors.accent} backgroundColor={colors.surfaceAccent}>
            <box flexDirection="row" gap={1}>
              {roles.map((role, index) => (
                <StatusBadge
                  key={role}
                  label={role}
                  tone={index === roleIndex ? "accent" : "neutral"}
                />
              ))}
            </box>
            {loading ? <text fg={colors.warning}>Loading persisted agent settings...</text> : null}
          </Panel>

          {selectedAgent && selectedPolicy ? (
            <Panel
              title={`${selectedRole} PROFILE`}
              borderColor={selectedAgent.enabled ? colors.success : colors.danger}
            >
              <box flexDirection="row" gap={1}>
                <Metric
                  label="Enabled"
                  value={selectedAgent.enabled ? "YES" : "NO"}
                  tone={selectedAgent.enabled ? "success" : "danger"}
                />
                <Metric
                  label="Provider"
                  value={selectedAgent.provider || "daemon default"}
                  tone="accent"
                />
                <Metric label="Model" value={selectedAgent.model || "daemon default"} />
              </box>
              <box flexDirection="row" gap={1}>
                <Metric label="Temperature" value={String(selectedAgent.temperature)} />
                <Metric
                  label="Max output"
                  value={`${selectedAgent.max_output_tokens.toLocaleString("en-US")} tokens`}
                />
              </box>
              <box flexDirection="row" gap={1}>
                <StatusBadge
                  label={`Network ${selectedPolicy.network_access}`}
                  tone={selectedPolicy.network_access === "DISABLED" ? "success" : "warning"}
                />
                <StatusBadge
                  label={`Filesystem ${selectedPolicy.filesystem_access}`}
                  tone="accent"
                />
              </box>
              <text
                fg={colors.muted}
              >{`Tools: ${selectedPolicy.allowed_tools.length > 0 ? selectedPolicy.allowed_tools.join(", ") : "none"}`}</text>
              <text
                fg={colors.muted}
              >{`Command profiles: ${selectedPolicy.allowed_command_profiles.length > 0 ? selectedPolicy.allowed_command_profiles.join(", ") : "none"}`}</text>
              <text
                fg={colors.dim}
              >{`Limits: ${formatBytes(selectedPolicy.max_file_bytes)} file · ${formatBytes(selectedPolicy.max_patch_bytes)} patch · ${formatDuration(selectedPolicy.command_timeout_ms)} command`}</text>
            </Panel>
          ) : null}

          {message ? <text fg={colors.warning}>{message}</text> : null}
          <text fg={colors.dim}>
            Provider, model, tools, command profiles, and numeric limits remain editable through the
            local API.
          </text>
        </box>
      </box>
      <ShortcutBar
        shortcuts={[
          { key: "↑↓", label: "project" },
          { key: "Tab", label: "role" },
          { key: "E", label: "toggle" },
          { key: "N", label: "network" },
          { key: "F", label: "filesystem" },
          { key: "S", label: "save" },
          { key: "R", label: "reload" },
        ]}
      />
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
