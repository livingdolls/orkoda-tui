/** @jsxImportSource @opentui/react */

import { useEffect, useState } from "react"

import { type CheckRun, type CheckStep, listCheckSteps, listChecks } from "./checks"
import type { DaemonConnection } from "./daemon"
import { type Execution, listExecutions } from "./executions"
import { listProjects } from "./projects"
import {
  listReviewIssues,
  listReviews,
  type ReviewIssue,
  type ReviewRun,
  type ReviewSeverity,
} from "./reviews"
import {
  listProjectWorkspaces,
  listWorkflowJobs,
  type WorkflowJob,
  type WorkflowStatus,
  type Workspace,
} from "./workflow-jobs"

type JobEntry = {
  projectName: string
  job: WorkflowJob
  workspace?: Workspace
  execution?: Execution
  check?: CheckRun
  checkSteps: CheckStep[]
  review?: ReviewRun
  reviewIssues: ReviewIssue[]
}

export function JobsScreen({ connection }: { connection: DaemonConnection }) {
  const [entries, setEntries] = useState<JobEntry[]>([])
  const [state, setState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [message, setMessage] = useState("")

  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected") {
      setEntries([])
      setState("idle")
      setMessage("Start the daemon before loading workflow jobs.")
      return
    }

    setState("loading")
    setMessage("")
    void listProjects()
      .then(async (projects) => {
        const grouped = await Promise.all(
          projects.map(async (project) => {
            const [jobs, workspaces] = await Promise.all([
              listWorkflowJobs(project.id),
              listProjectWorkspaces(project.id),
            ])
            const workspaceByJob = new Map(
              workspaces.map((workspace) => [workspace.workflow_job_id, workspace]),
            )
            return Promise.all(
              jobs.map(async (job) => {
                const [executions, checkRuns, reviewRuns] = await Promise.all([
                  listExecutions(job.id),
                  listChecks(job.id),
                  listReviews(job.id),
                ])
                const check = checkRuns[0]
                const review = reviewRuns[0]
                const [checkSteps, reviewIssues] = await Promise.all([
                  check ? listCheckSteps(check.id) : Promise.resolve([]),
                  review ? listReviewIssues(review.id) : Promise.resolve([]),
                ])
                return {
                  projectName: project.name,
                  job,
                  workspace: workspaceByJob.get(job.id),
                  execution: executions[0],
                  check,
                  checkSteps,
                  review,
                  reviewIssues,
                }
              }),
            )
          }),
        )
        return grouped
          .flat()
          .sort(
            (left, right) =>
              new Date(right.job.updated_at).getTime() - new Date(left.job.updated_at).getTime(),
          )
      })
      .then((jobs) => {
        if (!disposed) {
          setEntries(jobs)
          setState("ready")
          setMessage(jobs.length === 0 ? "No workflow job has been created." : "")
        }
      })
      .catch((error) => {
        if (!disposed) {
          setEntries([])
          setState("error")
          setMessage(error instanceof Error ? error.message : "Failed to load workflow jobs")
        }
      })

    return () => {
      disposed = true
    }
  }, [connection.state])

  return (
    <box flexDirection="column" gap={1}>
      <text fg="#E2E8F0">Versioned workflow jobs</text>
      <text fg="#64748B">
        Business state, execution snapshots, durable dispatch, checks, reviews, and isolated
        workspaces are persisted separately.
      </text>
      {state === "loading" ? <text fg="#FACC15">Loading workflow jobs...</text> : null}
      {message ? <text fg={state === "error" ? "#F87171" : "#94A3B8"}>{message}</text> : null}
      {entries.slice(0, 20).map((entry) => (
        <JobCard key={entry.job.id} entry={entry} />
      ))}
      {entries.length > 20 ? (
        <text fg="#64748B">{`${entries.length - 20} older jobs are not shown.`}</text>
      ) : null}
    </box>
  )
}

