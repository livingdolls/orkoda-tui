import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"

export type PlanStatus = "DRAFT" | "READY" | "PLANNING" | "NEEDS_INPUT" | "APPROVED" | "ARCHIVED"

export type PlanVersion = {
  id: string
  plan_id: string
  version: number
  requirement: string
  acceptance_criteria: string[]
  constraints: string[]
  created_at: string
}

export type Plan = {
  id: string
  project_id: string
  title: string
  status: PlanStatus
  current_version: number
  versions: PlanVersion[]
  created_at: string
  updated_at: string
}

export type PlanInput = {
  title: string
  requirement: string
  acceptanceCriteria: string[]
  constraints: string[]
}

export type PlanFetch = (input: string | URL | Request, init?: RequestInit) => Promise<Response>

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }

async function request<T>(path: string, init: RequestInit, fetcher: PlanFetch): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 5000)

  try {
    const headers = new Headers(init.headers)
    headers.set("accept", "application/json")
    if (init.body) {
      headers.set("content-type", "application/json")
    }
    const response = await requestWithDaemonAuth(fetcher, `${daemonBaseURL}${path}`, {
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
        // Keep the status-based fallback for non-JSON responses.
      }
      throw new Error(message)
    }
    if (response.status === 204) {
      return undefined as T
    }
    const payload = (await response.json()) as DataResponse<T>
    return payload.data
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("Plan request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

export function listPlans(projectID: string, fetcher: PlanFetch = fetch): Promise<Plan[]> {
  return request<Plan[]>(`/api/v1/projects/${projectID}/plans`, { method: "GET" }, fetcher)
}

export function createPlan(
  projectID: string,
  input: PlanInput,
  fetcher: PlanFetch = fetch,
): Promise<Plan> {
  return request<Plan>(
    `/api/v1/projects/${projectID}/plans`,
    {
      method: "POST",
      body: JSON.stringify({
        title: input.title,
        requirement: input.requirement,
        acceptance_criteria: input.acceptanceCriteria,
        constraints: input.constraints,
      }),
    },
    fetcher,
  )
}

export function addPlanVersion(
  planID: string,
  input: Omit<PlanInput, "title">,
  fetcher: PlanFetch = fetch,
): Promise<Plan> {
  return request<Plan>(
    `/api/v1/plans/${planID}/versions`,
    {
      method: "POST",
      body: JSON.stringify({
        requirement: input.requirement,
        acceptance_criteria: input.acceptanceCriteria,
        constraints: input.constraints,
      }),
    },
    fetcher,
  )
}

export function updatePlan(
  planID: string,
  title: string,
  status: PlanStatus,
  fetcher: PlanFetch = fetch,
): Promise<Plan> {
  return request<Plan>(
    `/api/v1/plans/${planID}`,
    { method: "PATCH", body: JSON.stringify({ title, status }) },
    fetcher,
  )
}

export function deletePlan(planID: string, fetcher: PlanFetch = fetch): Promise<void> {
  return request<void>(`/api/v1/plans/${planID}`, { method: "DELETE" }, fetcher)
}

export function splitPlanLines(value: string): string[] {
  return value
    .split("\n")
    .map((line) => line.trim().replace(/^[-*]\s*/, ""))
    .filter(Boolean)
}
