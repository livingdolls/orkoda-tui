import { type BoardItem, createBoardItem } from "./board-model"
import { daemonBaseURL, requestWithDaemonAuth } from "./daemon"
import { listPlans } from "./plans"
import { listProjects, type Project } from "./projects"
import { listWorkflowJobs, type WorkflowJob } from "./workflow-jobs"

type ProjectBoardJobs = {
  jobs: WorkflowJob[]
}

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }
type BoardFetch = (input: string | URL | Request, init?: RequestInit) => Promise<Response>

async function requestProjectBoardJobs(
  projectID: string,
  fetcher: BoardFetch = fetch,
): Promise<WorkflowJob[]> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 7000)
  try {
    const response = await requestWithDaemonAuth(
      fetcher,
      `${daemonBaseURL}/api/v1/projects/${projectID}/board`,
      {
        method: "GET",
        headers: { accept: "application/json" },
        signal: controller.signal,
      },
    )
    if (!response.ok) {
      let message = `Daemon returned HTTP ${response.status}`
      try {
        const payload = (await response.json()) as ErrorResponse
        message = payload.error?.message ?? message
      } catch {
        // Keep the status-based message for non-JSON responses.
      }
      throw new Error(message)
    }
    const payload = (await response.json()) as DataResponse<ProjectBoardJobs>
    return payload.data.jobs
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("Board request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

async function loadProjectBoard(project: Project) {
  const [plans, jobs] = await Promise.all([
    listPlans(project.id),
    requestProjectBoardJobs(project.id).catch(() => listWorkflowJobs(project.id)),
  ])
  return { project, plans, jobs }
}

export async function loadBoardItems(): Promise<{
  projects: Project[]
  items: BoardItem[]
}> {
  const projects = await listProjects()
  const summaries = await Promise.all(projects.map(loadProjectBoard))
  const items = summaries.flatMap((summary) => {
    const latestJobByPlan = new Map<string, WorkflowJob>()
    for (const job of summary.jobs) {
      const current = latestJobByPlan.get(job.plan_id)
      if (!current || new Date(job.updated_at).getTime() > new Date(current.updated_at).getTime()) {
        latestJobByPlan.set(job.plan_id, job)
      }
    }
    return summary.plans.map((plan) =>
      createBoardItem(summary.project, plan, latestJobByPlan.get(plan.id)),
    )
  })
  items.sort(
    (left, right) => new Date(right.updatedAt).getTime() - new Date(left.updatedAt).getTime(),
  )
  return { projects, items }
}
