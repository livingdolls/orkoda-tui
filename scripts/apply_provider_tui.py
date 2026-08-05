from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def write(path: str, content: str) -> None:
    target = ROOT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")


def replace(path: str, old: str, new: str, count: int = 1) -> None:
    target = ROOT / path
    content = target.read_text(encoding="utf-8")
    if old not in content:
        raise RuntimeError(f"expected snippet not found in {path}: {old[:180]!r}")
    target.write_text(content.replace(old, new, count), encoding="utf-8")


write("apps/tui/src/llm-providers.ts", r'''import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"

export type LLMProviderInfo = {
  name: string
  default_model: string
  configured: boolean
  structured_output: boolean
  default: boolean
  base_url?: string
  json_mode?: "json_schema" | "json_object" | "prompt_only"
  timeout_ms?: number
  credential_stored: boolean
  source?: "built-in" | "environment" | "tui" | "runtime"
  editable: boolean
  deletable: boolean
}

export type LLMProviderInput = {
  base_url: string
  default_model: string
  api_key?: string
  json_mode?: "json_schema" | "json_object" | "prompt_only"
  timeout_ms?: number
  headers?: Record<string, string>
}

export type LLMProviderTestResult = {
  provider: string
  model: string
  latency_ms: number
  response_preview: string
}

export type LLMFallbackTarget = {
  provider: string
  model: string
}

export type LLMPolicyInfo = {
  attempt_timeout_ms: number
  max_wall_clock_ms: number
  max_attempts: number
  initial_backoff_ms: number
  max_backoff_ms: number
  jitter: number
  fallbacks: LLMFallbackTarget[]
  budget: {
    max_input_tokens: number
    max_output_tokens: number
    max_total_tokens: number
  }
  redaction_mode: "strict" | "report" | "off"
  structured_validation: boolean
  max_repair_attempts: number
  max_structured_response_bytes: number
}

export type LLMProviderFetch = (
  input: string | URL | Request,
  init?: RequestInit,
) => Promise<Response>

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }

export async function listLLMProviders(
  fetcher: LLMProviderFetch = fetch,
): Promise<LLMProviderInfo[]> {
  return requestLLMData<LLMProviderInfo[]>("/api/v1/llm/providers", fetcher)
}

export async function saveLLMProvider(
  name: string,
  input: LLMProviderInput,
  fetcher: LLMProviderFetch = fetch,
): Promise<LLMProviderInfo> {
  return requestLLMData<LLMProviderInfo>(
    `/api/v1/llm/providers/${encodeURIComponent(name)}`,
    fetcher,
    { method: "PUT", body: JSON.stringify(input) },
  )
}

export async function deleteLLMProvider(
  name: string,
  fetcher: LLMProviderFetch = fetch,
): Promise<void> {
  await requestLLMData<void>(`/api/v1/llm/providers/${encodeURIComponent(name)}`, fetcher, {
    method: "DELETE",
  })
}

export async function testLLMProvider(
  name: string,
  fetcher: LLMProviderFetch = fetch,
): Promise<LLMProviderTestResult> {
  return requestLLMData<LLMProviderTestResult>(
    `/api/v1/llm/providers/${encodeURIComponent(name)}/test`,
    fetcher,
    { method: "POST" },
  )
}

export async function getLLMPolicy(fetcher: LLMProviderFetch = fetch): Promise<LLMPolicyInfo> {
  return requestLLMData<LLMPolicyInfo>("/api/v1/llm/policy", fetcher)
}

async function requestLLMData<T>(
  path: string,
  fetcher: LLMProviderFetch,
  init: RequestInit = {},
): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 30000)
  try {
    const headers = new Headers(init.headers)
    headers.set("accept", "application/json")
    if (init.body !== undefined) headers.set("content-type", "application/json")
    const response = await requestWithDaemonAuth(fetcher, `${daemonBaseURL}${path}`, {
      ...init,
      headers,
      signal: controller.signal,
    })
    if (!response.ok) {
      let message = `Daemon returned HTTP ${response.status}`
      try {
        const payload = (await response.json()) as ErrorResponse
        if (payload.error?.message) message = payload.error.message
      } catch {
        // Preserve the status fallback for non-JSON responses.
      }
      throw new Error(message)
    }
    if (response.status === 204) return undefined as T
    const payload = (await response.json()) as DataResponse<T>
    return payload.data
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("LLM configuration request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}
''')

