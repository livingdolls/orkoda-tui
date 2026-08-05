/** @jsxImportSource @opentui/react */

import type { ScrollBoxRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useCallback, useEffect, useRef, useState } from "react"

import { AgentProfileEditor } from "./agent-profile-editor"
import {
  type AgentConfig,
  type AgentRole,
  type AgentSettings,
  cloneAgentSettings,
  getAgentSettings,
  updateAgentSettings,
  validateDistinctAgentPair,
} from "./agent-settings"
import type { DaemonConnection } from "./daemon"
import { type LLMProviderInfo, listLLMProviders } from "./llm-providers"
import { listProjects, type Project } from "./projects"
import {
  Banner,
  BOLD,
  Card,
  Chip,
  colors,
  Info,
  KeyHints,
  PageHeader,
  Section,
  truncate,
} from "./ui"

const roles: AgentRole[] = ["PLANNER", "EXECUTOR", "REVIEWER"]
const roleDescriptions: Record<AgentRole, string> = {
  PLANNER: "Turns your requirement into a step-by-step plan.",
  EXECUTOR: "Implements the plan inside an isolated workspace.",
  REVIEWER: "Independently checks immutable changes before human approval.",
}

export function AgentSettingsScreen({ connection }: { connection: DaemonConnection }) {
  const [projects, setProjects] = useState<Project[]>([])
  const [providers, setProviders] = useState<LLMProviderInfo[]>([])
  const [projectIndex, setProjectIndex] = useState(0)
  const [roleIndex, setRoleIndex] = useState(0)
  const [settings, setSettings] = useState<AgentSettings | null>(null)
  const [editorAgent, setEditorAgent] = useState<AgentConfig | null>(null)
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
  const separationError = settings ? validateDistinctAgentPair(settings) : undefined

  const loadProjects = useCallback(async () => {
    if (connection.state !== "connected") {
      setProjects([])
      setSettings(null)
      setProviders([])
      setMessage("Start the daemon before loading agent settings.")
      return
    }
    setLoading(true)
    setMessage("")
    try {
      const [items, providerItems] = await Promise.all([listProjects(), listLLMProviders()])
      setProjects(items)
      setProviders(providerItems)
      setProjectIndex((current) => Math.min(current, Math.max(items.length - 1, 0)))
      if (items.length === 0) setMessage("Create a project before configuring agents.")
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to load projects and providers")
    } finally {
      setLoading(false)
    }
  }, [connection.state])

  const mutateSettings = (mutate: (draft: AgentSettings) => void) => {
    setSettings((current) => {
      if (!current) return current
      const draft = cloneAgentSettings(current)
      mutate(draft)
      return draft
    })
    setMessage("Unsaved changes. Press S to persist them.")
  }

  const saveSettings = async () => {
    if (!settings || saving) return
    const problem = validateDistinctAgentPair(settings)
    if (problem) {
      setMessage(problem)
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

  useKeyboard((key) => {
    if (editorAgent || loading || saving) return
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
    if ((key.name === "down" || key.name === "j") && projects.length) {
      setProjectIndex((current) => Math.min(current + 1, projects.length - 1))
      return
    }
    if ((key.name === "up" || key.name === "k") && projects.length) {
      setProjectIndex((current) => Math.max(current - 1, 0))
      return
    }
    if (key.name === "tab") {
      setRoleIndex((current) =>
        key.shift ? (current - 1 + roles.length) % roles.length : (current + 1) % roles.length,
      )
      return
    }
    if (!settings || !selectedAgent || !selectedPolicy) return
    if (key.name === "return" || key.name === "enter") {
      setEditorAgent({ ...selectedAgent })
      return
    }
    if (key.name === "e") {
      mutateSettings((draft) => {
        const agent = draft.agents.find((item) => item.role === selectedRole)
        if (agent) agent.enabled = !agent.enabled
      })
      return
    }
    if (key.name === "n" && selectedRole === "EXECUTOR") {
      mutateSettings((draft) => {
        const policy = draft.tool_policies.find((item) => item.role === selectedRole)
        if (policy)
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
        if (policy)
          policy.filesystem_access =
            policy.filesystem_access === "READ_ONLY" ? "WORKSPACE_WRITE" : "READ_ONLY"
      })
      return
    }
    if (key.name === "s") void saveSettings()
  })

  useEffect(() => {
    void loadProjects()
  }, [loadProjects])
  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected" || !selectedProjectID) {
      setSettings(null)
      return
    }
    setLoading(true)
    setMessage("")
    void getAgentSettings(selectedProjectID)
      .then((next) => {
        if (!disposed) setSettings(next)
      })
      .catch((error) => {
        if (!disposed) {
          setSettings(null)
          setMessage(error instanceof Error ? error.message : "Failed to load agent settings")
        }
      })
      .finally(() => {
        if (!disposed) setLoading(false)
      })
    return () => {
      disposed = true
    }
  }, [connection.state, selectedProjectID])

  if (editorAgent && settings) {
    const peerRole =
      editorAgent.role === "EXECUTOR"
        ? "REVIEWER"
        : editorAgent.role === "REVIEWER"
          ? "EXECUTOR"
          : undefined
    return (
      <AgentProfileEditor
        agent={editorAgent}
        peer={peerRole ? settings.agents.find((agent) => agent.role === peerRole) : undefined}
        providers={providers}
        onCancel={() => setEditorAgent(null)}
        onApply={(updated) => {
          mutateSettings((draft) => {
            const index = draft.agents.findIndex((agent) => agent.role === updated.role)
            if (index >= 0) draft.agents[index] = updated
          })
          setEditorAgent(null)
        }}
      />
    )
  }

  const registered = selectedAgent?.provider
    ? providers.some((provider) => provider.name === selectedAgent.provider)
    : true
  return (
    <box flexDirection="column" gap={1} flexGrow={1}>
      <PageHeader
        title={selectedProject ? `Agents · ${selectedProject.name}` : "Agents"}
        description="Assign independent providers and models to Planner, Executor, and Reviewer while preserving role safety."
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
              {projects.map((project, index) => (
                <box
                  key={project.id}
                  paddingLeft={1}
                  paddingRight={1}
                  backgroundColor={index === projectIndex ? colors.accentTint : colors.raised}
                >
                  <text
                    fg={index === projectIndex ? colors.text : colors.muted}
                    attributes={index === projectIndex ? BOLD : 0}
                  >{`${index === projectIndex ? "▸" : " "} ${truncate(project.name, 19)}`}</text>
                </box>
              ))}
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
              <Section title={`${selectedRole} profile`} action="Enter edits provider/model">
                <Card>
                  <box flexDirection="row" gap={1}>
                    <Chip
                      label={selectedAgent.enabled ? "enabled" : "disabled"}
                      tone={selectedAgent.enabled ? "success" : "danger"}
                    />
                    <Chip
                      label={registered ? "provider registered" : "provider missing"}
                      tone={registered ? "success" : "danger"}
                    />
                  </box>
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
                  <box flexDirection="row" gap={1} flexWrap="wrap">
                    <Chip
                      label={`network: ${selectedPolicy.network_access.toLowerCase()}`}
                      tone={selectedPolicy.network_access === "DISABLED" ? "success" : "warning"}
                    />
                    <Chip
                      label={`filesystem: ${selectedPolicy.filesystem_access.toLowerCase()}`}
                      tone={selectedPolicy.filesystem_access === "READ_ONLY" ? "success" : "accent"}
                    />
                  </box>
                  <text
                    fg={colors.muted}
                  >{`Tools: ${selectedPolicy.allowed_tools.length ? selectedPolicy.allowed_tools.join(", ") : "none"}`}</text>
                  <text
                    fg={colors.faint}
                  >{`Limits: ${formatBytes(selectedPolicy.max_file_bytes)} file · ${formatBytes(selectedPolicy.max_patch_bytes)} patch · ${formatDuration(selectedPolicy.command_timeout_ms)} command`}</text>
                </Card>
              </Section>
            ) : null}
            <Section title="Separation policy">
              <Card tone={separationError ? "danger" : "success"}>
                <text fg={separationError ? colors.danger : colors.success}>
                  {separationError || "Executor and Reviewer assignments are independent."}
                </text>
                <text fg={colors.faint}>
                  Reviewer remains read-only and cannot receive mutation or command tools.
                </text>
              </Card>
            </Section>
            {message ? (
              <Banner
                tone={
                  message.toLowerCase().includes("failed") || message.includes("must not")
                    ? "danger"
                    : "warning"
                }
              >
                <text fg={colors.muted}>{message}</text>
              </Banner>
            ) : null}
          </box>
        </scrollbox>
      </box>
      <KeyHints
        shortcuts={[
          { key: "↑↓", label: "project" },
          { key: "Tab", label: "role" },
          { key: "Enter", label: "edit model" },
          { key: "E", label: "enable/disable" },
          { key: "S", label: "save" },
          { key: "R", label: "reload" },
        ]}
      />
    </box>
  )
}

function formatBytes(value: number): string {
  if (value >= 1024 * 1024 && value % (1024 * 1024) === 0) return `${value / (1024 * 1024)} MiB`
  if (value >= 1024 && value % 1024 === 0) return `${value / 1024} KiB`
  return `${value} B`
}
function formatDuration(milliseconds: number): string {
  if (milliseconds >= 60000 && milliseconds % 60000 === 0) return `${milliseconds / 60000}m`
  if (milliseconds >= 1000 && milliseconds % 1000 === 0) return `${milliseconds / 1000}s`
  return `${milliseconds}ms`
}
