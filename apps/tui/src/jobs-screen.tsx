/** @jsxImportSource @opentui/react */

import type { TextareaRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { type RefObject, useCallback, useEffect, useRef, useState } from "react"

import {
  type ApprovalDecision,
  type ApprovalKind,
  listApprovalDecisions,
  submitApprovalDecision,
} from "./approvals"
import { type CheckRun, type CheckStep, listCheckSteps, listChecks } from "./checks"
import type { DaemonConnection } from "./daemon"
import { type Execution, listCheckpoints, listExecutions, type PatchCheckpoint } from "./executions"
import { listProjects } from "./projects"
import {
  listReviewIssues,
  listReviews,
  type ReviewIssue,
  type ReviewRun,
  type ReviewSeverity,
} from "./reviews"
import { colors, EmptyState, Metric, PageIntro, Panel, ShortcutBar, StatusBadge } from "./ui"
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
      setMessage(
        "Select a workflow that is waiting for approval and has a completed review snapshot.",
      )
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
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageIntro
        kicker="WORKFLOW CONTROL"
        title="Versioned workflow jobs"
        description="Review execution evidence, inspect the immutable patch fingerprint, and make a deliberate human decision."
        meta={entries.length > 0 ? `${selectedIndex + 1} / ${entries.length}` : "no jobs"}
      />
      {state === "loading" ? (
        <Panel title="JOB REGISTRY" borderColor={colors.accent}>
          <text fg={colors.warning}>Loading workflow jobs...</text>
          <text fg={colors.dim}>Collecting executions, checks, reviews, and decisions.</text>
        </Panel>
      ) : null}
      {state === "error" ? (
        <Panel title="JOB REGISTRY" borderColor={colors.danger} backgroundColor="#24151A">
          <text fg={colors.danger}>Unable to load workflow jobs.</text>
          <text fg={colors.muted}>{message}</text>
          <ShortcutBar shortcuts={[{ key: "R", label: "reload" }]} />
        </Panel>
      ) : null}
      {state !== "loading" && state !== "error" && entries.length === 0 ? (
        <EmptyState
          title="No workflow job yet"
          detail="Start a job from the API or project workflow to see its execution evidence here."
          action="R reload"
        />
      ) : null}
      {entries.length > 0 ? (
        <box flexDirection="row" flexGrow={1} gap={1}>
          <Panel width="32%" title={`JOBS  ${entries.length}`} borderColor={colors.lineStrong}>
            {entries.slice(0, 20).map((entry, index) => (
              <JobListItem key={entry.job.id} entry={entry} selected={index === selectedIndex} />
            ))}
            {entries.length > 20 ? (
              <text fg={colors.dim}>{`${entries.length - 20} older jobs hidden`}</text>
            ) : null}
          </Panel>
          <box flexGrow={1} flexDirection="column">
            {selectedEntry ? <JobCard entry={selectedEntry} selected /> : null}
          </box>
        </box>
      ) : null}
      {message && state !== "error" && entries.length > 0 ? (
        <text fg={colors.muted}>{message}</text>
      ) : null}
      <ShortcutBar
        shortcuts={[
          { key: "↑↓", label: "select job" },
          { key: "A", label: "approve" },
          { key: "V", label: "revision" },
          { key: "X", label: "reject" },
          { key: "R", label: "reload" },
        ]}
      />
    </box>
  )
}