write("apps/tui/src/llm-providers.test.ts", r'''import { describe, expect, test } from "bun:test"

import {
  deleteLLMProvider,
  getLLMPolicy,
  type LLMProviderFetch,
  listLLMProviders,
  saveLLMProvider,
  testLLMProvider,
} from "./llm-providers"

describe("LLM provider API client", () => {
  test("lists safe provider metadata", async () => {
    let requestedURL = ""
    const fetcher: LLMProviderFetch = async (input) => {
      requestedURL = String(input)
      return new Response(
        JSON.stringify({
          data: [
            {
              name: "openrouter",
              default_model: "example/model",
              configured: true,
              structured_output: true,
              default: true,
              base_url: "https://provider.example/v1",
              json_mode: "json_schema",
              timeout_ms: 60000,
              credential_stored: true,
              source: "tui",
              editable: true,
              deletable: true,
            },
          ],
        }),
        { status: 200 },
      )
    }

    const providers = await listLLMProviders(fetcher)
    expect(requestedURL).toEndWith("/api/v1/llm/providers")
    expect(providers[0]?.name).toBe("openrouter")
    expect(providers[0]?.credential_stored).toBe(true)
    expect(JSON.stringify(providers)).not.toContain("api_key")
  })

  test("creates, tests, and deletes a provider", async () => {
    const calls: Array<{ url: string; method: string; body: string }> = []
    const fetcher: LLMProviderFetch = async (input, init) => {
      calls.push({
        url: String(input),
        method: init?.method ?? "GET",
        body: typeof init?.body === "string" ? init.body : "",
      })
      if (init?.method === "DELETE") return new Response(null, { status: 204 })
      if (String(input).endsWith("/test")) {
        return new Response(
          JSON.stringify({
            data: { provider: "deepseek", model: "deepseek-v4-flash", latency_ms: 42, response_preview: "OK" },
          }),
          { status: 200 },
        )
      }
      return new Response(
        JSON.stringify({
          data: {
            name: "deepseek",
            default_model: "deepseek-v4-flash",
            configured: true,
            structured_output: true,
            default: false,
            credential_stored: true,
            source: "tui",
            editable: true,
            deletable: true,
          },
        }),
        { status: 200 },
      )
    }

    const saved = await saveLLMProvider(
      "deepseek",
      {
        base_url: "https://api.deepseek.com",
        default_model: "deepseek-v4-flash",
        api_key: "test-provider-value",
        json_mode: "json_object",
      },
      fetcher,
    )
    expect(saved.configured).toBe(true)
    expect(calls[0]?.method).toBe("PUT")
    expect(calls[0]?.body).toContain("test-provider-value")

    const result = await testLLMProvider("deepseek", fetcher)
    expect(result.response_preview).toBe("OK")
    expect(calls[1]?.method).toBe("POST")

    await deleteLLMProvider("deepseek", fetcher)
    expect(calls[2]?.method).toBe("DELETE")
  })

  test("loads the execution and safety policy", async () => {
    const fetcher: LLMProviderFetch = async () =>
      new Response(
        JSON.stringify({
          data: {
            attempt_timeout_ms: 45000,
            max_wall_clock_ms: 120000,
            max_attempts: 3,
            initial_backoff_ms: 500,
            max_backoff_ms: 8000,
            jitter: 0.2,
            fallbacks: [{ provider: "local-fake", model: "local-fake-planner-v1" }],
            budget: { max_input_tokens: 50000, max_output_tokens: 8000, max_total_tokens: 60000 },
            redaction_mode: "strict",
            structured_validation: true,
            max_repair_attempts: 1,
            max_structured_response_bytes: 1048576,
          },
        }),
        { status: 200 },
      )

    const policy = await getLLMPolicy(fetcher)
    expect(policy.max_attempts).toBe(3)
    expect(policy.budget.max_total_tokens).toBe(60000)
  })

  test("surfaces daemon errors", async () => {
    const fetcher: LLMProviderFetch = async () =>
      new Response(JSON.stringify({ error: { message: "provider catalog unavailable" } }), {
        status: 503,
      })
    await expect(listLLMProviders(fetcher)).rejects.toThrow("provider catalog unavailable")
  })
})
''')

