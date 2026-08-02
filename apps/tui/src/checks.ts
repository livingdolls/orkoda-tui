import { daemonBaseURL } from "./daemon"

export type CheckStatus = "PENDING" | "RUNNING" | "PASSED" | "FAILED" | "CANCELLED"

export type CheckRun = {
  id: string
  workflow_job_id: string
  execution_id: string
  execution_version: number
  workspace_id: string
  status: CheckStatus
  total_steps: number
  passed_steps: number
  failed_steps: number
  started_at?: string
  completed_at?: string
  created_at: string
  updated_at: string
}

export type CheckStep = {
  id: string
  check_run_id: string
  sequence: number
  profile: string
  command: string[]
  status: CheckStatus
  exit_code?: number
  duration_ms: number
  output_text: string
  output_truncated: boolean
  error_message?: string
  started_at?: string
  completed_at?: string
  created_at: string
  updated_at: string
}

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }
export type CheckFetch = (input: string | URL | Request, init?: RequestInit) => Promise<Response>

async function request<T>(path: string, fetcher: CheckFetch): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 5000)
  try {
    const response = await fetcher(`${daemonBaseURL}${path}`, {
      method: "GET",
      headers: { accept: "application/json" },
      signal: controller.signal,
    })
    if (!response.ok) {
      let message = `Daemon returned HTTP ${response.status}`
      try {
        const payload = (await response.json()) as ErrorResponse
        message = payload.error?.message ?? message
      } catch {
        // Keep status message for non-JSON errors.
      }
      throw new Error(message)
    }
    const payload = (await response.json()) as DataResponse<T>
    return payload.data
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("Check request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

export function listChecks(jobID: string, fetcher: CheckFetch = fetch): Promise<CheckRun[]> {
  return request<CheckRun[]>(`/api/v1/jobs/${jobID}/checks`, fetcher)
}

export function listCheckSteps(checkID: string, fetcher: CheckFetch = fetch): Promise<CheckStep[]> {
  return request<CheckStep[]>(`/api/v1/checks/${checkID}/steps`, fetcher)
}
