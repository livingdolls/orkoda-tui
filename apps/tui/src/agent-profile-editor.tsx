/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useMemo, useState } from "react"

import type { AgentConfig } from "./agent-settings"
import type { LLMProviderInfo } from "./llm-providers"
import { Banner, Card, colors, KeyHints, PageHeader } from "./ui"

export function AgentProfileEditor({
  agent,
  peer,
  providers,
  onApply,
  onCancel,
}: {
  agent: AgentConfig
  peer?: AgentConfig
  providers: LLMProviderInfo[]
  onApply: (agent: AgentConfig) => void
  onCancel: () => void
}) {
  const [field, setField] = useState<"provider" | "model">("provider")
  const [provider, setProvider] = useState(agent.provider)
  const [model, setModel] = useState(agent.model)
  const [message, setMessage] = useState("")
  const selectedProvider = useMemo(
    () => providers.find((item) => item.name === provider.trim().toLowerCase()),
    [providers, provider],
  )

  const apply = () => {
    const nextProvider = provider.trim().toLowerCase()
    const nextModel = model.trim()
    if ((nextProvider === "") !== (nextModel === "")) {
      setMessage(
        "Provider and model must both be set, or both be empty to inherit the daemon default.",
      )
      return
    }
    if (
      peer?.enabled &&
      agent.enabled &&
      nextProvider !== "" &&
      nextProvider === peer.provider &&
      nextModel === peer.model
    ) {
      setMessage("Executor and Reviewer cannot use the same explicit provider and model.")
      return
    }
    if (nextProvider && !providers.some((item) => item.name === nextProvider)) {
      setMessage(`Provider ${nextProvider} is not registered by the daemon.`)
      return
    }
    onApply({ ...agent, provider: nextProvider, model: nextModel })
  }

  useKeyboard((key) => {
    if (key.name === "escape") return onCancel()
    if (key.name === "tab") {
      setField((current) => (current === "provider" ? "model" : "provider"))
      return
    }
    if (key.ctrl && key.name === "s") {
      apply()
      return
    }
    if (key.name === "d" && selectedProvider) {
      setModel(selectedProvider.default_model)
    }
  })

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title={`Edit ${agent.role.toLowerCase()} model`}
        description="Assign a registered provider and model to this role. Empty fields inherit the daemon default."
        meta={`${providers.length} provider(s) registered`}
      />
      <Card>
        <text fg={field === "provider" ? colors.accent : colors.muted}>Provider</text>
        <input
          value={provider}
          focused={field === "provider"}
          placeholder="deepseek"
          onInput={setProvider}
          onSubmit={() => setField("model")}
        />
        <text fg={field === "model" ? colors.accent : colors.muted}>Model</text>
        <input
          value={model}
          focused={field === "model"}
          placeholder="executor-model"
          onInput={setModel}
          onSubmit={apply}
        />
      </Card>
      <Card>
        <text fg={colors.muted}>Registered providers</text>
        {providers.map((item) => (
          <text key={item.name} fg={item.name === provider ? colors.accent : colors.faint}>
            {`${item.name} · default ${item.default_model}${item.configured ? " · configured" : ""}`}
          </text>
        ))}
      </Card>
      {peer ? (
        <text
          fg={colors.faint}
        >{`Other role: ${peer.role.toLowerCase()} · ${peer.provider || "daemon default"}/${peer.model || "daemon default"}`}</text>
      ) : null}
      {message ? (
        <Banner tone="danger">
          <text fg={colors.danger}>{message}</text>
        </Banner>
      ) : null}
      <KeyHints
        shortcuts={[
          { key: "Tab", label: "field" },
          { key: "D", label: "provider default model" },
          { key: "Ctrl+S", label: "apply" },
          { key: "Esc", label: "cancel" },
        ]}
      />
    </box>
  )
}
