/** @jsxImportSource @opentui/react */

import type { TextareaRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useCallback, useEffect, useRef, useState } from "react"

import {
  type ApprovalDecision,
  type ApprovalKind,
  listApprovalDecisions,
  submitApprovalDecision,
} from "./approvals"
import { type CheckRun, type CheckStep, listCheckSteps, listChecks } from "./checks"
import type { DaemonConnection } from "./daemon"
import {
  type Execution,
  type PatchCheckpoint,
  listCheckpoints,
  listExecutions,
} from "./executions"
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
  checkpoint?: PatchCheckpoint
  check?: CheckRun
  checkSteps: CheckStep[]
  review?: ReviewRun
  reviewIssues: ReviewIssue[]
  decision?: ApprovalDecision
}

type DecisionComposer = {
  kind: ApprovalKind
}

export function JobsScreen({
  connection,
  onInteractionChange,
}: {
  connection: DaemonConnection
  onInteractionChange?: (active: boolean) => void
}) {
  const [entries, setEntries] = useState<JobEntry[]>([])
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [state, setState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [message, setMessage] = useState("")
  const [composer, setComposer] = useState<DecisionComposer | null>(null)
  const [note, setNote] = useState("")
  const [reviewOverride, setReviewOverride] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const noteRef = useRef<TextareaRenderable>(null)

  const selectedEntry = entries[selectedIndex] ?? null

  const loadJobs = useCallback(async () => {
    if (connection.state !== "connected") {
      setEntries([])
      setState("idle")
      setMessage("Start the daemon before loading workflow jobs.")
      return
    }

    setState("loading")
    setMessage("")
    try {
      const projects = await listProjects()
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
            jobs.map(async (job): Promise<JobEntry> => {
              const [executions, checkRuns, reviewRuns, decisions] = await Promise.all([
                listExecutions(job.id),
                listChecks(job.id),
                listReviews(job.id),
                listApprovalDecisions(job.id),
              ])
              const execution = executions[0]
              const check = checkRuns[0]
              const review = reviewRuns[0]
              const [checkSteps, reviewIssues, checkpoints] = await Promise.all([
                check ? listCheckSteps(check.id) : Promise.resolve([]),
                review ? listReviewIssues(review.id) : Promise.resolve([]),
                execution ? listCheckpoints(execution.id) : Promise.resolve([]),
              ])
              return {
                projectName: project.name,
                job,
                workspace: workspaceByJob.get(job.id),
                execution,
                checkpoint: checkpoints.at(-1),
                check,
                checkSteps,
                review,
                reviewIssues,
                decision:
                  decisions.find((item) => item.execution_version === job.execution_version) ??
                  decisions[0],
              }
            }),
          )
        }),
      )
      const jobs = grouped
        .flat()
        .sort(
          (left, right) =>
            new Date(right.job.updated_at).getTime() - new Date(left.job.updated_at).getTime(),
        )
      setEntries(jobs)
      setSelectedIndex((current) => Math.min(current, Math.max(jobs.length - 1, 0)))
      setState("ready")
      setMessage(jobs.length === 0 ? "No workflow job has been created." : "")
    } catch (error) {
      setEntries([])
      setState("error")
      setMessage(error instanceof Error ? error.message : "Failed to load workflow jobs")
    }
  }, [connection.state])

  const openComposer = (kind: ApprovalKind) => {
    if (!selectedEntry || !canDecide(selectedEntry)) {
      setMessage("Select a workflow that is waiting for approval and has a completed review snapshot.")
      return
    }
    const existing = selectedEntry.decision
    if (existing?.status === "APPLIED") {
      setMessage(`Execution v${existing.execution_version} already has an applied decision.`)
      return
    }
    if (existing && existing.decision !== kind) {
      setMessage(`A pending ${existing.decision} decision already owns this execution snapshot.`)
      return
    }
    setComposer({ kind })
    setNote(existing?.note ?? "")
    setReviewOverride(existing?.review_override ?? false)
    setMessage("")
  }

  const closeComposer = () => {
    if (submitting) {
      return
    }
    setComposer(null)
    setNote("")
    setReviewOverride(false)
  }

  const submitDecision = async () => {
    if (!composer || !selectedEntry || submitting) {
      return
    }
    const { job, execution, checkpoint, review } = selectedEntry
    if (!execution || !checkpoint || !review) {
      setMessage("The approval snapshot is incomplete. Reload the workflow job.")
      return
    }
    const nextNote = noteRef.current?.plainText.trim() ?? note.trim()
    if ((composer.kind === "REQUEST_REVISION" || composer.kind === "REJECT") && !nextNote) {
      setMessage("A reason is required for revision or rejection.")
      return
    }
    if (composer.kind === "APPROVE" && review.verdict === "REQUEST_REVISION") {
      if (!reviewOverride || !nextNote) {
        setMessage("Toggle Reviewer override and explain why the blocking review is accepted.")
        return
      }
    }

    setSubmitting(true)
    setMessage(`Applying ${composer.kind} to execution v${execution.execution_version}...`)
    try {
      const outcome = await submitApprovalDecision(job.id, composer.kind, {
        expected_version: job.version,
        execution_version: execution.execution_version,
        base_commit_sha: execution.base_commit_sha,
        patch_checksum: checkpoint.patch_checksum,
        note: nextNote,
        review_override: reviewOverride,
      })
      setEntries((current) =>
        current.map((entry) =>
          entry.job.id === job.id
            ? { ...entry, job: outcome.workflow, decision: outcome.decision }
            : entry,
        ),
      )
      setComposer(null)
      setNote("")
      setReviewOverride(false)
      setMessage(
        `${outcome.decision.decision} applied to execution v${outcome.decision.execution_version}; workflow is ${outcome.workflow.status}.`,
      )
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to apply approval decision")
    } finally {
      setSubmitting(false)
    }
  }

  useKeyboard((key) => {
    if (composer) {
      if (key.name === "escape") {
        closeComposer()
        return
      }
      if (key.name === "o" && composer.kind === "APPROVE") {
        setReviewOverride((current) => !current)
        return
      }
      if (key.ctrl && key.name === "s") {
        void submitDecision()
      }
      return
    }
    if (state === "loading" || submitting) {
      return
    }
    if (key.name === "r") {
      void loadJobs()
      return
    }
    if ((key.name === "down" || key.name === "j") && entries.length > 0) {
      setSelectedIndex((current) => Math.min(current + 1, entries.length - 1))
      return
    }
    if ((key.name === "up" || key.name === "k") && entries.length > 0) {
      setSelectedIndex((current) => Math.max(current - 1, 0))
      return
    }
    if (key.name === "a") {
      openComposer("APPROVE")
      return
    }
    if (key.name === "v") {
      openComposer("REQUEST_REVISION")
      return
    }
    if (key.name === "x") {
      openComposer("REJECT")
    }
  })

  useEffect(() => {
    void loadJobs()
  }, [loadJobs])

  useEffect(() => {
    onInteractionChange?.(composer !== null)
    return () => onInteractionChange?.(false)
  }, [composer, onInteractionChange])

  if (composer && selectedEntry) {
    return (
      <DecisionPanel
        entry={selectedEntry}
        kind={composer.kind}
        note={note}
        noteRef={noteRef}
        reviewOverride={reviewOverride}
        submitting={submitting}
        message={message}
        onNoteChange={setNote}
      />
    )
  }

  return (
    <box flexDirection="column" gap={1}>
      <text fg="#E2E8F0">Versioned workflow jobs</text>
      <text fg="#64748B">
        Select a job with ↑↓/jk. Approval controls bind the decision to the displayed execution and
        patch fingerprint.
      </text>
      {state === "loading" ? <text fg="#FACC15">Loading workflow jobs...</text> : null}
      {message ? <text fg={state === "error" ? "#F87171" : "#94A3B8"}>{message}</text> : null}
      {entries.slice(0, 20).map((entry, index) => (
        <JobCard key={entry.job.id} entry={entry} selected={index === selectedIndex} />
      ))}
      {entries.length > 20 ? (
        <text fg="#64748B">{`${entries.length - 20} older jobs are not shown.`}</text>
      ) : null}
      <text fg="#64748B">
        ↑↓/jk select • a approve • v request revision • x reject • r reload
      </text>
    </box>
  )
}

