import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"

export type Execution = {
  id: string
  workflow_job_id: string
  workflow_version: number
  execution_version: number
  plan_version_id: string
  workspace_id: string
  base_commit_sha: string
  agent_settings_version: number
  provider: string
  model: string
  status: "PENDING" | "RUNNING" | "COMPLETED" | "FAILED" | "CANCELLED"
  tool_calls: number
  failure_code?: string
  failure_message?: string
  created_at: string
  updated_at: string
}

export type ToolRun = {
  id: string
  execution_id: string
  sequence: number
  tool: string
  status: Execution["status"]
  input_summary: Record<string, unknown>
  output_summary: Record<string, unknown>
  error_code?: string
  error_message?: string
}

export type PatchCheckpoint = {
  id: string
  execution_id: string
  sequence: number
  base_commit_sha: string
  workspace_head_sha: string
  patch_checksum: string
  patch_bytes: number
  changed_files: string[]
  created_at: string
}

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }
export type ExecutionFetch = (
  input: string | URL | Request,
  init?: RequestInit,
) => Promise<Response>

async function request<T>(path: string, fetcher: ExecutionFetch): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 5000)
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
      throw new Error("Execution request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

export function listExecutions(
  jobID: string,
  fetcher: ExecutionFetch = fetch,
): Promise<Execution[]> {
  return request<Execution[]>(`/api/v1/jobs/${jobID}/executions`, fetcher)
}

export function listToolRuns(
  executionID: string,
  fetcher: ExecutionFetch = fetch,
): Promise<ToolRun[]> {
  return request<ToolRun[]>(`/api/v1/executions/${executionID}/tool-runs`, fetcher)
}

export function listCheckpoints(
  executionID: string,
  fetcher: ExecutionFetch = fetch,
): Promise<PatchCheckpoint[]> {
  return request<PatchCheckpoint[]>(`/api/v1/executions/${executionID}/checkpoints`, fetcher)
}
