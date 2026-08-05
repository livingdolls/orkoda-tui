/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useCallback, useEffect, useState } from "react"

import { getAgentSettings } from "./agent-settings"
import { listLLMProviders } from "./llm-providers"
import { Banner, BOLD, Card, Chip, colors, KeyHints, PageHeader, Section } from "./ui"
import {
  buildWorkflowAgentSelectionState,
  cycleChoice,
  validateWorkflowAgentAssignment,
  type WorkflowAgentAssignment,
} from "./workflow-agent-selection"
import type { WorkflowAgentSelection } from "./workflow-jobs"

export type { WorkflowAgentAssignment } from "./workflow-agent-selection"

type ActiveRole = "executor" | "reviewer"

export function WorkflowAgentPicker({
  projectID,
  projectName,
  planTitle,
  baseBranch,
  onConfirm,
  onCancel,
}: {
  projectID: string
  projectName: string
  planTitle: string
  baseBranch: string
  onConfirm: (assignment: WorkflowAgentAssignment) => Promise<void>
  onCancel: () => void
}) {
  const [activeRole, setActiveRole] = useState<ActiveRole>("executor")
  const [choices, setChoices] = useState<WorkflowAgentSelection[]>([])
  const [executorIndex, setExecutorIndex] = useState(0)
  const [reviewerIndex, setReviewerIndex] = useState(0)
  const [settingsVersion, setSettingsVersion] = useState(0)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState("")

  const load = useCallback(async () => {
    setLoading(true)
    setMessage("")
    try {
      const [settings, providers] = await Promise.all([
        getAgentSettings(projectID),
        listLLMProviders(),
      ])
      const state = buildWorkflowAgentSelectionState(settings, providers)
      setChoices(state.choices)
      setExecutorIndex(state.executorIndex)
      setReviewerIndex(state.reviewerIndex)
      setSettingsVersion(settings.version)
      if (state.choices.length < 2) {
        setMessage(
          "Add another provider/model in Settings or Agents so Executor and Reviewer can be separated.",
        )
      }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to load workflow agents")
    } finally {
      setLoading(false)
    }
  }, [projectID])

  useEffect(() => {
    void load()
  }, [load])

  const executor = choices[executorIndex]
  const reviewer = choices[reviewerIndex]

  const moveChoice = (delta: number) => {
    if (busy || loading || choices.length === 0) return
    if (activeRole === "executor") {
      setExecutorIndex((current) => cycleChoice(current, delta, choices.length))
    } else {
      setReviewerIndex((current) => cycleChoice(current, delta, choices.length))
    }
    setMessage("")
  }

  const confirm = async () => {
    if (busy || loading) return
    const validation = validateWorkflowAgentAssignment(executor, reviewer)
    if (validation) {
      setMessage(validation)
      return
    }
    setBusy(true)
    setMessage("Creating workflow with the selected agents...")
    try {
      await onConfirm({
        agent_settings_version: settingsVersion,
        executor: executor as WorkflowAgentSelection,
        reviewer: reviewer as WorkflowAgentSelection,
      })
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to create workflow")
      setBusy(false)
    }
  }

  useKeyboard((key) => {
    if (key.name === "escape" && !busy) {
      onCancel()
      return
    }
    if (key.name === "tab") {
      setActiveRole((current) => (current === "executor" ? "reviewer" : "executor"))
      return
    }
    if (key.name === "left" || key.name === "h" || key.name === "up" || key.name === "k") {
      moveChoice(-1)
      return
    }
    if (key.name === "right" || key.name === "l" || key.name === "down" || key.name === "j") {
      moveChoice(1)
      return
    }
    if (key.name === "r" && !busy) {
      void load()
      return
    }
    if (key.name === "return" || key.name === "enter" || (key.ctrl && key.name === "s")) {
      void confirm()
    }
  })

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title="Choose workflow agents"
        description="These provider/model pairs are frozen for execution, revision, review, and re-review. Project defaults are used only as the starting selection."
        meta={`${projectName} · ${baseBranch}`}
      />

      <Section title={`Plan · ${planTitle}`}>
        <box flexDirection="row" gap={1}>
          <AgentChoiceCard label="Executor" active={activeRole === "executor"} choice={executor} />
          <AgentChoiceCard label="Reviewer" active={activeRole === "reviewer"} choice={reviewer} />
        </box>
      </Section>

      <Card>
        <text fg={colors.muted}>Available provider/model choices</text>
        {loading ? <text fg={colors.warning}>Loading registered providers...</text> : null}
        {!loading && choices.length === 0 ? (
          <text fg={colors.danger}>No configured provider/model choices are available.</text>
        ) : null}
        {choices.map((choice, index) => (
          <text
            key={`${choice.provider}:${choice.model}`}
            fg={index === executorIndex || index === reviewerIndex ? colors.accent : colors.faint}
          >
            {`${index + 1}. ${choice.provider}/${choice.model}`}
          </text>
        ))}
        <text
          fg={colors.faint}
        >{`Agent settings snapshot version: ${settingsVersion || "unavailable"}`}</text>
      </Card>

      <KeyHints
        shortcuts={[
          { key: "Tab", label: "switch Executor / Reviewer" },
          { key: "←→", label: "choose provider/model" },
          { key: "Enter", label: "create and start workflow" },
          { key: "R", label: "reload defaults" },
          { key: "Esc", label: "cancel" },
        ]}
      />
      {message ? (
        <Banner tone={busy || loading ? "warning" : "danger"}>
          <text fg={busy || loading ? colors.warning : colors.danger}>{message}</text>
        </Banner>
      ) : null}
    </box>
  )
}

function AgentChoiceCard({
  label,
  active,
  choice,
}: {
  label: "Executor" | "Reviewer"
  active: boolean
  choice?: WorkflowAgentSelection
}) {
  return (
    <box
      width="50%"
      flexDirection="column"
      gap={1}
      padding={1}
      borderStyle="rounded"
      borderColor={active ? colors.accent : colors.line}
      backgroundColor={active ? colors.accentTint : colors.raised}
    >
      <text fg={active ? colors.text : colors.muted} attributes={active ? BOLD : 0}>
        {label}
      </text>
      {choice ? (
        <>
          <Chip label={choice.provider} tone={active ? "accent" : "neutral"} />
          <text fg={colors.text}>{choice.model}</text>
        </>
      ) : (
        <text fg={colors.danger}>No selection</text>
      )}
      <text fg={colors.faint}>{active ? "←/→ to change" : "Tab to select"}</text>
    </box>
  )
}