function JobCard({ entry }: { entry: JobEntry }) {
  const { projectName, job, workspace, execution, check, checkSteps, review, reviewIssues } = entry
  return (
    <box flexDirection="column" borderStyle="rounded" borderColor="#334155" padding={1}>
      <box flexDirection="row" justifyContent="space-between">
        <text fg="#E2E8F0">{projectName}</text>
        <text fg={statusColor(job.status)}>{job.status}</text>
      </box>
      <text fg="#94A3B8">
        {`${job.id.slice(0, 12)} • workflow v${job.version} • execution v${job.execution_version}`}
      </text>
      <text fg="#94A3B8">
        {`${job.base_branch}@${job.base_commit_sha.slice(0, 12)} • revisions ${job.revision_count}/${job.limits.max_revisions}`}
      </text>
      <text fg="#64748B">
        {job.current_dispatch_id
          ? `Dispatch ${job.current_dispatch_id.slice(0, 12)} is durable.`
          : "No pending dispatch."}
      </text>
      {execution ? (
        <box flexDirection="column">
          <text fg={execution.status === "FAILED" ? "#F87171" : "#7DD3FC"}>
            {`Execution v${execution.execution_version} ${execution.status} • ${execution.tool_calls}/${job.limits.max_tool_calls} tool calls`}
          </text>
          <text fg="#64748B">
            {`${execution.provider || "daemon default"}/${execution.model || "daemon default"} • settings v${execution.agent_settings_version}`}
          </text>
          {execution.failure_message ? <text fg="#F87171">{execution.failure_message}</text> : null}
        </box>
      ) : (
        <text fg="#64748B">Execution has not started.</text>
      )}
      {check ? (
        <box flexDirection="column">
          <text fg={checkStatusColor(check.status)}>
            {`Checks v${check.execution_version} ${check.status} • ${check.passed_steps} passed • ${check.failed_steps} failed`}
          </text>
          {checkSteps.map((step) => (
            <text key={step.id} fg={checkStatusColor(step.status)}>
              {`${step.profile}: ${step.status}${step.exit_code === undefined ? "" : ` (exit ${step.exit_code})`} • ${step.duration_ms} ms${step.output_truncated ? " • output truncated" : ""}`}
            </text>
          ))}
        </box>
      ) : (
        <text fg="#64748B">Checks have not started.</text>
      )}
      {review ? (
        <box flexDirection="column">
          <text fg={reviewStatusColor(review)}>
            {`Review v${review.execution_version} ${review.status}${review.verdict ? ` • ${review.verdict}` : ""} • ${review.blocking_issues}/${review.total_issues} blocking`}
          </text>
          {review.summary ? <text fg="#94A3B8">{review.summary}</text> : null}
          {reviewIssues.slice(0, 5).map((issue) => (
            <text key={issue.id} fg={severityColor(issue.severity)}>
              {`${issue.blocking ? "BLOCKING " : ""}${issue.severity} ${issue.category}: ${issue.title}${formatIssueLocation(issue)}`}
            </text>
          ))}
          {reviewIssues.length > 5 ? (
            <text fg="#64748B">{`${reviewIssues.length - 5} more review issues are not shown.`}</text>
          ) : null}
          {review.failure_message ? <text fg="#F87171">{review.failure_message}</text> : null}
        </box>
      ) : (
        <text fg="#64748B">Review has not started.</text>
      )}
      {workspace ? (
        <box flexDirection="column">
          <text fg={workspace.status === "FAILED" ? "#F87171" : "#7DD3FC"}>
            {`Workspace ${workspace.status} • ${workspace.head_sha?.slice(0, 12) ?? "no HEAD"}${workspace.dirty ? " • dirty" : ""}`}
          </text>
          <text fg="#64748B">{workspace.path}</text>
          {workspace.lease_owner ? (
            <text fg="#FACC15">
              {`Lease ${workspace.lease_owner} until ${workspace.lease_expires_at ?? "unknown"}`}
            </text>
          ) : null}
          {workspace.failure_message ? <text fg="#F87171">{workspace.failure_message}</text> : null}
        </box>
      ) : (
        <text fg="#64748B">Workspace has not been requested.</text>
      )}
      {job.failure_message ? <text fg="#F87171">{job.failure_message}</text> : null}
    </box>
  )
}

function formatIssueLocation(issue: ReviewIssue): string {
  if (!issue.file_path) {
    return ""
  }
  if (!issue.line_start) {
    return ` • ${issue.file_path}`
  }
  const end = issue.line_end && issue.line_end !== issue.line_start ? `-${issue.line_end}` : ""
  return ` • ${issue.file_path}:${issue.line_start}${end}`
}

function statusColor(status: WorkflowStatus): string {
  switch (status) {
    case "COMPLETED":
    case "APPROVED":
      return "#4ADE80"
    case "FAILED":
    case "REJECTED":
    case "CANCELLED":
      return "#F87171"
    case "WAITING_FOR_APPROVAL":
    case "REVISION_REQUIRED":
      return "#FACC15"
    default:
      return "#7DD3FC"
  }
}

function checkStatusColor(status: CheckRun["status"]): string {
  switch (status) {
    case "PASSED":
      return "#4ADE80"
    case "FAILED":
    case "CANCELLED":
      return "#F87171"
    case "RUNNING":
      return "#FACC15"
    default:
      return "#7DD3FC"
  }
}

function reviewStatusColor(review: ReviewRun): string {
  if (review.status === "FAILED" || review.status === "CANCELLED") {
    return "#F87171"
  }
  if (review.verdict === "APPROVE") {
    return "#4ADE80"
  }
  if (review.verdict === "REQUEST_REVISION" || review.status === "RUNNING") {
    return "#FACC15"
  }
  return "#7DD3FC"
}

function severityColor(severity: ReviewSeverity): string {
  switch (severity) {
    case "CRITICAL":
    case "HIGH":
      return "#F87171"
    case "MEDIUM":
      return "#FACC15"
    default:
      return "#94A3B8"
  }
}
