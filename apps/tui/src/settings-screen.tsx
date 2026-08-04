/** @jsxImportSource @opentui/react */

import { useEffect, useState } from "react"

import type { DaemonConnection } from "./daemon"
import { daemonBaseURL } from "./daemon"
import {
  getLLMPolicy,
  type LLMPolicyInfo,
  type LLMProviderInfo,
  listLLMProviders,
} from "./llm-providers"
import { colors, Metric, PageIntro, Panel, ShortcutBar, StatusBadge } from "./ui"

export function SettingsScreen({ connection }: { connection: DaemonConnection }) {
  const [providers, setProviders] = useState<LLMProviderInfo[]>([])
  const [policy, setPolicy] = useState<LLMPolicyInfo | null>(null)
  const [message, setMessage] = useState("")
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected") {
      setProviders([])
      setPolicy(null)
      setMessage("Start the daemon to inspect configured providers.")
      return () => {
        disposed = true
      }
    }

    setLoading(true)
    setMessage("")
    void Promise.all([listLLMProviders(), getLLMPolicy()])
      .then(([items, nextPolicy]) => {
        if (!disposed) {
          setProviders(items)
          setPolicy(nextPolicy)
        }
      })
      .catch((error) => {
        if (!disposed) {
          setMessage(error instanceof Error ? error.message : "Failed to load LLM configuration")
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
  }, [connection.state])

  const fallbackLabel =
    policy && policy.fallbacks.length > 0
      ? policy.fallbacks.map((fallback) => `${fallback.provider}/${fallback.model}`).join(", ")
      : "none"

  return (
    <box flexDirection="column" gap={1} flexGrow={1}>
      <PageIntro
        kicker="RUNTIME SETTINGS"
        title="Providers & execution policy"
        description="Inspect the local daemon contract and the safety budget applied to every agent run."
        meta="read-only view"
      />
      <Panel
        title="LOCAL DAEMON"
        borderColor={colors.accent}
        backgroundColor={colors.surfaceAccent}
      >
        <box flexDirection="row" gap={1}>
          <Metric label="Endpoint" value={daemonBaseURL} tone="accent" />
          <Metric label="Mode" value="local" />
          <Metric label="Auth" value="bearer token" tone="success" />
        </box>
        <text fg={colors.dim}>Override with ORKODA_DAEMON_URL before running the TUI.</text>
      </Panel>

      <Panel title="LLM PROVIDERS" flexGrow={1}>
        {loading ? <text fg={colors.warning}>Loading LLM configuration...</text> : null}
        {!loading && providers.length === 0 && message === "" ? (
          <text fg={colors.dim}>No provider metadata is available.</text>
        ) : null}
        {providers.map((provider) => (
          <box
            key={provider.name}
            flexDirection="row"
            justifyContent="space-between"
            gap={1}
            padding={1}
            backgroundColor={provider.default ? colors.surfaceAccent : colors.surface}
          >
            <box flexDirection="column" gap={1}>
              <text
                fg={provider.default ? colors.accent : colors.text}
              >{`${provider.default ? "★ " : "  "}${provider.name}`}</text>
              <text
                fg={colors.muted}
              >{`model: ${provider.default_model || "not configured"}`}</text>
            </box>
            <box flexDirection="row" gap={1}>
              <StatusBadge
                label={provider.configured ? "configured" : "not configured"}
                tone={provider.configured ? "success" : "danger"}
              />
              <StatusBadge
                label={provider.structured_output ? "structured" : "prompt only"}
                tone={provider.structured_output ? "accent" : "warning"}
              />
            </box>
          </box>
        ))}
      </Panel>

      {policy ? (
        <Panel title="EXECUTION POLICY">
          <box flexDirection="row" gap={1}>
            <Metric label="Attempts" value={String(policy.max_attempts)} />
            <Metric label="Attempt timeout" value={formatDuration(policy.attempt_timeout_ms)} />
            <Metric label="Wall clock" value={formatDuration(policy.max_wall_clock_ms)} />
          </box>
          <box flexDirection="row" gap={1}>
            <Metric label="Input budget" value={formatNumber(policy.budget.max_input_tokens)} />
            <Metric label="Output budget" value={formatNumber(policy.budget.max_output_tokens)} />
            <Metric label="Total budget" value={formatNumber(policy.budget.max_total_tokens)} />
          </box>
          <text
            fg={colors.muted}
          >{`Backoff ${formatDuration(policy.initial_backoff_ms)} → ${formatDuration(policy.max_backoff_ms)} · jitter ${Math.round(policy.jitter * 100)}% · fallbacks ${fallbackLabel}`}</text>
          <box flexDirection="row" gap={1}>
            <StatusBadge
              label={`redaction ${policy.redaction_mode}`}
              tone={policy.redaction_mode === "strict" ? "success" : "warning"}
            />
            <StatusBadge
              label={`schema ${policy.structured_validation ? "enabled" : "disabled"}`}
              tone={policy.structured_validation ? "success" : "warning"}
            />
            <StatusBadge label={`repair ${policy.max_repair_attempts}`} tone="accent" />
          </box>
          <text
            fg={colors.dim}
          >{`Maximum structured response: ${formatBytes(policy.max_structured_response_bytes)}`}</text>
        </Panel>
      ) : null}

      {message ? <text fg={colors.warning}>{message}</text> : null}
      <ShortcutBar
        shortcuts={[
          { key: "1–5", label: "navigate" },
          { key: "R", label: "reconnect" },
          { key: "?", label: "keyboard guide" },
        ]}
      />
    </box>
  )
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

function formatNumber(value: number): string {
  return value === 0 ? "unlimited" : value.toLocaleString("en-US")
}

function formatBytes(value: number): string {
  if (value >= 1024 * 1024 && value % (1024 * 1024) === 0) {
    return `${value / (1024 * 1024)} MiB`
  }
  if (value >= 1024 && value % 1024 === 0) {
    return `${value / 1024} KiB`
  }
  return `${value} bytes`
}
