/** @jsxImportSource @opentui/react */

import { useEffect, useState } from "react"

import type { DaemonConnection } from "./daemon"
import { daemonBaseURL } from "./daemon"
import { type LLMProviderInfo, listLLMProviders } from "./llm-providers"

export function SettingsScreen({ connection }: { connection: DaemonConnection }) {
  const [providers, setProviders] = useState<LLMProviderInfo[]>([])
  const [message, setMessage] = useState("")
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected") {
      setProviders([])
      setMessage("Start the daemon to inspect configured providers.")
      return () => {
        disposed = true
      }
    }

    setLoading(true)
    setMessage("")
    void listLLMProviders()
      .then((items) => {
        if (!disposed) {
          setProviders(items)
        }
      })
      .catch((error) => {
        if (!disposed) {
          setMessage(error instanceof Error ? error.message : "Failed to load LLM providers")
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

  return (
    <box flexDirection="column" gap={1}>
      <text fg="#E2E8F0">Local daemon endpoint</text>
      <text fg="#7DD3FC">{daemonBaseURL}</text>
      <text fg="#94A3B8">Override with ORKODA_DAEMON_URL before running the TUI.</text>

      <text fg="#E2E8F0">LLM providers</text>
      {loading ? <text fg="#FACC15">Loading provider configuration...</text> : null}
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
      {message ? <text fg="#FACC15">{message}</text> : null}
      <text fg="#64748B">
        Configure ORKODA_LLM_PROVIDER, ORKODA_LLM_BASE_URL, ORKODA_LLM_MODEL, and ORKODA_LLM_API_KEY
        before starting the daemon.
      </text>
    </box>
  )
}
