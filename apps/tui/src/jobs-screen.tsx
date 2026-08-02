/** @jsxImportSource @opentui/react */

import { useEffect, useState } from "react"

import type { DaemonConnection } from "./daemon"
import { listProjects } from "./projects"
import { listWorkflowJobs, type WorkflowJob, type WorkflowStatus } from "./workflow-jobs"

type JobEntry = {
  projectName: string
  job: WorkflowJob
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
          projects.map(async (project) => ({
            project,
            jobs: await listWorkflowJobs(project.id),
          })),
        )
        return grouped
          .flatMap(({ project, jobs }) => jobs.map((job) => ({ projectName: project.name, job })))
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
        Business state is persisted separately from the durable dispatch queue.
      </text>
      {state === "loading" ? <text fg="#FACC15">Loading workflow jobs...</text> : null}
      {message ? <text fg={state === "error" ? "#F87171" : "#94A3B8"}>{message}</text> : null}
      {entries.slice(0, 20).map(({ projectName, job }) => (
        <box
          key={job.id}
          flexDirection="column"
          borderStyle="rounded"
          borderColor="#334155"
          padding={1}
        >
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
              ? `Dispatch ${job.current_dispatch_id.slice(0, 12)} is durable and awaiting a registered handler.`
              : "No pending dispatch."}
          </text>
          {job.failure_message ? <text fg="#F87171">{job.failure_message}</text> : null}
        </box>
      ))}
      {entries.length > 20 ? (
        <text fg="#64748B">{`${entries.length - 20} older jobs are not shown.`}</text>
      ) : null}
    </box>
  )
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