function DecisionPanel({
  entry,
  kind,
  note,
  noteRef,
  reviewOverride,
  submitting,
  message,
  onNoteChange,
}: {
  entry: JobEntry
  kind: ApprovalKind
  note: string
  noteRef: React.RefObject<TextareaRenderable | null>
  reviewOverride: boolean
  submitting: boolean
  message: string
  onNoteChange: (value: string) => void
}) {
  const { job, execution, checkpoint, review } = entry
  return (
    <box flexDirection="column" gap={1}>
      <text fg="#E2E8F0">Human decision: {kind}</text>
      <text fg="#94A3B8">{`${entry.projectName} • ${job.id.slice(0, 12)} • workflow v${job.version}`}</text>
      <box flexDirection="column" borderStyle="rounded" borderColor="#FACC15" padding={1}>
        <text fg="#FACC15">Verify immutable approval fingerprint</text>
        <text fg="#94A3B8">{`Execution v${execution?.execution_version ?? 0}`}</text>
        <text fg="#94A3B8">{`Base ${execution?.base_commit_sha ?? "missing"}`}</text>
        <text fg="#94A3B8">{`Patch ${checkpoint?.patch_checksum ?? "missing"}`}</text>
        <text fg={review?.verdict === "APPROVE" ? "#4ADE80" : "#FACC15"}>
          {`Reviewer ${review?.verdict ?? "missing"} • ${review?.blocking_issues ?? 0} blocking issues`}
        </text>
      </box>
      <text fg="#7DD3FC">
        {kind === "REQUEST_REVISION"
          ? "Revision instructions"
          : kind === "REJECT"
            ? "Rejection reason"
            : "Approval note"}
      </text>
      <textarea
        ref={noteRef}
        width="100%"
        height={7}
        initialValue={note}
        placeholder={
          kind === "APPROVE"
            ? "Optional unless overriding a REQUEST_REVISION verdict"
            : "Explain the required change or rejection reason"
        }
        focused
        wrapMode="word"
        backgroundColor="#11182B"
        focusedBackgroundColor="#172036"
        onContentChange={() => onNoteChange(noteRef.current?.plainText ?? "")}
      />
      {kind === "APPROVE" && review?.verdict === "REQUEST_REVISION" ? (
        <text fg={reviewOverride ? "#4ADE80" : "#F87171"}>
          {`Reviewer override: ${reviewOverride ? "ACKNOWLEDGED" : "NOT ACKNOWLEDGED"} • press o to toggle`}
        </text>
      ) : null}
      {message ? <text fg={submitting ? "#FACC15" : "#F87171"}>{message}</text> : null}
      <text fg="#64748B">Ctrl+S apply bound decision • Esc cancel • o toggle override</text>
    </box>
  )
}