function JobListItem({ entry, selected }: { entry: JobEntry; selected: boolean }) {
  return (
    <box
      flexDirection="column"
      gap={0}
      padding={1}
      backgroundColor={selected ? colors.surfaceAccent : colors.surface}
      borderStyle="rounded"
      borderColor={selected ? colors.accent : colors.line}
    >
      <text fg={selected ? colors.accent : colors.text}>
        {`${selected ? "›" : " "} ${entry.projectName}`}
      </text>
      <text fg={colors.dim}>{`v${entry.job.version} / exec ${entry.job.execution_version}`}</text>
      <text fg={statusColor(entry.job.status)}>{entry.job.status}</text>
      {entry.review ? (
        <text fg={colors.dim}>{`review ${entry.review.verdict || entry.review.status}`}</text>
      ) : null}
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
  noteRef: RefObject<TextareaRenderable | null>
  reviewOverride: boolean
  submitting: boolean
  message: string
  onNoteChange: (value: string) => void
}) {
  const { job, execution, checkpoint, review } = entry

  useEffect(() => {
    noteRef.current?.focus()
  }, [noteRef])

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageIntro
        kicker="HUMAN DECISION"
        title={`Human decision: ${kind}`}
        description={`${entry.projectName} · workflow v${job.version} · execution v${execution?.execution_version ?? 0}`}
        meta="review before applying"
      />
      <box flexDirection="row" flexGrow={1} gap={1}>
        <Panel
          width="43%"
          title="IMMUTABLE FINGERPRINT"
          borderColor={colors.warning}
          backgroundColor="#211E14"
        >
          <text fg={colors.warning}>Verify immutable approval fingerprint</text>
          <Metric label="Execution" value={`v${execution?.execution_version ?? 0}`} tone="accent" />
          <text fg={colors.dim}>Base commit</text>
          <text fg={colors.muted}>{execution?.base_commit_sha ?? "missing"}</text>
          <text fg={colors.dim}>Patch checksum</text>
          <text fg={colors.muted}>{checkpoint?.patch_checksum ?? "missing"}</text>
          <StatusBadge
            label={`Reviewer ${review?.verdict ?? "missing"} · ${review?.blocking_issues ?? 0} blocking`}
            tone={review?.verdict === "APPROVE" ? "success" : "warning"}
          />
        </Panel>
        <Panel
          flexGrow={1}
          title="DECISION NOTE"
          borderColor={colors.accent}
          backgroundColor={colors.surfaceAccent}
        >
          <text fg={colors.accent}>
            {kind === "REQUEST_REVISION"
              ? "Revision instructions"
              : kind === "REJECT"
                ? "Rejection reason"
                : "Approval note"}
          </text>
          <textarea
            ref={noteRef}
            width="100%"
            height={9}
            initialValue={note}
            placeholder={
              kind === "APPROVE"
                ? "Optional unless overriding a REQUEST_REVISION verdict"
                : "Explain the required change or rejection reason"
            }
            focused
            wrapMode="word"
            backgroundColor={colors.surface}
            focusedBackgroundColor={colors.surfaceRaised}
            onContentChange={() => onNoteChange(noteRef.current?.plainText ?? "")}
          />
          {kind === "APPROVE" && review?.verdict === "REQUEST_REVISION" ? (
            <text fg={reviewOverride ? colors.success : colors.danger}>
              {`Reviewer override: ${reviewOverride ? "ACKNOWLEDGED" : "NOT ACKNOWLEDGED"} · press O to toggle`}
            </text>
          ) : null}
        </Panel>
      </box>
      {message ? <text fg={submitting ? colors.warning : colors.danger}>{message}</text> : null}
      <ShortcutBar
        shortcuts={[
          { key: "Ctrl+S", label: "apply decision" },
          { key: "Esc", label: "cancel" },
          { key: "O", label: "reviewer override" },
        ]}
      />
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
      borderColor={selected ? colors.accent : colors.line}
      backgroundColor={colors.surfaceRaised}
      padding={1}
    >
      <box flexDirection="row" justifyContent="space-between">
        <text
          fg={selected ? colors.accent : colors.text}
        >{`${selected ? "› " : ""}${projectName}`}</text>
        <StatusBadge label={job.status} tone={workflowTone(job.status)} />
      </box>
      <text fg={colors.muted}>
        {`${job.id.slice(0, 8)} · workflow v${job.version} · exec v${job.execution_version}`}
      </text>
      <text fg={colors.muted}>
        {`${job.base_branch}@${job.base_commit_sha.slice(0, 8)} · rev ${job.revision_count}/${job.limits.max_revisions}`}
      </text>
      <text fg={colors.dim}>EXECUTION</text>
      <text fg={execution?.status === "FAILED" ? colors.danger : colors.accent}>
        {execution
          ? `Execution v${execution.execution_version} ${execution.status}`
          : "Execution pending"}
      </text>
      <text fg={colors.dim}>
        {execution
          ? `${execution.tool_calls}/${job.limits.max_tool_calls} calls · ${execution.provider || "daemon"}`
          : "No execution evidence yet"}
      </text>
      {checkpoint ? (
        <text fg={colors.dim}>{`Patch ${checkpoint.patch_checksum.slice(0, 12)}`}</text>
      ) : null}
      <text fg={colors.dim}>CHECKS</text>
      {check ? (
        <text
          fg={checkStatusColor(check.status)}
        >{`Checks v${check.execution_version} ${check.status}`}</text>
      ) : (
        <text fg={colors.dim}>Checks have not started.</text>
      )}
      {check ? (
        <text
          fg={colors.muted}
        >{`${check.passed_steps} passed · ${check.failed_steps} failed · ${checkSteps.length} steps`}</text>
      ) : null}
      <text fg={colors.dim}>REVIEW</text>
      {review ? (
        <text
          fg={reviewStatusColor(review)}
        >{`Review v${review.execution_version} ${review.status}`}</text>
      ) : (
        <text fg={colors.dim}>Review has not started.</text>
      )}
      {review ? (
        <text
          fg={colors.muted}
        >{`${review.blocking_issues}/${review.total_issues} blocking${review.verdict ? ` · ${review.verdict}` : ""}`}</text>
      ) : null}
      {review?.summary ? <text fg={colors.dim}>{review.summary.slice(0, 64)}</text> : null}
      {reviewIssues.length > 0 ? (
        <text fg={severityColor(reviewIssues[0]?.severity ?? "LOW")}>
          {`${reviewIssues.length} review issue${reviewIssues.length === 1 ? "" : "s"}`}
        </text>
      ) : null}
      <text fg={colors.dim}>DECISION</text>
      {decision ? (
        <text fg={decision.status === "APPLIED" ? colors.success : colors.warning}>
          {`Human decision ${decision.decision}`}
        </text>
      ) : (
        <text fg={colors.warning}>Awaiting human decision</text>
      )}
      {decision ? (
        <text fg={decision.status === "APPLIED" ? colors.success : colors.muted}>
          {`${decision.status} · execution v${decision.execution_version}`}
        </text>
      ) : null}
      {decision?.review_override ? (
        <text fg={colors.warning}>Reviewer override recorded.</text>
      ) : null}
      <text fg={colors.dim}>
        {workspace
          ? `Workspace ${workspace.status} · ${workspace.head_sha?.slice(0, 8) ?? "no HEAD"}`
          : "Workspace not requested"}
      </text>
      {job.failure_message ? (
        <text fg={colors.danger}>{job.failure_message.slice(0, 80)}</text>
      ) : null}
    </box>
  )
}

