import { daemonBaseURL } from "./daemon"
import type { WorkflowJob } from "./workflow-jobs"

export type ApprovalKind = "APPROVE" | "REQUEST_REVISION" | "REJECT"
export type ApprovalStatus = "PENDING" | "APPLIED"

export type ApprovalDecision = {
  id: string
  workflow_job_id: string
  review_run_id: string
  execution_id: string
  execution_version: number
  checkpoint_id: string
  base_commit_sha: string
  patch_checksum: string
  decision: ApprovalKind
  status: ApprovalStatus
  note: string
  revision_instructions?: string
  review_override: boolean
  reviewer_verdict: "APPROVE" | "REQUEST_REVISION"
  workflow_version_before: number
  workflow_version_after?: number
  revision_count_before: number
  created_at: string
  applied_at?: string
  updated_at: string
}

export type ApprovalInput = {
  expected_version: number
  execution_version: number
  base_commit_sha: string
  patch_checksum: string
  note: string
  review_override: boolean
}

export type ApprovalOutcome = {
  decision: ApprovalDecision
  workflow: WorkflowJob
}

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }
export type ApprovalFetch = (
  input: string | URL | Request,
  init?: RequestInit,
) => Promise<Response>

async function request<T>(
  path: string,
  init: RequestInit,
  fetcher: ApprovalFetch,
): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 5000)
  try {
    const response = await fetcher(`${daemonBaseURL}${path}`, {
      ...init,
      headers: {
        accept: "application/json",
        ...(init.body ? { "content-type": "application/json" } : {}),
        ...init.headers,
      },
      signal: controller.signal,
    })
    if (!response.ok) {
      let message = `Daemon returned HTTP ${response.status}`
      try {
        const payload = (await response.json()) as ErrorResponse
        message = payload.error?.message ?? message
      } catch {
        // Keep the HTTP status for non-JSON errors.
      }
      throw new Error(message)
    }
    const payload = (await response.json()) as DataResponse<T>
    return payload.data
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("Approval request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

export function listApprovalDecisions(
  jobID: string,
  fetcher: ApprovalFetch = fetch,
): Promise<ApprovalDecision[]> {
  return request<ApprovalDecision[]>(`/api/v1/jobs/${jobID}/decisions`, { method: "GET" }, fetcher)
}

export function submitApprovalDecision(
  jobID: string,
  kind: ApprovalKind,
  input: ApprovalInput,
  fetcher: ApprovalFetch = fetch,
): Promise<ApprovalOutcome> {
  const route =
    kind === "APPROVE" ? "approve" : kind === "REQUEST_REVISION" ? "request-revision" : "reject"
  return request<ApprovalOutcome>(
    `/api/v1/jobs/${jobID}/${route}`,
    { method: "POST", body: JSON.stringify(input) },
    fetcher,
  )
}