write("apps/tui/src/provider-editor.tsx", r'''/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useMemo, useState } from "react"

import {
  type LLMProviderInfo,
  type LLMProviderInput,
  saveLLMProvider,
} from "./llm-providers"
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
    () => Math.max(0, presets.findIndex((item) => item.name === provider?.name)),
    [provider?.name],
  )
  const [presetIndex, setPresetIndex] = useState(initialPreset)
  const [field, setField] = useState<Field>(editing ? "baseURL" : "name")
  const [name, setName] = useState(provider?.name ?? presets[initialPreset]?.name ?? "custom")
  const [baseURL, setBaseURL] = useState(provider?.base_url ?? presets[initialPreset]?.baseURL ?? "https://")
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
        meta={editing ? `source: ${provider?.source ?? "runtime"}` : `preset: ${presets[presetIndex]?.label}`}
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
            <text fg={colors.text} attributes={BOLD}>{provider?.name}</text>
          </box>
        ) : (
          <FieldInput label="Provider name" value={name} focused={field === "name"} placeholder="deepseek" onInput={setName} onSubmit={() => moveField(1)} />
        )}
        <FieldInput label="Base URL" value={baseURL} focused={field === "baseURL"} placeholder="https://api.example.com/v1" onInput={setBaseURL} onSubmit={() => moveField(1)} />
        <FieldInput label="Default model" value={model} focused={field === "model"} placeholder="provider-model-id" onInput={setModel} onSubmit={() => moveField(1)} />
        <FieldInput
          label={editing ? "New API key (blank keeps current)" : "API key"}
          value={apiKey}
          focused={field === "apiKey"}
          placeholder={editing && provider?.credential_stored ? "credential already stored" : "paste provider API key"}
          onInput={setAPIKey}
          onSubmit={() => moveField(1)}
        />
        <FieldInput label="Timeout seconds" value={timeoutSeconds} focused={field === "timeout"} placeholder="60" onInput={setTimeoutSeconds} onSubmit={() => void save()} />
      </Card>
      <Card>
        <box flexDirection="row" gap={1} alignItems="center">
          <text fg={colors.muted}>Structured output</text>
          <Chip label={jsonMode} tone={jsonMode === "prompt_only" ? "warning" : "accent"} dot={false} />
        </box>
        <text fg={colors.faint}>Use json_schema when supported; json_object is the compatible fallback.</text>
      </Card>
      <Banner tone="warning">
        <text fg={colors.warning} wrapMode="word">
          The API key is visible while you type because the terminal input component has no password-mask mode. Save it in private, then Orkoda immediately clears it from this form.
        </text>
      </Banner>
      {message ? (
        <Banner tone={message.toLowerCase().includes("failed") || message.toLowerCase().includes("required") ? "danger" : "accent"}>
          <text fg={colors.muted} wrapMode="word">{message}</text>
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
      <text fg={focused ? colors.accent : colors.muted} attributes={focused ? BOLD : 0}>{label}</text>
      <input value={value} focused={focused} placeholder={placeholder} onInput={onInput} onSubmit={onSubmit} />
    </box>
  )
}
''')