function statusColor(status: WorkflowStatus): string {
  switch (status) {
    case "COMPLETED":
    case "APPROVED":
      return colors.success
    case "FAILED":
    case "REJECTED":
    case "CANCELLED":
      return colors.danger
    case "WAITING_FOR_APPROVAL":
    case "REVISION_REQUIRED":
      return colors.warning
    default:
      return colors.accent
  }
}

function workflowTone(
  status: WorkflowStatus,
): "neutral" | "accent" | "success" | "warning" | "danger" {
  switch (status) {
    case "COMPLETED":
    case "APPROVED":
      return "success"
    case "FAILED":
    case "REJECTED":
    case "CANCELLED":
      return "danger"
    case "WAITING_FOR_APPROVAL":
    case "REVISION_REQUIRED":
      return "warning"
    default:
      return "accent"
  }
}

function checkStatusColor(status: CheckRun["status"]): string {
  switch (status) {
    case "PASSED":
      return colors.success
    case "FAILED":
    case "CANCELLED":
      return colors.danger
    case "RUNNING":
      return colors.warning
    default:
      return colors.accent
  }
}

function reviewStatusColor(review: ReviewRun): string {
  if (review.status === "FAILED" || review.status === "CANCELLED") {
    return colors.danger
  }
  if (review.verdict === "APPROVE") {
    return colors.success
  }
  if (review.verdict === "REQUEST_REVISION" || review.status === "RUNNING") {
    return colors.warning
  }
  return colors.accent
}

function severityColor(severity: ReviewSeverity): string {
  switch (severity) {
    case "CRITICAL":
    case "HIGH":
      return colors.danger
    case "MEDIUM":
      return colors.warning
    default:
      return colors.muted
  }
}
