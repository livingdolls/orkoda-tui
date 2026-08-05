from pathlib import Path


def replace_once(path: Path, old: str, new: str) -> None:
    text = path.read_text()
    if old not in text:
        raise SystemExit(f"marker not found in {path}: {old[:120]!r}")
    path.write_text(text.replace(old, new, 1))


router = Path("internal/httpapi/router.go")
replace_once(
    router,
    "\tregisterReviewRoutes(api, services.Reviews)\n\tregisterApprovalRoutes(api, services.Approvals)\n",
    "\tregisterReviewRoutes(api, services.Reviews)\n\tregisterReviewBoardRoutes(\n\t\tapi, services.Plans, services.WorkflowJobs, services.AgentSettings,\n\t\tservices.Executions, services.Checks, services.Reviews, services.Approvals,\n\t)\n\tregisterApprovalRoutes(api, services.Approvals)\n",
)

llm = Path("internal/config/llm.go")
text = llm.read_text()
text = text.replace('fallback.Provider + "\\\\x00" + fallback.Model', 'fallback.Provider + "\\x00" + fallback.Model')
llm.write_text(text)

board = Path("apps/tui/src/review-board-data.ts")
replace_once(
    board,
    'import { type CheckRun, listChecks } from "./checks"\n',
    'import { type CheckRun, listChecks } from "./checks"\nimport { daemonBaseURL, requestWithDaemonAuth } from "./daemon"\n',
)
replace_once(
    board,
    "async function loadProjectCards(project: Project): Promise<ReviewBoardCard[]> {\n",
    "async function loadProjectCardsFallback(project: Project): Promise<ReviewBoardCard[]> {\n",
)
marker = "async function loadProjectCardsFallback(project: Project): Promise<ReviewBoardCard[]> {\n"
wrapper = '''type BackendReviewBoardCard = {
  plan: Plan
  workflow: WorkflowJob
  execution?: Execution
  check?: CheckRun
  review?: ReviewRun
  issues: ReviewIssue[]
  previous_review?: ReviewRun
  previous_issues: ReviewIssue[]
  executor: { provider: string; model: string; settings_version?: number }
  reviewer: { provider: string; model: string; settings_version?: number }
  review_column: ReviewColumn
  updated_at: string
}

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }

async function requestProjectReviewBoard(projectID: string): Promise<BackendReviewBoardCard[]> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 7000)
  try {
    const response = await requestWithDaemonAuth(
      fetch,
      `${daemonBaseURL}/api/v1/projects/${projectID}/review-board`,
      { method: "GET", headers: { accept: "application/json" }, signal: controller.signal },
    )
    if (!response.ok) {
      let message = `Daemon returned HTTP ${response.status}`
      try {
        const payload = (await response.json()) as ErrorResponse
        message = payload.error?.message ?? message
      } catch {
        // Keep the status-based fallback for rolling upgrades and non-JSON failures.
      }
      throw new Error(message)
    }
    const payload = (await response.json()) as DataResponse<{ cards: BackendReviewBoardCard[] }>
    return payload.data.cards
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("Review board request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

async function loadProjectCards(project: Project): Promise<ReviewBoardCard[]> {
  try {
    const cards = await requestProjectReviewBoard(project.id)
    return cards.map((card) => ({
      id: `review:${card.workflow.id}`,
      project,
      plan: card.plan,
      workflow: card.workflow,
      execution: card.execution,
      check: card.check,
      review: card.review,
      issues: card.issues ?? [],
      previousReview: card.previous_review,
      previousIssues: card.previous_issues ?? [],
      executor: {
        provider: card.executor.provider,
        model: card.executor.model,
        settingsVersion: card.executor.settings_version,
      },
      reviewer: {
        provider: card.reviewer.provider,
        model: card.reviewer.model,
        settingsVersion: card.reviewer.settings_version,
      },
      column: card.review_column,
      status: reviewCardStatus(card.workflow, card.review),
      updatedAt: card.updated_at,
    }))
  } catch {
    return loadProjectCardsFallback(project)
  }
}

'''
replace_once(board, marker, wrapper + marker)
