import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"

export type PlanningRunStatus =
  | "RUNNING"
  | "NEEDS_INPUT"
  | "COMPLETED"
  | "FAILED"
  | "CANCELLED"
  | "SUPERSEDED"

export type PlanningQuestionStatus = "OPEN" | "ANSWERED"

export type PlanningStep = {
  id: string
  title: string
  description: string
  affected_files: string[]
  acceptance_criteria: string[]
}

export type PlanningResult = {
  summary: string
  steps: PlanningStep[]
  open_questions: string[]
  risks: string[]
}

export type PlanningQuestion = {
  id: string
  run_id: string
  position: number
  question: string
  answer?: string
  status: PlanningQuestionStatus
  created_at: string
  answered_at?: string
}

export type PlanningUsage = {
  input_tokens: number
  output_tokens: number
  cached_input_tokens?: number
  total_tokens: number
  attempt_count?: number
  fallback_used?: boolean
  final_provider?: string
  final_model?: string
  estimated_input_tokens?: number
  estimated_tokens_spent?: number
  validation_attempts?: number
  validation_error_count?: number
  repair_used?: boolean
  redaction_count?: number
}

export type PlanningRun = {
  id: string
  plan_id: string
  plan_version_id: string
  planning_context_id: string
  parent_run_id?: string
  provider: string
  model: string
  status: PlanningRunStatus
  result?: PlanningResult
  questions: PlanningQuestion[]
  usage: PlanningUsage
  error_code?: string
  error_message?: string
  created_at: string
  updated_at: string
}

export type PlanningAnswer = {
  questionID: string
  answer: string
}

export type PlanningAgentFetch = (
  input: string | URL | Request,
  init?: RequestInit,
) => Promise<Response>

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }

async function request<T>(
  path: string,
  init: RequestInit,
  fetcher: PlanningAgentFetch,
): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 30000)

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
        // Preserve the HTTP fallback for non-JSON responses.
      }
      throw new Error(message)
    }
    const payload = (await response.json()) as DataResponse<T>
    return payload.data
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("Planning agent request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

export function startPlanningRun(
  planID: string,
  fetcher: PlanningAgentFetch = fetch,
): Promise<PlanningRun> {
  return request<PlanningRun>(
    `/api/v1/plans/${planID}/planning-runs`,
    {
      method: "POST",
      body: JSON.stringify({}),
    },
    fetcher,
  )
}

export function getCurrentPlanningRun(
  planID: string,
  fetcher: PlanningAgentFetch = fetch,
): Promise<PlanningRun> {
  return request<PlanningRun>(
    `/api/v1/plans/${planID}/planning-runs/current`,
    { method: "GET" },
    fetcher,
  )
}

export function answerPlanningRun(
  runID: string,
  answers: PlanningAnswer[],
  fetcher: PlanningAgentFetch = fetch,
): Promise<PlanningRun> {
  return request<PlanningRun>(
    `/api/v1/planning-runs/${runID}/answers`,
    {
      method: "POST",
      body: JSON.stringify({
        answers: answers.map((answer) => ({
          question_id: answer.questionID,
          answer: answer.answer,
        })),
      }),
    },
    fetcher,
  )
}
