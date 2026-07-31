import { daemonBaseURL } from "./daemon"

export type RepositoryCommands = Record<string, string[]>

export type RepositorySnapshot = {
  root_path: string
  head_sha: string
  languages: string[]
  frameworks: string[]
  package_managers: string[]
  commands: RepositoryCommands
  important_files: string[]
  top_level_entries: string[]
  file_count: number
  skipped_files: number
  truncated: boolean
}

export type RepositorySummary = {
  id: string
  repository_id: string
  project_id: string
  head_sha: string
  dirty: boolean
  summary: RepositorySnapshot
  created_at: string
}

export type NormalizedPlan = {
  goal: string
  summary: string
  scope: string[]
  non_goals: string[]
  acceptance_criteria: string[]
  constraints: string[]
  affected_areas: string[]
  risks: string[]
  open_questions: string[]
  repository: {
    repository_id: string
    summary_id: string
    head_sha: string
    dirty: boolean
    languages: string[]
    frameworks: string[]
    package_managers: string[]
    commands: RepositoryCommands
    important_files: string[]
  }
}

export type PlanningContext = {
  id: string
  plan_id: string
  plan_version_id: string
  plan_version: number
  repository_summary_id: string
  normalized_plan: NormalizedPlan
  created_at: string
}

export type PlanningFetch = (input: string | URL | Request, init?: RequestInit) => Promise<Response>

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }

async function request<T>(path: string, init: RequestInit, fetcher: PlanningFetch): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 15000)

  try {
    const headers = new Headers(init.headers)
    headers.set("accept", "application/json")
    const response = await fetcher(`${daemonBaseURL}${path}`, {
      ...init,
      headers,
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
      throw new Error("Planning request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

export function generateRepositorySummary(
  repositoryID: string,
  fetcher: PlanningFetch = fetch,
): Promise<RepositorySummary> {
  return request<RepositorySummary>(
    `/api/v1/repositories/${repositoryID}/summaries`,
    { method: "POST" },
    fetcher,
  )
}

export function getCurrentRepositorySummary(
  repositoryID: string,
  fetcher: PlanningFetch = fetch,
): Promise<RepositorySummary> {
  return request<RepositorySummary>(
    `/api/v1/repositories/${repositoryID}/summaries/current`,
    { method: "GET" },
    fetcher,
  )
}

export function normalizePlan(
  planID: string,
  fetcher: PlanningFetch = fetch,
): Promise<PlanningContext> {
  return request<PlanningContext>(`/api/v1/plans/${planID}/normalize`, { method: "POST" }, fetcher)
}

export function getPlanningContext(
  planID: string,
  fetcher: PlanningFetch = fetch,
): Promise<PlanningContext> {
  return request<PlanningContext>(`/api/v1/plans/${planID}/context`, { method: "GET" }, fetcher)
}
