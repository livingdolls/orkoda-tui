import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"

export type LLMProviderInfo = {
  name: string
  default_model: string
  configured: boolean
  structured_output: boolean
  default: boolean
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

export async function getLLMPolicy(fetcher: LLMProviderFetch = fetch): Promise<LLMPolicyInfo> {
  return requestLLMData<LLMPolicyInfo>("/api/v1/llm/policy", fetcher)
}

async function requestLLMData<T>(path: string, fetcher: LLMProviderFetch): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 10000)
  try {
    const response = await requestWithDaemonAuth(fetcher, `${daemonBaseURL}${path}`, {
      method: "GET",
      headers: { accept: "application/json" },
      signal: controller.signal,
    })
    if (!response.ok) {
      let message = `Daemon returned HTTP ${response.status}`
      try {
        const payload = (await response.json()) as ErrorResponse
        if (payload.error?.message) {
          message = payload.error.message
        }
      } catch {
        // Preserve the HTTP fallback for non-JSON responses.
      }
      throw new Error(message)
    }
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
