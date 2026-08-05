import { type AgentConfig, type AgentSettings, getAgentSettings } from "./agent-settings"
import { type CheckRun, listChecks } from "./checks"
import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"
import { type Execution, listExecutions } from "./executions"
import { listPlans, type Plan } from "./plans"
import { listProjects, type Project } from "./projects"
import {
  isReviewRelevant,
  type ReviewColumn,
  resolveReviewColumn,
  reviewCardStatus,
} from "./review-board-model"
import { listReviewIssues, listReviews, type ReviewIssue, type ReviewRun } from "./reviews"
import { listWorkflowJobs, type WorkflowJob } from "./workflow-jobs"

export type ReviewAgentAssignment = {
  provider: string
  model: string
  settingsVersion?: number
}

export type ReviewBoardCard = {
  id: string
  project: Project
  plan: Plan
  workflow: WorkflowJob
  execution?: Execution
  check?: CheckRun
  review?: ReviewRun
  issues: ReviewIssue[]
  previousReview?: ReviewRun
  previousIssues: ReviewIssue[]
  executor: ReviewAgentAssignment
  reviewer: ReviewAgentAssignment
  column: ReviewColumn
  status: string
  updatedAt: string
}

function configuredAgent(
  settings: AgentSettings | undefined,
  role: AgentConfig["role"],
): AgentConfig | undefined {
  return settings?.agents.find((agent) => agent.role === role)
}

function assignment(
  snapshot: { provider?: string; model?: string; agent_settings_version?: number } | undefined,
  configured: AgentConfig | undefined,
): ReviewAgentAssignment {
  return {
    provider: snapshot?.provider || configured?.provider || "daemon default",
    model: snapshot?.model || configured?.model || "daemon default",
    settingsVersion: snapshot?.agent_settings_version,
  }
}

type BackendReviewBoardCard = {
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

export async function loadProjectReviewCards(project: Project): Promise<ReviewBoardCard[]> {
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

async function loadProjectCardsFallback(project: Project): Promise<ReviewBoardCard[]> {
  const [plans, jobs, settings] = await Promise.all([
    listPlans(project.id),
    listWorkflowJobs(project.id),
    getAgentSettings(project.id).catch(() => undefined),
  ])
  const planByID = new Map(plans.map((plan) => [plan.id, plan]))
  const relevant = jobs.filter(isReviewRelevant)
  return Promise.all(
    relevant.map(async (workflow) => {
      const [executions, checks, reviews] = await Promise.all([
        listExecutions(workflow.id).catch(() => []),
        listChecks(workflow.id).catch(() => []),
        listReviews(workflow.id).catch(() => []),
      ])
      const execution = executions[0]
      const check = checks[0]
      const review = reviews[0]
      const previousReview = reviews[1]
      const [issues, previousIssues] = await Promise.all([
        review ? listReviewIssues(review.id).catch(() => []) : Promise.resolve([]),
        previousReview ? listReviewIssues(previousReview.id).catch(() => []) : Promise.resolve([]),
      ])
      const plan = planByID.get(workflow.plan_id)
      if (!plan) throw new Error(`Workflow ${workflow.id} references an unavailable plan`)
      return {
        id: `review:${workflow.id}`,
        project,
        plan,
        workflow,
        execution,
        check,
        review,
        issues,
        previousReview,
        previousIssues,
        executor: assignment(execution, configuredAgent(settings, "EXECUTOR")),
        reviewer: assignment(review, configuredAgent(settings, "REVIEWER")),
        column: resolveReviewColumn(workflow, review),
        status: reviewCardStatus(workflow, review),
        updatedAt:
          review?.updated_at ?? check?.updated_at ?? execution?.updated_at ?? workflow.updated_at,
      }
    }),
  )
}

export async function loadReviewBoard(): Promise<{
  projects: Project[]
  cards: ReviewBoardCard[]
}> {
  const projects = await listProjects()
  const cards = (await Promise.all(projects.map(loadProjectReviewCards))).flat()
  cards.sort(
    (left, right) => new Date(right.updatedAt).getTime() - new Date(left.updatedAt).getTime(),
  )
  return { projects, cards }
}
