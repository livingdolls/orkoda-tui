/** @jsxImportSource @opentui/react */

import type { ScrollBoxRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useCallback, useEffect, useRef, useState } from "react"

import {
  type AgentRole,
  type AgentSettings,
  cloneAgentSettings,
  getAgentSettings,
  updateAgentSettings,
} from "./agent-settings"
import type { DaemonConnection } from "./daemon"
import { listProjects, type Project } from "./projects"
import { BOLD, Card, Chip, colors, Info, KeyHints, PageHeader, Section, truncate } from "./ui"

const roles: AgentRole[] = ["PLANNER", "EXECUTOR", "REVIEWER"]

const roleDescriptions: Record<AgentRole, string> = {
  PLANNER: "Turns your requirement into a step-by-step plan.",
  EXECUTOR: "Implements the plan inside an isolated workspace.",
  REVIEWER: "Checks the changes before you are asked to approve.",
}

export function AgentSettingsScreen({ connection }: { connection: DaemonConnection }) {
  const [projects, setProjects] = useState<Project[]>([])
  const [projectIndex, setProjectIndex] = useState(0)
  const [roleIndex, setRoleIndex] = useState(0)
  const [settings, setSettings] = useState<AgentSettings | null>(null)
  const [message, setMessage] = useState("")
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const detailScrollRef = useRef<ScrollBoxRenderable>(null)

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
    if (key.name === "pageup") {
      detailScrollRef.current?.scrollBy(-1, "viewport")
      return
    }
    if (key.name === "pagedown") {
      detailScrollRef.current?.scrollBy(1, "viewport")
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
      <PageHeader
        title={selectedProject ? `Agents · ${selectedProject.name}` : "Agents"}
        description="Each project has three AI roles. Tune them while keeping the safety limits that make every run reviewable."
        meta={settings ? `settings v${settings.version}` : "no project selected"}
      />
      <box flexDirection="row" gap={1} flexGrow={1}>
        <box
          width={26}
          flexDirection="column"
          backgroundColor={colors.raised}
          borderStyle="rounded"
          borderColor={colors.line}
        >
          <scrollbox flexGrow={1} scrollY={true} padding={1}>
            <box flexDirection="column">
              {projects.length === 0 ? <text fg={colors.faint}>No projects available.</text> : null}
              {projects.map((project, index) => {
                const selected = index === projectIndex
                return (
                  <box
                    key={project.id}
                    paddingLeft={1}
                    paddingRight={1}
                    backgroundColor={selected ? colors.accentTint : colors.raised}
                  >
                    <text
                      fg={selected ? colors.text : colors.muted}
                      attributes={selected ? BOLD : 0}
                    >
                      {`${selected ? "▸" : " "} ${truncate(project.name, 19)}`}
                    </text>
                  </box>
                )
              })}
            </box>
          </scrollbox>
        </box>

        <scrollbox ref={detailScrollRef} flexGrow={1} scrollY={true}>
          <box flexDirection="column" gap={1}>
            <Section title="Role" action="Tab switches role">
              <Card>
                <box flexDirection="row" gap={1}>
                  {roles.map((role, index) => (
                    <Chip
                      key={role}
                      label={role.toLowerCase()}
                      tone={index === roleIndex ? "accent" : "neutral"}
                      dot={false}
                    />
                  ))}
                </box>
                <text fg={colors.muted}>{roleDescriptions[selectedRole]}</text>
                {loading ? <text fg={colors.warning}>Loading agent settings...</text> : null}
              </Card>
            </Section>

            {selectedAgent && selectedPolicy ? (
              <Section title={`${selectedRole} profile`}>
                <Card>
                  <box flexDirection="row" gap={1} alignItems="center">
                    <Chip
                      label={selectedAgent.enabled ? "enabled" : "disabled"}
                      tone={selectedAgent.enabled ? "success" : "danger"}
                    />
                    <text fg={colors.faint}>press E to toggle</text>
                  </box>
                  <box flexDirection="column" gap={0}>
                    <Info
                      label="Provider"
                      value={selectedAgent.provider || "daemon default"}
                      tone="accent"
                    />
                    <Info label="Model" value={selectedAgent.model || "daemon default"} />
                    <Info label="Temperature" value={String(selectedAgent.temperature)} />
                    <Info
                      label="Max output"
                      value={`${selectedAgent.max_output_tokens.toLocaleString("en-US")} tokens`}
                    />
                  </box>
                  <box flexDirection="row" gap={1} flexWrap="wrap">
                    <Chip
                      label={`network: ${selectedPolicy.network_access.toLowerCase()}`}
                      tone={selectedPolicy.network_access === "DISABLED" ? "success" : "warning"}
                    />
                    <Chip
                      label={`filesystem: ${selectedPolicy.filesystem_access.toLowerCase()}`}
                      tone="accent"
                    />
                    {selectedRole === "EXECUTOR" ? (
                      <text fg={colors.faint}>N cycles network · F cycles filesystem</text>
                    ) : null}
                  </box>
                  <text fg={colors.muted}>
                    {`Tools: ${selectedPolicy.allowed_tools.length > 0 ? selectedPolicy.allowed_tools.join(", ") : "none"}`}
                  </text>
                  <text fg={colors.muted}>
                    {`Command profiles: ${selectedPolicy.allowed_command_profiles.length > 0 ? selectedPolicy.allowed_command_profiles.join(", ") : "none"}`}
                  </text>
                  <text fg={colors.faint}>
                    {`Limits: ${formatBytes(selectedPolicy.max_file_bytes)} file · ${formatBytes(selectedPolicy.max_patch_bytes)} patch · ${formatDuration(selectedPolicy.command_timeout_ms)} command`}
                  </text>
                </Card>
              </Section>
            ) : null}

            {message ? <text fg={colors.warning}>{message}</text> : null}
            <text fg={colors.faint}>
              Provider, model, tools, command profiles, and numeric limits remain editable through
              the local API.
            </text>
          </box>
        </scrollbox>
      </box>
      <KeyHints
        shortcuts={[
          { key: "↑↓", label: "project" },
          { key: "Tab", label: "role" },
          { key: "E", label: "enable / disable" },
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
