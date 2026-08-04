/** @jsxImportSource @opentui/react */

import type { ScrollBoxRenderable, TextareaRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { type RefObject, useCallback, useEffect, useRef, useState } from "react"

import {
  type ApprovalDecision,
  type ApprovalKind,
  listApprovalDecisions,
  submitApprovalDecision,
} from "./approvals"
import {
  type CheckRun,
  type CheckStep,
  getArtifactText,
  listCheckSteps,
  listChecks,
} from "./checks"
import type { DaemonConnection } from "./daemon"
import {
  type Execution,
  getExecutionDiff,
  listCheckpoints,
  listExecutions,
  type PatchCheckpoint,
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
  Banner,
  BOLD,
  Card,
  Chip,
  colors,
  EmptyState,
  KeyHints,
  PageHeader,
  Section,
  type StatusTone,
  truncate,
} from "./ui"
import {
  listProjectWorkspaces,
  listWorkflowJobs,
  releaseWorkspace,
  takeOverWorkspace,
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

type ManualWorkspaceLease = {
  workspaceID: string
  sessionToken: string
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
  const [manualLease, setManualLease] = useState<ManualWorkspaceLease | null>(null)
  const [manualLeaseBusy, setManualLeaseBusy] = useState(false)
  const noteRef = useRef<TextareaRenderable>(null)
  const listScrollRef = useRef<ScrollBoxRenderable>(null)
  const detailScrollRef = useRef<ScrollBoxRenderable>(null)

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

  const toggleManualWorkspaceLease = async () => {
    if (!selectedEntry?.workspace || manualLeaseBusy) {
      setMessage("Select a prepared workspace before taking it over for manual editing.")
      return
    }
    const item = selectedEntry.workspace
    setManualLeaseBusy(true)
    try {
      if (manualLease) {
        if (manualLease.workspaceID !== item.id) {
          setMessage("Release the active manual workspace lease before selecting another job.")
          return
        }
        const released = await releaseWorkspace(
          item.id,
          manualLease.sessionToken,
          item.head_sha ?? selectedEntry.job.base_commit_sha,
          item.dirty,
        )
        setEntries((current) =>
          current.map((entry) =>
            entry.workspace?.id === released.id ? { ...entry, workspace: released } : entry,
          ),
        )
        setManualLease(null)
        setMessage("Manual workspace lease released; the daemon can resume workflow work.")
        return
      }

      const lease = await takeOverWorkspace(item.workflow_job_id, `tui-${process.pid}`)
      setEntries((current) =>
        current.map((entry) =>
          entry.workspace?.id === lease.workspace.id
            ? { ...entry, workspace: lease.workspace }
            : entry,
        ),
      )
      setManualLease({ workspaceID: lease.workspace.id, sessionToken: lease.session_token })
      setMessage(`Manual lease active. Edit the isolated workspace at ${lease.workspace.path}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Workspace lease operation failed")
    } finally {
      setManualLeaseBusy(false)
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
    if (state === "loading" || submitting || manualLeaseBusy) {
      return
    }
    if (key.name === "r") {
      void loadJobs()
      return
    }
    if (key.name === "pageup") {
      listScrollRef.current?.scrollBy(-1, "viewport")
      detailScrollRef.current?.scrollBy(-1, "viewport")
      return
    }
    if (key.name === "pagedown") {
      listScrollRef.current?.scrollBy(1, "viewport")
      detailScrollRef.current?.scrollBy(1, "viewport")
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
      return
    }
    if (key.name === "e") {
      void toggleManualWorkspaceLease()
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
      <PageHeader
        title="Jobs"
        description="Versioned workflow jobs. Review what the agents changed, then approve, revise, or reject."
        meta={entries.length > 0 ? `${selectedIndex + 1} of ${entries.length}` : "no jobs"}
      />
      {state === "loading" ? (
        <Card>
          <text fg={colors.warning}>Loading workflow jobs...</text>
          <text fg={colors.faint}>Collecting executions, checks, reviews, and decisions.</text>
        </Card>
      ) : null}
      {state === "error" ? (
        <box flexDirection="column" gap={1}>
          <Banner tone="danger">
            <text fg={colors.danger} attributes={BOLD}>
              Unable to load workflow jobs
            </text>
            <text fg={colors.muted}>{message}</text>
          </Banner>
          <KeyHints shortcuts={[{ key: "R", label: "reload" }]} />
        </box>
      ) : null}
      {state !== "loading" && state !== "error" && entries.length === 0 ? (
        <EmptyState
          icon="◇"
          title="No workflow job yet"
          detail="Start a workflow from the Projects screen (press W there) and follow its progress here."
          shortcut={{ key: "R", label: "reload" }}
        />
      ) : null}
      {entries.length > 0 ? (
        <box flexDirection="row" flexGrow={1} gap={1}>
          <box
            width={26}
            flexDirection="column"
            backgroundColor={colors.raised}
            borderStyle="rounded"
            borderColor={colors.line}
          >
            <scrollbox ref={listScrollRef} flexGrow={1} scrollY={true} padding={1}>
              <box flexDirection="column">
                {entries.slice(0, 20).map((entry, index) => (
                  <JobListItem
                    key={entry.job.id}
                    entry={entry}
                    selected={index === selectedIndex}
                  />
                ))}
                {entries.length > 20 ? (
                  <text fg={colors.faint}>{`${entries.length - 20} older jobs hidden`}</text>
                ) : null}
              </box>
            </scrollbox>
          </box>
          <scrollbox ref={detailScrollRef} flexGrow={1} scrollY={true}>
            <box flexDirection="column" gap={1}>
              {selectedEntry ? (
                <JobDetail
                  entry={selectedEntry}
                  manualLease={manualLease?.workspaceID === selectedEntry.workspace?.id}
                />
              ) : null}
            </box>
          </scrollbox>
        </box>
      ) : null}
      {message && state !== "error" && entries.length > 0 ? (
        <text fg={colors.muted}>{message}</text>
      ) : null}
    </box>
  )
}

function JobListItem({ entry, selected }: { entry: JobEntry; selected: boolean }) {
  return (
    <box
      flexDirection="column"
      paddingLeft={1}
      paddingRight={1}
      backgroundColor={selected ? colors.accentTint : colors.raised}
    >
      <text fg={selected ? colors.text : colors.muted} attributes={selected ? BOLD : 0}>
        {`${selected ? "▸" : " "} ${truncate(entry.projectName, 19)}`}
      </text>
      <text
        fg={statusColor(entry.job.status)}
      >{`  ${truncate(workflowShortLabel(entry.job.status), 19)}`}</text>
    </box>
  )
}

function JobDetail({ entry, manualLease }: { entry: JobEntry; manualLease: boolean }) {
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
    <box flexDirection="column" gap={1} flexGrow={1}>
      <box flexDirection="row" gap={1} alignItems="center" flexWrap="wrap">
        <Chip label={workflowLabel(job.status)} tone={workflowTone(job.status)} />
        <text fg={colors.text} attributes={BOLD}>
          {projectName}
        </text>
        {manualLease ? <Chip label="manual edit active" tone="warning" /> : null}
      </box>
      <text fg={colors.faint}>
        {`${job.id.slice(0, 8)} · workflow v${job.version} · exec v${job.execution_version} · status ${job.status} · ${job.base_branch}@${job.base_commit_sha.slice(0, 8)} · revisions ${job.revision_count}/${job.limits.max_revisions}`}
      </text>

      <Section title="Pipeline">
        <Card>
          <PipelineRow
            marker={
              execution
                ? execution.status === "COMPLETED"
                  ? "done"
                  : execution.status === "FAILED"
                    ? "failed"
                    : "active"
                : "todo"
            }
            title={
              execution
                ? `Execution v${execution.execution_version} ${execution.status}`
                : "Execution pending"
            }
            detail={
              execution
                ? `${execution.tool_calls}/${job.limits.max_tool_calls} tool calls · ${execution.provider || "daemon"} · ${execution.usage?.total_tokens ?? 0} tokens${execution.usage?.estimated_cost_usd ? ` · $${execution.usage.estimated_cost_usd.toFixed(4)}` : ""}`
                : "The agents have not started yet"
            }
          />
          <PipelineRow
            marker={
              check
                ? check.status === "PASSED"
                  ? "done"
                  : check.status === "FAILED" || check.status === "CANCELLED"
                    ? "failed"
                    : "active"
                : "todo"
            }
            title={
              check
                ? `Checks v${check.execution_version} ${check.status}`
                : "Checks have not started"
            }
            detail={
              check
                ? `${check.passed_steps} passed · ${check.failed_steps} failed · ${checkSteps.length} steps`
                : "Automated checks run after the agents finish"
            }
          />
          {checkSteps.length > 0 ? (
            <box flexDirection="row" gap={2} flexWrap="wrap" paddingLeft={2}>
              {checkSteps.slice(0, 8).map((step) => (
                <text
                  key={step.id}
                  fg={
                    step.status === "PASSED"
                      ? colors.success
                      : step.status === "FAILED"
                        ? colors.danger
                        : colors.warning
                  }
                >
                  {`${step.status === "PASSED" ? "✓" : step.status === "FAILED" ? "×" : "·"} ${step.profile}`}
                </text>
              ))}
            </box>
          ) : null}
          <PipelineRow
            marker={
              review
                ? review.status === "COMPLETED"
                  ? "done"
                  : review.status === "FAILED" || review.status === "CANCELLED"
                    ? "failed"
                    : "active"
                : "todo"
            }
            title={
              review
                ? `Review v${review.execution_version} ${review.status}`
                : "Review has not started"
            }
            detail={
              review
                ? `${review.blocking_issues}/${review.total_issues} blocking${review.verdict ? ` · verdict ${review.verdict}` : ""}`
                : "An AI reviewer inspects the changes"
            }
          />
          {review?.summary ? (
            <text fg={colors.faint} wrapMode="word">
              {truncate(review.summary, 120)}
            </text>
          ) : null}
          {reviewIssues.length > 0 ? (
            <text fg={severityColor(reviewIssues[0]?.severity ?? "LOW")}>
              {`${reviewIssues.length} review issue${reviewIssues.length === 1 ? "" : "s"}`}
            </text>
          ) : null}
          <PipelineRow
            marker={decision ? (decision.status === "APPLIED" ? "done" : "active") : "active"}
            title={
              decision
                ? `Human decision ${decision.decision}`
                : canDecide(entry)
                  ? "Awaiting human decision — press A to approve"
                  : "Awaiting human decision"
            }
            detail={
              decision
                ? `${decision.status} · execution v${decision.execution_version}${decision.review_override ? " · reviewer override recorded" : ""}`
                : "Nothing is published without your explicit approval"
            }
          />
          {job.failure_message ? (
            <text fg={colors.danger}>{truncate(job.failure_message, 100)}</text>
          ) : null}
          <text fg={colors.faint}>
            {workspace
              ? `Workspace ${workspace.status} · ${workspace.head_sha?.slice(0, 8) ?? "no HEAD"}`
              : "Workspace not requested"}
          </text>
        </Card>
      </Section>

      <box flexDirection="row" flexGrow={1} gap={1}>
        <DiffViewer
          executionID={execution?.id}
          checkpoint={checkpoint}
          artifactKey={entry.checkSteps.find((step) => step.artifact_key)?.artifact_key}
        />
      </box>
    </box>
  )
}

function PipelineRow({
  marker,
  title,
  detail,
}: {
  marker: "done" | "active" | "failed" | "todo"
  title: string
  detail?: string
}) {
  const symbol =
    marker === "done" ? "✓" : marker === "failed" ? "×" : marker === "active" ? "◐" : "○"
  const symbolColor =
    marker === "done"
      ? colors.success
      : marker === "failed"
        ? colors.danger
        : marker === "active"
          ? colors.warning
          : colors.faint
  return (
    <box flexDirection="row" gap={1}>
      <text fg={symbolColor}>{symbol}</text>
      <box flexDirection="column" flexGrow={1}>
        <text fg={marker === "todo" ? colors.faint : colors.text}>{title}</text>
        {detail ? <text fg={colors.faint}>{detail}</text> : null}
      </box>
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
      <PageHeader
        title={`Human decision: ${kind}`}
        description={`${entry.projectName} · workflow v${job.version} · execution v${execution?.execution_version ?? 0}`}
        meta="review before applying"
      />
      <box flexDirection="row" flexGrow={1} gap={1}>
        <box flexDirection="column" width="43%" gap={1}>
          <Section title="Verify immutable approval fingerprint">
            <Card tone="warning">
              <text fg={colors.muted} wrapMode="word">
                These values pin your decision to one exact set of changes.
              </text>
              <box flexDirection="row" gap={1}>
                <text fg={colors.faint}>Execution</text>
                <text fg={colors.accent}>{`v${execution?.execution_version ?? 0}`}</text>
              </box>
              <box flexDirection="column">
                <text fg={colors.faint}>Base commit</text>
                <text fg={colors.muted}>{compactFingerprint(execution?.base_commit_sha)}</text>
              </box>
              <box flexDirection="column">
                <text fg={colors.faint}>Patch checksum</text>
                <text fg={colors.muted}>{compactFingerprint(checkpoint?.patch_checksum)}</text>
              </box>
              <Chip
                label={`Reviewer ${review?.verdict ?? "missing"} · ${review?.blocking_issues ?? 0} blocking`}
                tone={review?.verdict === "APPROVE" ? "success" : "warning"}
              />
            </Card>
          </Section>
        </box>
        <box flexDirection="column" flexGrow={1} gap={1}>
          <Section
            title={
              kind === "REQUEST_REVISION"
                ? "Revision instructions"
                : kind === "REJECT"
                  ? "Rejection reason"
                  : "Approval note"
            }
          >
            <Card tone="accent" flexGrow={1}>
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
                backgroundColor={colors.inset}
                focusedBackgroundColor={colors.raised}
                onContentChange={() => onNoteChange(noteRef.current?.plainText ?? "")}
              />
              {kind === "APPROVE" && review?.verdict === "REQUEST_REVISION" ? (
                <text fg={reviewOverride ? colors.success : colors.danger}>
                  {`Reviewer override: ${reviewOverride ? "ACKNOWLEDGED" : "NOT ACKNOWLEDGED"} · press O to toggle`}
                </text>
              ) : null}
            </Card>
          </Section>
        </box>
      </box>
      {message ? (
        <Banner tone={submitting ? "warning" : "danger"}>
          <text fg={submitting ? colors.warning : colors.danger}>{message}</text>
        </Banner>
      ) : null}
      <KeyHints
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

function compactFingerprint(value?: string): string {
  if (!value) return "missing"
  if (value.length <= 32) return value
  return `${value.slice(0, 20)}…${value.slice(-8)}`
}

function DiffViewer({
  executionID,
  checkpoint,
  artifactKey,
}: {
  executionID?: string
  checkpoint?: PatchCheckpoint
  artifactKey?: string
}) {
  const [state, setState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [lines, setLines] = useState<string[]>([])
  const [message, setMessage] = useState("")
  const [logState, setLogState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [logLines, setLogLines] = useState<string[]>([])

  useEffect(() => {
    let disposed = false
    if (!executionID) {
      setState("idle")
      setLines([])
      setLogState("idle")
      setLogLines([])
      return
    }
    setState("loading")
    setMessage("")
    void getExecutionDiff(executionID, { limit: 800 })
      .then((page) => {
        if (!disposed) {
          setLines(page.lines)
          setState("ready")
        }
      })
      .catch((error) => {
        if (!disposed) {
          setState("error")
          setMessage(error instanceof Error ? error.message : "Failed to load diff")
        }
      })
    if (artifactKey) {
      setLogState("loading")
      void getArtifactText(artifactKey)
        .then((content) => {
          if (!disposed) {
            setLogLines(content.split(/\r?\n/))
            setLogState("ready")
          }
        })
        .catch((error) => {
          if (!disposed) {
            setLogState("error")
            setMessage(error instanceof Error ? error.message : "Failed to load check log")
          }
        })
    } else {
      setLogState("idle")
      setLogLines([])
    }
    return () => {
      disposed = true
    }
  }, [artifactKey, executionID])

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <Section title="Changed files & diff">
        <Card>
          {checkpoint?.changed_files?.length ? (
            <box flexDirection="column">
              {checkpoint.changed_files.slice(0, 8).map((path) => (
                <text key={path} fg={colors.accent}>{`▸ ${path}`}</text>
              ))}
              {checkpoint.changed_files.length > 8 ? (
                <text fg={colors.faint}>{`${checkpoint.changed_files.length - 8} more files`}</text>
              ) : null}
            </box>
          ) : (
            <text fg={colors.faint}>No changed-file checkpoint yet.</text>
          )}
          {state === "loading" ? <text fg={colors.warning}>Loading diff...</text> : null}
          {state === "error" ? <text fg={colors.danger}>{message}</text> : null}
          {state === "ready" ? (
            <scrollbox height={12} backgroundColor={colors.inset} padding={1}>
              {lines.length > 0 ? (
                keyedLines(lines, "diff").map(({ key, line }) => (
                  <text
                    key={key}
                    fg={
                      line.startsWith("+")
                        ? colors.success
                        : line.startsWith("-")
                          ? colors.danger
                          : colors.muted
                    }
                  >
                    {truncate(line, 96)}
                  </text>
                ))
              ) : (
                <text fg={colors.faint}>Checkpoint has no patch content.</text>
              )}
            </scrollbox>
          ) : null}
          {artifactKey ? (
            <box flexDirection="column" gap={1}>
              <text fg={colors.faint}>{`Check log · ${artifactKey}`}</text>
              {logState === "loading" ? (
                <text fg={colors.warning}>Loading check log...</text>
              ) : null}
              {logState === "error" ? <text fg={colors.danger}>{message}</text> : null}
              {logState === "ready" ? (
                <scrollbox height={6} backgroundColor={colors.inset} padding={1}>
                  {logLines.length > 0 ? (
                    keyedLines(logLines, "log").map(({ key, line }) => (
                      <text key={key} fg={colors.muted}>
                        {truncate(line, 96)}
                      </text>
                    ))
                  ) : (
                    <text fg={colors.faint}>Check log artifact is empty.</text>
                  )}
                </scrollbox>
              ) : null}
            </box>
          ) : null}
        </Card>
      </Section>
    </box>
  )
}

function keyedLines(lines: string[], prefix: string): Array<{ key: string; line: string }> {
  const occurrences = new Map<string, number>()
  return lines.map((line) => {
    const count = (occurrences.get(line) ?? 0) + 1
    occurrences.set(line, count)
    return { key: `${prefix}-${line}-${count}`, line }
  })
}

function workflowLabel(status: WorkflowStatus): string {
  switch (status) {
    case "READY":
      return "Ready to start"
    case "WORKSPACE_PREPARING":
      return "Preparing workspace"
    case "QUEUED":
      return "Queued"
    case "EXECUTING":
      return "Agents working…"
    case "CHECKING":
      return "Running checks"
    case "REVIEWING":
      return "Reviewing changes"
    case "WAITING_FOR_APPROVAL":
      return "Waiting for your approval"
    case "REVISION_REQUIRED":
      return "Needs revision"
    case "APPROVED":
      return "Approved"
    case "PUBLISHING":
      return "Publishing"
    case "COMPLETED":
      return "Completed"
    case "FAILED":
      return "Failed"
    case "REJECTED":
      return "Rejected"
    case "CANCELLED":
      return "Cancelled"
    default:
      return status
  }
}

function workflowShortLabel(status: WorkflowStatus): string {
  switch (status) {
    case "READY":
      return "ready"
    case "WORKSPACE_PREPARING":
      return "preparing…"
    case "QUEUED":
      return "queued"
    case "EXECUTING":
      return "working…"
    case "CHECKING":
      return "checking…"
    case "REVIEWING":
      return "reviewing…"
    case "WAITING_FOR_APPROVAL":
      return "needs approval"
    case "REVISION_REQUIRED":
      return "needs revision"
    case "APPROVED":
      return "approved"
    case "PUBLISHING":
      return "publishing…"
    case "COMPLETED":
      return "completed"
    case "FAILED":
      return "failed"
    case "REJECTED":
      return "rejected"
    case "CANCELLED":
      return "cancelled"
    default:
      return status
  }
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

function workflowTone(status: WorkflowStatus): StatusTone {
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
