/** @jsxImportSource @opentui/react */

import type { ScrollBoxRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useCallback, useEffect, useRef, useState } from "react"

import type { DaemonConnection } from "./daemon"
import { daemonBaseURL } from "./daemon"
import {
  deleteLLMProvider,
  getLLMPolicy,
  type LLMPolicyInfo,
  type LLMProviderInfo,
  listLLMProviders,
  testLLMProvider,
} from "./llm-providers"
import { ProviderEditor } from "./provider-editor"
import { Banner, BOLD, Card, Chip, colors, Info, KeyHints, Metric, PageHeader, Section } from "./ui"

type SettingsMode = "list" | "editor" | "delete"

export function SettingsScreen({
  connection,
  onInteractionChange,
}: {
  connection: DaemonConnection
  onInteractionChange?: (active: boolean) => void
}) {
  const [providers, setProviders] = useState<LLMProviderInfo[]>([])
  const [policy, setPolicy] = useState<LLMPolicyInfo | null>(null)
  const [message, setMessage] = useState("")
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [mode, setMode] = useState<SettingsMode>("list")
  const [editingProvider, setEditingProvider] = useState<LLMProviderInfo | undefined>()
  const contentRef = useRef<ScrollBoxRenderable>(null)
  const selected = providers[Math.min(selectedIndex, Math.max(providers.length - 1, 0))]

  const reload = useCallback(async () => {
    if (connection.state !== "connected") {
      setProviders([])
      setPolicy(null)
      setMessage("Start the daemon to configure AI providers.")
      return
    }
    setLoading(true)
    setMessage("")
    try {
      const [items, nextPolicy] = await Promise.all([listLLMProviders(), getLLMPolicy()])
      setProviders(items)
      setPolicy(nextPolicy)
      setSelectedIndex((current) => Math.min(current, Math.max(items.length - 1, 0)))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to load LLM configuration")
    } finally {
      setLoading(false)
    }
  }, [connection.state])

  useEffect(() => {
    void reload()
  }, [reload])

  useEffect(() => {
    onInteractionChange?.(mode !== "list" || busy)
    return () => onInteractionChange?.(false)
  }, [mode, busy, onInteractionChange])

  const testSelected = async () => {
    if (!selected || busy) return
    setBusy(true)
    setMessage(`Testing ${selected.name}/${selected.default_model}...`)
    try {
      const result = await testLLMProvider(selected.name)
      setMessage(
        `${result.provider}/${result.model} connected in ${result.latency_ms} ms · ${result.response_preview || "response received"}`,
      )
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Provider test failed")
    } finally {
      setBusy(false)
    }
  }

  const deleteSelected = async () => {
    if (!selected?.deletable || busy) return
    setBusy(true)
    setMessage(`Removing ${selected.name} and its stored credential...`)
    try {
      await deleteLLMProvider(selected.name)
      setMode("list")
      setMessage(`${selected.name} removed. Existing workflow snapshots remain unchanged.`)
      await reload()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Provider deletion failed")
    } finally {
      setBusy(false)
    }
  }

  useKeyboard((key) => {
    if (mode === "editor" || busy) return
    if (mode === "delete") {
      if (key.name === "escape") setMode("list")
      if (key.name === "return" || key.name === "enter") void deleteSelected()
      return
    }
    if (key.name === "pageup") contentRef.current?.scrollBy(-1, "viewport")
    if (key.name === "pagedown") contentRef.current?.scrollBy(1, "viewport")
    if (key.name === "up" || key.name === "k") {
      setSelectedIndex((current) => Math.max(0, current - 1))
      return
    }
    if (key.name === "down" || key.name === "j") {
      setSelectedIndex((current) => Math.min(Math.max(providers.length - 1, 0), current + 1))
      return
    }
    if (key.name === "n") {
      setEditingProvider(undefined)
      setMode("editor")
      setMessage("")
      return
    }
    if ((key.name === "return" || key.name === "enter" || key.name === "e") && selected?.editable) {
      setEditingProvider(selected)
      setMode("editor")
      setMessage("")
      return
    }
    if (key.name === "t") {
      void testSelected()
      return
    }
    if (key.name === "d" && selected?.deletable) {
      setMode("delete")
      return
    }
    if (key.name === "r") void reload()
  })

  if (mode === "editor") {
    return (
      <ProviderEditor
        provider={editingProvider}
        onSaved={(provider) => {
          setMode("list")
          setEditingProvider(undefined)
          setMessage(
            `${provider.name} saved and activated. Assign it to Planner, Executor, or Reviewer in Agents.`,
          )
          void reload()
        }}
        onCancel={() => {
          setMode("list")
          setEditingProvider(undefined)
        }}
      />
    )
  }

  const fallbackLabel =
    policy && policy.fallbacks.length > 0
      ? policy.fallbacks.map((fallback) => `${fallback.provider}/${fallback.model}`).join(", ")
      : "none"

  return (
    <box flexDirection="column" gap={1} flexGrow={1}>
      <PageHeader
        title="Settings"
        description="Add AI providers here, test their connection, then assign each provider and model to an agent role."
        meta={`${providers.length} provider(s) · ${busy ? "working" : "ready"}`}
      />
      <scrollbox ref={contentRef} flexGrow={1} scrollY={true}>
        <box flexDirection="column" gap={1}>
          <Section title="AI providers">
            <Card>
              {loading ? <text fg={colors.warning}>Loading LLM configuration...</text> : null}
              {!loading && providers.length === 0 ? (
                <text fg={colors.faint}>
                  No provider metadata is available. Press N to add one.
                </text>
              ) : null}
              {providers.map((provider, index) => {
                const isSelected = index === selectedIndex
                return (
                  <box
                    key={provider.name}
                    flexDirection="column"
                    gap={1}
                    padding={1}
                    borderStyle="rounded"
                    borderColor={isSelected ? colors.accent : colors.line}
                    backgroundColor={isSelected ? colors.accentTint : colors.raised}
                  >
                    <box flexDirection="row" justifyContent="space-between" gap={1}>
                      <box flexDirection="row" gap={1}>
                        <text fg={isSelected ? colors.accent : colors.faint}>
                          {isSelected ? "▸" : " "}
                        </text>
                        <text fg={colors.text} attributes={BOLD}>
                          {provider.name}
                        </text>
                        {provider.default ? (
                          <Chip label="daemon default" tone="accent" dot={false} />
                        ) : null}
                        <Chip label={provider.source ?? "runtime"} dot={false} />
                      </box>
                      <box flexDirection="row" gap={1}>
                        <Chip
                          label={
                            provider.credential_stored ? "credential stored" : "credential missing"
                          }
                          tone={provider.credential_stored ? "success" : "danger"}
                        />
                        <Chip
                          label={provider.structured_output ? "structured" : "prompt only"}
                          tone={provider.structured_output ? "accent" : "warning"}
                        />
                      </box>
                    </box>
                    <text
                      fg={colors.muted}
                    >{`model: ${provider.default_model || "not configured"}`}</text>
                    {provider.base_url ? <text fg={colors.faint}>{provider.base_url}</text> : null}
                  </box>
                )
              })}
            </Card>
          </Section>

          <Section title="How assignment works">
            <Card tone="accent">
              <text fg={colors.muted} wrapMode="word">
                Saving a provider activates it immediately. Open Agents afterward and choose a
                provider/model independently for Planner, Executor, and Reviewer.
              </text>
              <Info label="Daemon" value={daemonBaseURL} tone="accent" />
              <Info
                label="Credential storage"
                value="OS keychain, with protected local fallback"
                tone="success"
              />
            </Card>
          </Section>

          {policy ? (
            <Section title="Execution policy">
              <Card>
                <box flexDirection="row" gap={1}>
                  <Metric label="Retries" value={String(policy.max_attempts)} />
                  <Metric
                    label="Timeout per try"
                    value={formatDuration(policy.attempt_timeout_ms)}
                  />
                  <Metric
                    label="Total time limit"
                    value={formatDuration(policy.max_wall_clock_ms)}
                  />
                </box>
                <box flexDirection="row" gap={1}>
                  <Metric
                    label="Input budget"
                    value={formatNumber(policy.budget.max_input_tokens)}
                  />
                  <Metric
                    label="Output budget"
                    value={formatNumber(policy.budget.max_output_tokens)}
                  />
                  <Metric
                    label="Total budget"
                    value={formatNumber(policy.budget.max_total_tokens)}
                  />
                </box>
                <text fg={colors.faint}>
                  {`Backoff ${formatDuration(policy.initial_backoff_ms)} → ${formatDuration(policy.max_backoff_ms)} · jitter ${Math.round(policy.jitter * 100)}% · fallbacks ${fallbackLabel}`}
                </text>
                <box flexDirection="row" gap={1} flexWrap="wrap">
                  <Chip
                    label={`secret redaction: ${policy.redaction_mode}`}
                    tone={policy.redaction_mode === "strict" ? "success" : "warning"}
                  />
                  <Chip
                    label={`schema validation: ${policy.structured_validation ? "on" : "off"}`}
                    tone={policy.structured_validation ? "success" : "warning"}
                  />
                  <Chip label={`repair attempts: ${policy.max_repair_attempts}`} tone="accent" />
                </box>
                <text
                  fg={colors.faint}
                >{`Maximum structured response: ${formatBytes(policy.max_structured_response_bytes)}`}</text>
              </Card>
            </Section>
          ) : null}

          {mode === "delete" && selected ? (
            <Banner tone="danger">
              <text fg={colors.danger} attributes={BOLD}>{`Remove ${selected.name}?`}</text>
              <text fg={colors.muted} wrapMode="word">
                This removes the TUI provider configuration and stored credential. Existing
                execution and review snapshots are preserved. Press Enter to confirm or Esc to
                cancel.
              </text>
            </Banner>
          ) : null}
          {message ? (
            <Banner
              tone={
                message.toLowerCase().includes("failed") || message.toLowerCase().includes("error")
                  ? "danger"
                  : "accent"
              }
            >
              <text fg={colors.muted} wrapMode="word">
                {message}
              </text>
            </Banner>
          ) : null}
        </box>
      </scrollbox>
      <KeyHints
        shortcuts={[
          { key: "↑↓", label: "provider" },
          { key: "N", label: "add" },
          { key: "Enter", label: selected?.editable ? "edit" : "view only" },
          { key: "T", label: "test connection" },
          { key: "D", label: selected?.deletable ? "delete" : "not removable" },
          { key: "R", label: "refresh" },
        ]}
      />
    </box>
  )
}

function formatDuration(milliseconds: number): string {
  if (milliseconds >= 60000 && milliseconds % 60000 === 0) return `${milliseconds / 60000}m`
  if (milliseconds >= 1000 && milliseconds % 1000 === 0) return `${milliseconds / 1000}s`
  return `${milliseconds}ms`
}

function formatNumber(value: number): string {
  return value === 0 ? "unlimited" : value.toLocaleString("en-US")
}

function formatBytes(value: number): string {
  if (value >= 1024 * 1024 && value % (1024 * 1024) === 0) return `${value / (1024 * 1024)} MiB`
  if (value >= 1024 && value % 1024 === 0) return `${value / 1024} KiB`
  return `${value} bytes`
}
