/** @jsxImportSource @opentui/react */

import type { ScrollBoxRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useEffect, useRef, useState } from "react"

import type { DaemonConnection } from "./daemon"
import { daemonBaseURL } from "./daemon"
import {
  getLLMPolicy,
  type LLMPolicyInfo,
  type LLMProviderInfo,
  listLLMProviders,
} from "./llm-providers"
import { BOLD, Card, Chip, colors, Info, Metric, PageHeader, Section } from "./ui"

export function SettingsScreen({ connection }: { connection: DaemonConnection }) {
  const [providers, setProviders] = useState<LLMProviderInfo[]>([])
  const [policy, setPolicy] = useState<LLMPolicyInfo | null>(null)
  const [message, setMessage] = useState("")
  const [loading, setLoading] = useState(false)
  const contentRef = useRef<ScrollBoxRenderable>(null)

  useKeyboard((key) => {
    if (key.name === "pageup") contentRef.current?.scrollBy(-1, "viewport")
    if (key.name === "pagedown") contentRef.current?.scrollBy(1, "viewport")
  })

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
      <PageHeader
        title="Settings"
        description="Which AI providers are configured, and the safety budgets applied to every agent run. This view is read-only."
        meta="read-only view"
      />
      <scrollbox ref={contentRef} flexGrow={1} scrollY={true}>
        <box flexDirection="column" gap={1}>
          <Section title="Local daemon">
            <Card tone="accent">
              <Info label="Address" value={daemonBaseURL} tone="accent" />
              <Info label="Mode" value="local" />
              <Info label="Authentication" value="bearer token" tone="success" />
              <text fg={colors.faint}>Override with ORKODA_DAEMON_URL before running the TUI.</text>
            </Card>
          </Section>

          <Section title="AI providers">
            <Card>
              {loading ? <text fg={colors.warning}>Loading LLM configuration...</text> : null}
              {!loading && providers.length === 0 && message === "" ? (
                <text fg={colors.faint}>No provider metadata is available.</text>
              ) : null}
              {providers.map((provider) => (
                <box
                  key={provider.name}
                  flexDirection="row"
                  justifyContent="space-between"
                  gap={1}
                  paddingLeft={1}
                  paddingRight={1}
                  backgroundColor={provider.default ? colors.accentTint : colors.raised}
                >
                  <box flexDirection="column">
                    <box flexDirection="row" gap={1}>
                      <text
                        fg={provider.default ? colors.text : colors.muted}
                        attributes={provider.default ? BOLD : 0}
                      >
                        {provider.name}
                      </text>
                      {provider.default ? <Chip label="default" tone="accent" dot={false} /> : null}
                    </box>
                    <text fg={colors.faint}>
                      {`model: ${provider.default_model || "not configured"}`}
                    </text>
                  </box>
                  <box flexDirection="row" gap={1}>
                    <Chip
                      label={provider.configured ? "configured" : "not configured"}
                      tone={provider.configured ? "success" : "danger"}
                    />
                    <Chip
                      label={provider.structured_output ? "structured output" : "prompt only"}
                      tone={provider.structured_output ? "accent" : "warning"}
                    />
                  </box>
                </box>
              ))}
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
                <text fg={colors.faint}>
                  {`Maximum structured response: ${formatBytes(policy.max_structured_response_bytes)}`}
                </text>
              </Card>
            </Section>
          ) : null}

          {message ? <text fg={colors.warning}>{message}</text> : null}
        </box>
      </scrollbox>
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
