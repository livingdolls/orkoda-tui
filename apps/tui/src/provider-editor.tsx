/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useMemo, useState } from "react"

import { type LLMProviderInfo, type LLMProviderInput, saveLLMProvider } from "./llm-providers"
import { Banner, BOLD, Card, Chip, colors, KeyHints, PageHeader } from "./ui"

type JSONMode = NonNullable<LLMProviderInput["json_mode"]>
type Field = "name" | "baseURL" | "model" | "apiKey" | "timeout"

type Preset = {
  label: string
  name: string
  baseURL: string
  model: string
  jsonMode: JSONMode
}

const presets: Preset[] = [
  {
    label: "DeepSeek",
    name: "deepseek",
    baseURL: "https://api.deepseek.com",
    model: "deepseek-v4-flash",
    jsonMode: "json_object",
  },
  {
    label: "OpenAI",
    name: "openai",
    baseURL: "https://api.openai.com/v1",
    model: "gpt-5.2",
    jsonMode: "json_schema",
  },
  { label: "Custom", name: "custom", baseURL: "https://", model: "", jsonMode: "json_schema" },
]

const jsonModes: JSONMode[] = ["json_schema", "json_object", "prompt_only"]

export function ProviderEditor({
  provider,
  onSaved,
  onCancel,
}: {
  provider?: LLMProviderInfo
  onSaved: (provider: LLMProviderInfo) => void
  onCancel: () => void
}) {
  const editing = Boolean(provider)
  const initialPreset = useMemo(
    () =>
      Math.max(
        0,
        presets.findIndex((item) => item.name === provider?.name),
      ),
    [provider?.name],
  )
  const [presetIndex, setPresetIndex] = useState(initialPreset)
  const [field, setField] = useState<Field>(editing ? "baseURL" : "name")
  const [name, setName] = useState(provider?.name ?? presets[initialPreset]?.name ?? "custom")
  const [baseURL, setBaseURL] = useState(
    provider?.base_url ?? presets[initialPreset]?.baseURL ?? "https://",
  )
  const [model, setModel] = useState(provider?.default_model ?? presets[initialPreset]?.model ?? "")
  const [apiKey, setAPIKey] = useState("")
  const [jsonMode, setJSONMode] = useState<JSONMode>(
    provider?.json_mode ?? presets[initialPreset]?.jsonMode ?? "json_schema",
  )
  const [timeoutSeconds, setTimeoutSeconds] = useState(
    String(Math.max(1, Math.round((provider?.timeout_ms ?? 60000) / 1000))),
  )
  const [message, setMessage] = useState("")
  const [saving, setSaving] = useState(false)

  const fields: Field[] = editing
    ? ["baseURL", "model", "apiKey", "timeout"]
    : ["name", "baseURL", "model", "apiKey", "timeout"]

  const moveField = (offset: number) => {
    const index = fields.indexOf(field)
    setField(fields[(index + offset + fields.length) % fields.length] ?? fields[0] ?? "name")
  }

  const applyPreset = (nextIndex: number) => {
    const next = presets[nextIndex]
    if (!next || editing) return
    setPresetIndex(nextIndex)
    setName(next.name)
    setBaseURL(next.baseURL)
    setModel(next.model)
    setJSONMode(next.jsonMode)
    setMessage(`Preset ${next.label} applied. Review the model before saving.`)
  }

  const save = async () => {
    if (saving) return
    const normalizedName = name.trim().toLowerCase()
    const seconds = Number.parseInt(timeoutSeconds, 10)
    if (!normalizedName || !baseURL.trim() || !model.trim()) {
      setMessage("Provider name, base URL, and default model are required.")
      return
    }
    if (!editing && !apiKey.trim()) {
      setMessage("Enter an API key when adding a provider.")
      return
    }
    if (!Number.isInteger(seconds) || seconds < 1 || seconds > 600) {
      setMessage("Timeout must be between 1 and 600 seconds.")
      return
    }
    setSaving(true)
    setMessage("Saving provider and protected credential...")
    try {
      const saved = await saveLLMProvider(normalizedName, {
        base_url: baseURL.trim(),
        default_model: model.trim(),
        ...(apiKey.trim() ? { api_key: apiKey.trim() } : {}),
        json_mode: jsonMode,
        timeout_ms: seconds * 1000,
      })
      setAPIKey("")
      onSaved(saved)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to save provider")
    } finally {
      setSaving(false)
    }
  }

  useKeyboard((key) => {
    if (saving) return
    if (key.name === "escape") return onCancel()
    if (key.name === "tab") {
      moveField(key.shift ? -1 : 1)
      return
    }
    if (key.ctrl && key.name === "p" && !editing) {
      applyPreset((presetIndex + 1) % presets.length)
      return
    }
    if (key.ctrl && key.name === "m") {
      const index = jsonModes.indexOf(jsonMode)
      setJSONMode(jsonModes[(index + 1) % jsonModes.length] ?? "json_schema")
      return
    }
    if (key.ctrl && key.name === "s") void save()
  })

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title={editing ? `Edit provider · ${provider?.name}` : "Add AI provider"}
        description="Configure an OpenAI-compatible endpoint. The API key is stored separately from SQLite and is never returned by the daemon."
        meta={
          editing
            ? `source: ${provider?.source ?? "runtime"}`
            : `preset: ${presets[presetIndex]?.label}`
        }
      />
      {!editing ? (
        <box flexDirection="row" gap={1}>
          {presets.map((preset, index) => (
            <Chip
              key={preset.name}
              label={preset.label}
              tone={index === presetIndex ? "accent" : "neutral"}
              dot={false}
            />
          ))}
        </box>
      ) : null}
      <Card>
        {editing ? (
          <box flexDirection="row" gap={1}>
            <text fg={colors.muted}>Provider</text>
            <text fg={colors.text} attributes={BOLD}>
              {provider?.name}
            </text>
          </box>
        ) : (
          <FieldInput
            label="Provider name"
            value={name}
            focused={field === "name"}
            placeholder="deepseek"
            onInput={setName}
            onSubmit={() => moveField(1)}
          />
        )}
        <FieldInput
          label="Base URL"
          value={baseURL}
          focused={field === "baseURL"}
          placeholder="https://api.example.com/v1"
          onInput={setBaseURL}
          onSubmit={() => moveField(1)}
        />
        <FieldInput
          label="Default model"
          value={model}
          focused={field === "model"}
          placeholder="provider-model-id"
          onInput={setModel}
          onSubmit={() => moveField(1)}
        />
        <FieldInput
          label={editing ? "New API key (blank keeps current)" : "API key"}
          value={apiKey}
          focused={field === "apiKey"}
          placeholder={
            editing && provider?.credential_stored
              ? "credential already stored"
              : "paste provider API key"
          }
          onInput={setAPIKey}
          onSubmit={() => moveField(1)}
        />
        <FieldInput
          label="Timeout seconds"
          value={timeoutSeconds}
          focused={field === "timeout"}
          placeholder="60"
          onInput={setTimeoutSeconds}
          onSubmit={() => void save()}
        />
      </Card>
      <Card>
        <box flexDirection="row" gap={1} alignItems="center">
          <text fg={colors.muted}>Structured output</text>
          <Chip
            label={jsonMode}
            tone={jsonMode === "prompt_only" ? "warning" : "accent"}
            dot={false}
          />
        </box>
        <text fg={colors.faint}>
          Use json_schema when supported; json_object is the compatible fallback.
        </text>
      </Card>
      <Banner tone="warning">
        <text fg={colors.warning} wrapMode="word">
          The API key is visible while you type because the terminal input component has no
          password-mask mode. Save it in private, then Orkoda immediately clears it from this form.
        </text>
      </Banner>
      {message ? (
        <Banner
          tone={
            message.toLowerCase().includes("failed") || message.toLowerCase().includes("required")
              ? "danger"
              : "accent"
          }
        >
          <text fg={colors.muted} wrapMode="word">
            {message}
          </text>
        </Banner>
      ) : null}
      <KeyHints
        shortcuts={[
          { key: "Tab", label: "next field" },
          ...(!editing ? [{ key: "Ctrl+P", label: "preset" }] : []),
          { key: "Ctrl+M", label: "JSON mode" },
          { key: "Ctrl+S", label: "save" },
          { key: "Esc", label: "cancel" },
        ]}
      />
    </box>
  )
}

function FieldInput({
  label,
  value,
  focused,
  placeholder,
  onInput,
  onSubmit,
}: {
  label: string
  value: string
  focused: boolean
  placeholder: string
  onInput: (value: string) => void
  onSubmit: () => void
}) {
  return (
    <box flexDirection="column">
      <text fg={focused ? colors.accent : colors.muted} attributes={focused ? BOLD : 0}>
        {label}
      </text>
      <input
        value={value}
        focused={focused}
        placeholder={placeholder}
        onInput={onInput}
        onSubmit={onSubmit}
      />
    </box>
  )
}
