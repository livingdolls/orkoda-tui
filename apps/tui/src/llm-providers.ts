import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"

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
