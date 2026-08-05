import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"

export type WorkflowStatus =
  | "READY"
  | "WORKSPACE_PREPARING"
  | "QUEUED"
  | "EXECUTING"
  | "CHECKING"
  | "REVIEWING"
  | "WAITING_FOR_APPROVAL"
  | "REVISION_REQUIRED"
  | "APPROVED"
  | "PUBLISHING"
  | "COMPLETED"
  | "FAILED"
  | "REJECTED"
  | "CANCELLED"

export type WorkspaceStatus =
  | "REQUESTED"
  | "PREPARING"
  | "READY"
  | "WRITE_LOCKED"
  | "ARCHIVED"
  | "FAILED"

export type WorkflowLimits = {
  max_revisions: number
  max_stage_attempts: number
  max_executor_turns?: number
  max_tool_calls: number
  max_consecutive_tool_errors?: number
  max_no_progress_turns?: number
  wall_clock_seconds: number
}

export type WorkflowAgentSelection = {
  provider: string
  model: string
}

export type WorkflowJob = {
  id: string
  project_id: string
  plan_id: string
  plan_version_id: string
  repository_id: string
  base_branch: string
  base_commit_sha: string
  agent_settings_version?: number
  executor?: WorkflowAgentSelection
  reviewer?: WorkflowAgentSelection
  status: WorkflowStatus
  version: number
  current_dispatch_id?: string
  retry_status?: WorkflowStatus
  execution_version: number
  revision_count: number
  limits: WorkflowLimits
  cancellation_requested: boolean
  failure_code?: string
  failure_message?: string
  created_at: string
  updated_at: string
  completed_at?: string
}

export type Workspace = {
  id: string
  workflow_job_id: string
  project_id: string
  repository_id: string
  path: string
  base_commit_sha: string
  head_sha?: string
  status: WorkspaceStatus
  dirty: boolean
  lease_owner?: string
  lease_expires_at?: string
  failure_message?: string
  created_at: string
  updated_at: string
}

export type WorkspaceLease = {
  workspace: Workspace
  session_token: string
  expires_at?: string
}

export type WorkflowTransition = {
  sequence: number
  workflow_job_id: string
  from_status?: WorkflowStatus
  action: string
  to_status: WorkflowStatus
  workflow_version: number
  dispatch_job_id?: string
  details: Record<string, unknown>
  created_at: string
}

type DataResponse<T> = {
  data: T
}

type ErrorResponse = {
  error?: {
    message?: string
  }
}

export type WorkflowFetch = (input: string | URL | Request, init?: RequestInit) => Promise<Response>

async function request<T>(path: string, init: RequestInit, fetcher: WorkflowFetch): Promise<T> {
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
    const payload = (await response.json()) as DataResponse<T>
    return payload.data
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("Workflow request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

export function listWorkflowJobs(
  projectID: string,
  fetcher: WorkflowFetch = fetch,
): Promise<WorkflowJob[]> {
  return request<WorkflowJob[]>(`/api/v1/projects/${projectID}/jobs`, { method: "GET" }, fetcher)
}

export function getWorkflowJob(
  jobID: string,
  fetcher: WorkflowFetch = fetch,
): Promise<WorkflowJob> {
  return request<WorkflowJob>(`/api/v1/jobs/${jobID}`, { method: "GET" }, fetcher)
}

export function listProjectWorkspaces(
  projectID: string,
  fetcher: WorkflowFetch = fetch,
): Promise<Workspace[]> {
  return request<Workspace[]>(
    `/api/v1/projects/${projectID}/workspaces`,
    { method: "GET" },
    fetcher,
  )
}

export function getWorkflowWorkspace(
  jobID: string,
  fetcher: WorkflowFetch = fetch,
): Promise<Workspace> {
  return request<Workspace>(`/api/v1/jobs/${jobID}/workspace`, { method: "GET" }, fetcher)
}

export function takeOverWorkspace(
  jobID: string,
  clientID?: string,
  fetcher: WorkflowFetch = fetch,
): Promise<WorkspaceLease> {
  return request<WorkspaceLease>(
    `/api/v1/jobs/${jobID}/workspace/take-over`,
    {
      method: "POST",
      headers: clientID ? { "x-client-id": clientID } : undefined,
      body: JSON.stringify({}),
    },
    fetcher,
  )
}

export function releaseWorkspace(
  workspaceID: string,
  sessionToken: string,
  headSHA: string,
  dirty: boolean,
  fetcher: WorkflowFetch = fetch,
): Promise<Workspace> {
  return request<Workspace>(
    `/api/v1/workspaces/${workspaceID}/release`,
    {
      method: "POST",
      body: JSON.stringify({ session_token: sessionToken, head_sha: headSHA, dirty }),
    },
    fetcher,
  )
}

export function listWorkflowTransitions(
  jobID: string,
  fetcher: WorkflowFetch = fetch,
): Promise<WorkflowTransition[]> {
  return request<WorkflowTransition[]>(
    `/api/v1/jobs/${jobID}/transitions`,
    { method: "GET" },
    fetcher,
  )
}

export function createWorkflowJob(
  projectID: string,
  input: {
    plan_id: string
    repository_id: string
    base_branch?: string
    agent_settings_version?: number
    executor?: WorkflowAgentSelection
    reviewer?: WorkflowAgentSelection
    limits?: Partial<WorkflowLimits>
  },
  fetcher: WorkflowFetch = fetch,
): Promise<WorkflowJob> {
  return request<WorkflowJob>(
    `/api/v1/projects/${projectID}/jobs`,
    { method: "POST", body: JSON.stringify(input) },
    fetcher,
  )
}

export function performWorkflowAction(
  jobID: string,
  action: "start" | "cancel" | "retry" | "approve" | "request-revision" | "reject" | "publish",
  expectedVersion: number,
  details: Record<string, unknown> = {},
  fetcher: WorkflowFetch = fetch,
): Promise<WorkflowJob> {
  return request<WorkflowJob>(
    `/api/v1/jobs/${jobID}/${action}`,
    {
      method: "POST",
      body: JSON.stringify({ expected_version: expectedVersion, details }),
    },
    fetcher,
  )
}

export function continueWorkflow(
  jobID: string,
  expectedVersion: number,
  additionalExecutorTurns: 8 | 16,
  fetcher: WorkflowFetch = fetch,
): Promise<WorkflowJob> {
  return request<WorkflowJob>(
    `/api/v1/jobs/${jobID}/continue`,
    {
      method: "POST",
      body: JSON.stringify({
        expected_version: expectedVersion,
        additional_executor_turns: additionalExecutorTurns,
        details: { requested_by: "kanban-board" },
      }),
    },
    fetcher,
  )
}
