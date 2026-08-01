import { daemonBaseURL } from "./daemon"

export type LLMProviderInfo = {
  name: string
  default_model: string
  configured: boolean
  structured_output: boolean
  default: boolean
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
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 10000)
  try {
    const response = await fetcher(`${daemonBaseURL}/api/v1/llm/providers`, {
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
    const payload = (await response.json()) as DataResponse<LLMProviderInfo[]>
    return payload.data ?? []
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("LLM provider request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}
