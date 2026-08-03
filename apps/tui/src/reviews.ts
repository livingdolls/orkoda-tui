import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"

export type ReviewStatus = "PENDING" | "RUNNING" | "COMPLETED" | "FAILED" | "CANCELLED"
export type ReviewVerdict = "APPROVE" | "REQUEST_REVISION"
export type ReviewSeverity = "CRITICAL" | "HIGH" | "MEDIUM" | "LOW"

export type ReviewRun = {
  id: string
  workflow_job_id: string
  execution_id: string
  execution_version: number
  check_run_id: string
  checkpoint_id: string
  agent_settings_version: number
  provider: string
  model: string
  status: ReviewStatus
  verdict?: ReviewVerdict
  summary: string
  total_issues: number
  blocking_issues: number
  failure_code?: string
  failure_message?: string
  created_at: string
  updated_at: string
}

export type ReviewIssue = {
  id: string
  review_run_id: string
  position: number
  key: string
  severity: ReviewSeverity
  category: string
  blocking: boolean
  title: string
  description: string
  file_path?: string
  line_start?: number
  line_end?: number
  criteria_refs: string[]
  created_at: string
}

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }
export type ReviewFetch = (input: string | URL | Request, init?: RequestInit) => Promise<Response>

async function request<T>(path: string, fetcher: ReviewFetch): Promise<T> {
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
        // Keep the HTTP status message for non-JSON responses.
      }
      throw new Error(message)
    }
    const payload = (await response.json()) as DataResponse<T>
    return payload.data
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("Review request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

export function listReviews(jobID: string, fetcher: ReviewFetch = fetch): Promise<ReviewRun[]> {
  return request<ReviewRun[]>(`/api/v1/jobs/${jobID}/reviews`, fetcher)
}

export function listReviewIssues(
  reviewID: string,
  fetcher: ReviewFetch = fetch,
): Promise<ReviewIssue[]> {
  return request<ReviewIssue[]>(`/api/v1/reviews/${reviewID}/issues`, fetcher)
}