write("apps/tui/src/settings-screen.tsx", r'''/** @jsxImportSource @opentui/react */

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
          setMessage(`${provider.name} saved and activated. Assign it to Planner, Executor, or Reviewer in Agents.`)
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
                <text fg={colors.faint}>No provider metadata is available. Press N to add one.</text>
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
                        <text fg={isSelected ? colors.accent : colors.faint}>{isSelected ? "▸" : " "}</text>
                        <text fg={colors.text} attributes={BOLD}>{provider.name}</text>
                        {provider.default ? <Chip label="daemon default" tone="accent" dot={false} /> : null}
                        <Chip label={provider.source ?? "runtime"} dot={false} />
                      </box>
                      <box flexDirection="row" gap={1}>
                        <Chip
                          label={provider.credential_stored ? "credential stored" : "credential missing"}
                          tone={provider.credential_stored ? "success" : "danger"}
                        />
                        <Chip
                          label={provider.structured_output ? "structured" : "prompt only"}
                          tone={provider.structured_output ? "accent" : "warning"}
                        />
                      </box>
                    </box>
                    <text fg={colors.muted}>{`model: ${provider.default_model || "not configured"}`}</text>
                    {provider.base_url ? <text fg={colors.faint}>{provider.base_url}</text> : null}
                  </box>
                )
              })}
            </Card>
          </Section>

          <Section title="How assignment works">
            <Card tone="accent">
              <text fg={colors.muted} wrapMode="word">
                Saving a provider activates it immediately. Open Agents afterward and choose a provider/model independently for Planner, Executor, and Reviewer.
              </text>
              <Info label="Daemon" value={daemonBaseURL} tone="accent" />
              <Info label="Credential storage" value="OS keychain, with protected local fallback" tone="success" />
            </Card>
          </Section>

          {policy ? (
            <Section title="Execution policy">
              <Card>
                <box flexDirection="row" gap={1}>
                  <Metric label="Retries" value={String(policy.max_attempts)} />
                  <Metric label="Timeout per try" value={formatDuration(policy.attempt_timeout_ms)} />
                  <Metric label="Total time limit" value={formatDuration(policy.max_wall_clock_ms)} />
                </box>
                <box flexDirection="row" gap={1}>
                  <Metric label="Input budget" value={formatNumber(policy.budget.max_input_tokens)} />
                  <Metric label="Output budget" value={formatNumber(policy.budget.max_output_tokens)} />
                  <Metric label="Total budget" value={formatNumber(policy.budget.max_total_tokens)} />
                </box>
                <text fg={colors.faint}>
                  {`Backoff ${formatDuration(policy.initial_backoff_ms)} → ${formatDuration(policy.max_backoff_ms)} · jitter ${Math.round(policy.jitter * 100)}% · fallbacks ${fallbackLabel}`}
                </text>
                <box flexDirection="row" gap={1} flexWrap="wrap">
                  <Chip label={`secret redaction: ${policy.redaction_mode}`} tone={policy.redaction_mode === "strict" ? "success" : "warning"} />
                  <Chip label={`schema validation: ${policy.structured_validation ? "on" : "off"}`} tone={policy.structured_validation ? "success" : "warning"} />
                  <Chip label={`repair attempts: ${policy.max_repair_attempts}`} tone="accent" />
                </box>
                <text fg={colors.faint}>{`Maximum structured response: ${formatBytes(policy.max_structured_response_bytes)}`}</text>
              </Card>
            </Section>
          ) : null}

          {mode === "delete" && selected ? (
            <Banner tone="danger">
              <text fg={colors.danger} attributes={BOLD}>{`Remove ${selected.name}?`}</text>
              <text fg={colors.muted} wrapMode="word">
                This removes the TUI provider configuration and stored credential. Existing execution and review snapshots are preserved. Press Enter to confirm or Esc to cancel.
              </text>
            </Banner>
          ) : null}
          {message ? (
            <Banner tone={message.toLowerCase().includes("failed") || message.toLowerCase().includes("error") ? "danger" : "accent"}>
              <text fg={colors.muted} wrapMode="word">{message}</text>
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
''')

