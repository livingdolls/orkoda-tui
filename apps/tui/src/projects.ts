import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"

export type ProjectRepository = {
  id: string
  project_id: string
  local_path: string
  current_branch: string
  head_sha: string
  remote_url?: string
  dirty: boolean
  trust_level: "UNTRUSTED" | "TRUSTED" | "BLOCKED" | string
  ignore_policy: Record<string, unknown>
  submodules: Array<{ path: string; commit: string; url?: string }>
  created_at: string
  updated_at: string
}

export type RepositoryBranch = {
  name: string
  head_sha: string
  current: boolean
}

export type Project = {
  id: string
  name: string
  repositories: ProjectRepository[]
  created_at: string
  updated_at: string
}

type DataResponse<T> = {
  data: T
}

type ErrorResponse = {
  error?: {
    message?: string
  }
}

export type ProjectFetch = (input: string | URL | Request, init?: RequestInit) => Promise<Response>

async function request<T>(path: string, init: RequestInit, fetcher: ProjectFetch): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 5000)
  const headers = new Headers(init.headers)
  headers.set("accept", "application/json")
  if (init.body) {
    headers.set("content-type", "application/json")
  }

  try {
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
        // Keep the status-based message for non-JSON failures.
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
      throw new Error("Project request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

export function listProjects(fetcher: ProjectFetch = fetch): Promise<Project[]> {
  return request<Project[]>("/api/v1/projects", { method: "GET" }, fetcher)
}

export function createProject(
  name: string,
  repositoryPath: string,
  fetcher: ProjectFetch = fetch,
): Promise<Project> {
  return request<Project>(
    "/api/v1/projects",
    {
      method: "POST",
      body: JSON.stringify({ name, repository_path: repositoryPath }),
    },
    fetcher,
  )
}

export function deleteProject(projectID: string, fetcher: ProjectFetch = fetch): Promise<void> {
  return request<void>(`/api/v1/projects/${projectID}`, { method: "DELETE" }, fetcher)
}

export function refreshProject(projectID: string, fetcher: ProjectFetch = fetch): Promise<Project> {
  return request<Project>(`/api/v1/projects/${projectID}/refresh`, { method: "POST" }, fetcher)
}

export function listRepositoryBranches(
  repositoryID: string,
  fetcher: ProjectFetch = fetch,
): Promise<RepositoryBranch[]> {
  return request<RepositoryBranch[]>(
    `/api/v1/repositories/${repositoryID}/branches`,
    { method: "GET" },
    fetcher,
  )
}

export function updateRepositoryTrust(
  repositoryID: string,
  level: "UNTRUSTED" | "TRUSTED" | "BLOCKED",
  ignorePolicy: Record<string, unknown> = {},
  fetcher: ProjectFetch = fetch,
): Promise<ProjectRepository> {
  return request<ProjectRepository>(
    `/api/v1/repositories/${repositoryID}/trust`,
    { method: "POST", body: JSON.stringify({ level, ignore_policy: ignorePolicy }) },
    fetcher,
  )
}
