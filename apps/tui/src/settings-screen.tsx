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
    <box flexDirection="column" gap={1}>
      <text fg="#E2E8F0">Local daemon endpoint</text>
      <text fg="#7DD3FC">{daemonBaseURL}</text>
      <text fg="#94A3B8">Override with ORKODA_DAEMON_URL before running the TUI.</text>

      <text fg="#E2E8F0">LLM providers</text>
      {loading ? <text fg="#FACC15">Loading LLM configuration...</text> : null}
      {!loading && providers.length === 0 && message === "" ? (
        <text fg="#94A3B8">No provider metadata is available.</text>
      ) : null}
      {providers.map((provider) => (
        <box key={provider.name} flexDirection="column">
          <text fg={provider.configured ? "#4ADE80" : "#F87171"}>
            {`${provider.default ? "★" : "•"} ${provider.name}${provider.default ? " (default)" : ""}`}
          </text>
          <text fg="#94A3B8">
            {`  model: ${provider.default_model || "not configured"} • structured output: ${provider.structured_output ? "yes" : "prompt only"}`}
          </text>
        </box>
      ))}

      {policy ? (
        <box flexDirection="column">
          <text fg="#E2E8F0">LLM execution policy</text>
          <text fg="#94A3B8">
            {`Attempts: ${policy.max_attempts} • attempt timeout: ${formatDuration(policy.attempt_timeout_ms)} • wall clock: ${formatDuration(policy.max_wall_clock_ms)}`}
          </text>
          <text fg="#94A3B8">
            {`Backoff: ${formatDuration(policy.initial_backoff_ms)} → ${formatDuration(policy.max_backoff_ms)} • jitter: ${Math.round(policy.jitter * 100)}%`}
          </text>
          <text fg="#94A3B8">
            {`Token budget: ${formatNumber(policy.budget.max_input_tokens)} input / ${formatNumber(policy.budget.max_output_tokens)} output / ${formatNumber(policy.budget.max_total_tokens)} total`}
          </text>
          <text fg="#94A3B8">{`Fallbacks: ${fallbackLabel}`}</text>

          <text fg="#E2E8F0">LLM safety</text>
          <text fg={policy.redaction_mode === "strict" ? "#4ADE80" : "#FACC15"}>
            {`Prompt redaction: ${policy.redaction_mode}`}
          </text>
          <text fg="#94A3B8">
            {`Structured validation: ${policy.structured_validation ? "enabled" : "disabled"} • repair attempts: ${policy.max_repair_attempts}`}
          </text>
          <text fg="#94A3B8">
            {`Maximum structured response: ${formatBytes(policy.max_structured_response_bytes)}`}
          </text>
        </box>
      ) : null}

      {message ? <text fg="#FACC15">{message}</text> : null}
      <text fg="#64748B">
        Configure provider credentials, execution policy, and safety through ORKODA_LLM_* environment
        variables before starting the daemon.
      </text>
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