replace(
    "apps/tui/src/app.tsx",
    '''  const [boardInteractionActive, setBoardInteractionActive] = useState(false)
  const [showHelp, setShowHelp] = useState(false)''',
    '''  const [boardInteractionActive, setBoardInteractionActive] = useState(false)
  const [settingsInteractionActive, setSettingsInteractionActive] = useState(false)
  const [showHelp, setShowHelp] = useState(false)''',
)
replace(
    "apps/tui/src/app.tsx",
    '''    if (boardInteractionActive) return''',
    '''    if (boardInteractionActive || settingsInteractionActive) return''',
)
replace(
    "apps/tui/src/app.tsx",
    '''          ) : activeScreen === "settings" ? (
            <SettingsScreen connection={connection} />''',
    '''          ) : activeScreen === "settings" ? (
            <SettingsScreen
              connection={connection}
              onInteractionChange={setSettingsInteractionActive}
            />''',
)
replace(
    "apps/tui/src/app.tsx",
    '''    { key: "R", label: "refresh all execution and review stages" },
  ]
  const general''',
    '''    { key: "R", label: "refresh all execution and review stages" },
  ]
  const settingsShortcuts: Shortcut[] = [
    { key: "↑↓", label: "select provider" },
    { key: "N", label: "add provider" },
    { key: "Enter", label: "edit provider" },
    { key: "T", label: "test connection" },
    { key: "D", label: "delete TUI provider" },
  ]
  const general''',
)
replace(
    "apps/tui/src/app.tsx",
    '''            shortcuts={screen === "board" ? boardShortcuts : [{ key: "←→", label: "switch area" }]}''',
    '''            shortcuts={
              screen === "board"
                ? boardShortcuts
                : screen === "settings"
                  ? settingsShortcuts
                  : [{ key: "←→", label: "switch area" }]
            }''',
)

write(".env.example", r'''ORKODA_ENV=development
ORKODA_LOG_LEVEL=debug
ORKODA_API_HOST=127.0.0.1
ORKODA_API_PORT=8181
ORKODA_API_TOKEN=
# Defaults to ${ORKODA_DATA_DIR}/api.token when unset.
ORKODA_API_TOKEN_FILE=.orkoda/api.token
ORKODA_SHUTDOWN_TIMEOUT=10s
ORKODA_DATA_DIR=.orkoda
ORKODA_DATABASE_PATH=.orkoda/orkoda.db
ORKODA_ARTIFACT_DIR=.orkoda/artifacts
ORKODA_SANDBOX_MODE=docker
ORKODA_SANDBOX_IMAGE=orkoda-sandbox:local
ORKODA_ALLOW_UNSANDBOXED_CHECKS=false

# Start without an external credential, then add OpenAI, DeepSeek, or another
# OpenAI-compatible provider from TUI → Settings. Assign models in TUI → Agents.
ORKODA_LLM_PROVIDER=local-fake

# Advanced bootstrap/CI configuration only. Normal desktop use should leave
# these empty and manage providers through the TUI.
ORKODA_LLM_PROVIDERS_JSON=
ORKODA_LLM_BASE_URL=
ORKODA_LLM_API_KEY=
ORKODA_LLM_MODEL=
ORKODA_LLM_JSON_MODE=json_schema
ORKODA_LLM_TIMEOUT=60s
ORKODA_LLM_HEADERS_JSON={}

ORKODA_LLM_ATTEMPT_TIMEOUT=45s
ORKODA_LLM_MAX_WALL_CLOCK=2m
ORKODA_LLM_MAX_ATTEMPTS=3
ORKODA_LLM_BACKOFF_INITIAL=500ms
ORKODA_LLM_BACKOFF_MAX=8s
ORKODA_LLM_BACKOFF_JITTER=0.2
ORKODA_LLM_MAX_INPUT_TOKENS=50000
ORKODA_LLM_MAX_OUTPUT_TOKENS=8000
ORKODA_LLM_MAX_TOTAL_TOKENS=60000
ORKODA_LLM_FALLBACKS_JSON=[]
ORKODA_LLM_REDACTION_MODE=strict
ORKODA_LLM_MAX_REPAIR_ATTEMPTS=1
ORKODA_LLM_MAX_STRUCTURED_RESPONSE_BYTES=1048576
''')