function canDecide(entry: JobEntry): boolean {
  return (
    entry.job.status === "WAITING_FOR_APPROVAL" &&
    entry.execution?.status === "COMPLETED" &&
    entry.review?.status === "COMPLETED" &&
    entry.checkpoint !== undefined
  )
}

function JobCard({ entry, selected }: { entry: JobEntry; selected: boolean }) {
  const {
    projectName,
    job,
    workspace,
    execution,
    checkpoint,
    check,
    checkSteps,
    review,
    reviewIssues,
    decision,
  } = entry
  return (
    <box
      flexDirection="column"
      borderStyle="rounded"
      borderColor={selected ? "#7DD3FC" : "#334155"}
      padding={1}
    >
      <box flexDirection="row" justifyContent="space-between">
        <text fg={selected ? "#7DD3FC" : "#E2E8F0"}>{`${selected ? "› " : ""}${projectName}`}</text>
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
          {checkpoint ? (
            <text fg="#64748B">{`Patch ${checkpoint.patch_checksum} • ${checkpoint.patch_bytes} bytes`}</text>
          ) : null}
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
      {decision ? (
        <box flexDirection="column">
          <text fg={decision.status === "APPLIED" ? "#4ADE80" : "#FACC15"}>
            {`Human decision ${decision.decision} • ${decision.status} • execution v${decision.execution_version}`}
          </text>
          <text fg="#64748B">{`Bound ${decision.base_commit_sha.slice(0, 12)} • ${decision.patch_checksum}`}</text>
          {decision.review_override ? <text fg="#FACC15">Reviewer verdict override recorded.</text> : null}
        </box>
      ) : job.status === "WAITING_FOR_APPROVAL" ? (
        <text fg="#FACC15">Awaiting a version-bound human decision.</text>
      ) : null}
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