replace(
    "README.md",
    '''To use an OpenAI-compatible endpoint, configure the daemon environment before startup:

```text
ORKODA_LLM_PROVIDER=openrouter
ORKODA_LLM_BASE_URL=https://provider.example/v1
ORKODA_LLM_API_KEY=your-secret
ORKODA_LLM_MODEL=provider/model-name
ORKODA_LLM_JSON_MODE=json_schema
ORKODA_LLM_TIMEOUT=60s
ORKODA_LLM_HEADERS_JSON={"X-Title":"Orkoda"}
```

`ORKODA_LLM_JSON_MODE` accepts `json_schema`, `json_object`, or `prompt_only`. HTTPS is required except for loopback development endpoints such as `http://127.0.0.1:11434/v1`. Credentials remain in process memory and are never returned by the provider status API or stored in SQLite.''',
    '''Add external providers from the TUI instead of editing JSON:

1. Open **Settings**.
2. Press `N` and choose the DeepSeek, OpenAI, or Custom preset with `Ctrl+P`.
3. Enter the base URL, model, and API key, then save with `Ctrl+S`.
4. Press `T` on the saved provider to test the connection.
5. Open **Agents** and assign a provider/model independently to Planner, Executor, and Reviewer.

Provider metadata is stored in SQLite. API keys are stored separately in the operating-system keychain; when no supported keychain command is available, Orkoda uses a local credential file under `.orkoda/` protected with owner-only permissions. API keys are never returned by the provider API or included in diagnostics.

Environment provider variables remain available for automated deployments and CI bootstrap. `ORKODA_LLM_JSON_MODE` accepts `json_schema`, `json_object`, or `prompt_only`. HTTPS is required except for loopback development endpoints such as `http://127.0.0.1:11434/v1`.''',
)
replace(
    "README.md",
    '''The Settings screen lists registered providers, execution policy, prompt-redaction mode, repair limit, and maximum structured-response size.''',
    '''The Settings screen creates, edits, tests, and removes runtime providers, and also lists execution policy, prompt-redaction mode, repair limit, and maximum structured-response size.''',
)
replace(
    "README.md",
    '''├── api.token
├── orkoda.db.bak''',
    '''├── api.token
├── credentials.json  # only when the OS keychain is unavailable
├── orkoda.db.bak''',
)

write("docs/provider-setup.md", r'''# AI provider setup

The normal setup path is entirely inside the TUI.

1. Start the daemon with `make api`.
2. Start the TUI with `make tui`.
3. Open **Settings** and press `N`.
4. Cycle DeepSeek, OpenAI, and Custom presets with `Ctrl+P`.
5. Review the provider name, base URL, model, structured-output mode, and timeout.
6. Paste the API key and save with `Ctrl+S`.
7. Select the provider and press `T` to run a small connection test.
8. Open **Agents** and assign the provider/model to Planner, Executor, or Reviewer.

A saved provider is registered in the running daemon immediately; no restart is required. Existing workflow runs retain their immutable provider/model snapshots.

## Credential boundary

Provider metadata is persisted in SQLite, but API keys are not. Orkoda first uses the operating-system keychain. On systems where the keychain command is unavailable, it falls back to `${ORKODA_DATA_DIR}/credentials.json`, created with owner-only permissions. Provider APIs, diagnostics, and activity events expose only whether a credential is stored.

The current terminal input component does not provide password masking. The key is therefore visible only while it is being typed. After a successful save, the TUI clears the field and the daemon never returns the value.

## Advanced environment bootstrap

Environment configuration remains supported for CI, containers, and centrally managed deployments:

```text
ORKODA_LLM_PROVIDER=provider-name
ORKODA_LLM_PROVIDERS_JSON=[{"name":"provider-name","base_url":"https://provider.example/v1","api_key_env":"PROVIDER_API_KEY","model":"provider-model"}]
PROVIDER_API_KEY=...
```

A TUI-managed record with the same provider name overrides the environment provider at runtime. Removing that TUI record restores the environment-backed provider.
''')
