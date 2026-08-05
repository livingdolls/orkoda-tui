/** @jsxImportSource @opentui/react */

import type { TextareaRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { type RefObject, useCallback, useEffect, useRef, useState } from "react"

import { type ApprovalKind, submitApprovalDecision } from "./approvals"
import type { BoardItem } from "./board-model"
import { isExecutorPaused, workflowStatusLabel, workflowTone } from "./board-model"
import { type CheckRun, type CheckStep, listCheckSteps, listChecks } from "./checks"
import {
  type Execution,
  type ExecutorIteration,
  getExecutionDiff,
  listCheckpoints,
  listExecutions,
  listExecutorIterations,
  type PatchCheckpoint,
} from "./executions"
import { compareReviewIssues } from "./review-board-model"
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
  truncate,
} from "./ui"
import { collectWorkflowFailureEvidence, workflowFailureSummary } from "./workflow-failure"
import {
  getWorkflowWorkspace,
  releaseWorkspace,
  takeOverWorkspace,
  type WorkflowJob,
  type Workspace,
} from "./workflow-jobs"

type DetailSnapshot = {
  execution?: Execution
  checkpoint?: PatchCheckpoint
  check?: CheckRun
  checkSteps: CheckStep[]
  review?: ReviewRun
  reviewIssues: ReviewIssue[]
  previousReview?: ReviewRun
  previousReviewIssues: ReviewIssue[]
  workspace?: Workspace
  diffLines: string[]
  iterations: ExecutorIteration[]
}

type ManualLease = {
  token: string
}

export function BoardDetail({
  item,
  onClose,
  onWorkflowUpdated,
}: {
  item: BoardItem
  onClose: () => void
  onWorkflowUpdated: (workflow: WorkflowJob) => void
}) {
  const [workflow, setWorkflow] = useState(item.workflow)
  const [snapshot, setSnapshot] = useState<DetailSnapshot>({
    checkSteps: [],
    reviewIssues: [],
    previousReviewIssues: [],
    diffLines: [],
    iterations: [],
  })
  const [state, setState] = useState<"loading" | "ready" | "error">("loading")
  const [message, setMessage] = useState("")
  const [composer, setComposer] = useState<ApprovalKind | null>(null)
  const [note, setNote] = useState("")
  const [reviewOverride, setReviewOverride] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [manualLease, setManualLease] = useState<ManualLease | null>(null)
  const [leaseBusy, setLeaseBusy] = useState(false)
  const noteRef = useRef<TextareaRenderable>(null)

  const load = useCallback(async () => {
    if (!workflow) return
    setState("loading")
    try {
      const [executions, checks, reviews] = await Promise.all([
        listExecutions(workflow.id),
        listChecks(workflow.id),
        listReviews(workflow.id),
      ])
      const execution = executions[0]
      const check = checks[0]
      const review = reviews[0]
      const previousReview = reviews[1]
      const [checkSteps, reviewIssues, previousReviewIssues, checkpoints, workspace, iterations] =
        await Promise.all([
          check ? listCheckSteps(check.id) : Promise.resolve([]),
          review ? listReviewIssues(review.id) : Promise.resolve([]),
          previousReview ? listReviewIssues(previousReview.id) : Promise.resolve([]),
          execution ? listCheckpoints(execution.id) : Promise.resolve([]),
          getWorkflowWorkspace(workflow.id).catch(() => undefined),
          execution ? listExecutorIterations(execution.id) : Promise.resolve([]),
        ])
      const checkpoint = checkpoints.at(-1)
      const diff = execution
        ? await getExecutionDiff(execution.id, { limit: 800 }).catch(() => undefined)
        : undefined
      setSnapshot({
        execution,
        checkpoint,
        check,
        checkSteps,
        review,
        reviewIssues,
        previousReview,
        previousReviewIssues,
        workspace,
        diffLines: diff?.lines ?? [],
        iterations,
      })
      setState("ready")
    } catch (error) {
      setState("error")
      setMessage(error instanceof Error ? error.message : "Failed to load workflow details")
    }
  }, [workflow])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (composer) noteRef.current?.focus()
  }, [composer])

  const failureEvidence = workflow
    ? collectWorkflowFailureEvidence({
        workflow,
        workspace: snapshot.workspace,
        execution: snapshot.execution,
        checkSteps: snapshot.checkSteps,
        review: snapshot.review,
      })
    : []

  const reviewComparison = compareReviewIssues(snapshot.previousReviewIssues, snapshot.reviewIssues)

  const canDecide =
    workflow?.status === "WAITING_FOR_APPROVAL" &&
    snapshot.execution?.status === "COMPLETED" &&
    snapshot.review?.status === "COMPLETED" &&
    snapshot.checkpoint !== undefined

  const openComposer = (kind: ApprovalKind) => {
    if (!canDecide) {
      setMessage("This workflow is not ready for a human decision yet.")
      return
    }
    setComposer(kind)
    setNote("")
    setReviewOverride(false)
    setMessage("")
  }

  const submitDecision = async () => {
    if (!workflow || !composer || submitting) return
    const { execution, checkpoint, review } = snapshot
    if (!execution || !checkpoint || !review) {
      setMessage("The immutable approval snapshot is incomplete. Reload the workflow.")
      return
    }
    const nextNote = noteRef.current?.plainText.trim() ?? note.trim()
    if ((composer === "REQUEST_REVISION" || composer === "REJECT") && !nextNote) {
      setMessage("A reason is required for revision or rejection.")
      return
    }
    if (composer === "APPROVE" && review.verdict === "REQUEST_REVISION") {
      if (!reviewOverride || !nextNote) {
        setMessage("Acknowledge the reviewer override and explain why the risk is accepted.")
        return
      }
    }

    setSubmitting(true)
    setMessage(`Applying ${composer.toLowerCase().replaceAll("_", " ")}...`)
    try {
      const outcome = await submitApprovalDecision(workflow.id, composer, {
        expected_version: workflow.version,
        execution_version: execution.execution_version,
        base_commit_sha: execution.base_commit_sha,
        patch_checksum: checkpoint.patch_checksum,
        note: nextNote,
        review_override: reviewOverride,
      })
      setWorkflow(outcome.workflow)
      onWorkflowUpdated(outcome.workflow)
      setComposer(null)
      setNote("")
      setReviewOverride(false)
      setMessage(
        `Decision applied. Workflow is now ${workflowStatusLabel(outcome.workflow.status)}.`,
      )
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to apply the decision")
    } finally {
      setSubmitting(false)
    }
  }

  const toggleWorkspaceLease = async () => {
    if (!workflow || leaseBusy) return
    const workspace = snapshot.workspace
    if (!workspace) {
      setMessage("The isolated workspace is not available yet.")
      return
    }
    setLeaseBusy(true)
    try {
      if (manualLease) {
        const released = await releaseWorkspace(
          workspace.id,
          manualLease.token,
          workspace.head_sha ?? workflow.base_commit_sha,
          workspace.dirty,
        )
        setSnapshot((current) => ({ ...current, workspace: released }))
        setManualLease(null)
        setMessage("Manual workspace access released. The daemon can resume work.")
      } else {
        const lease = await takeOverWorkspace(workflow.id, `tui-board-${process.pid}`)
        setSnapshot((current) => ({ ...current, workspace: lease.workspace }))
        setManualLease({ token: lease.session_token })
        setMessage(`Manual workspace access active at ${lease.workspace.path}`)
      }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Workspace operation failed")
    } finally {
      setLeaseBusy(false)
    }
  }

  useKeyboard((key) => {
    if (composer) {
      if (key.name === "escape") {
        if (!submitting) setComposer(null)
        return
      }
      if (key.name === "o" && composer === "APPROVE") {
        setReviewOverride((current) => !current)
        return
      }
      if (key.ctrl && key.name === "s") void submitDecision()
      return
    }
    if (key.name === "escape") {
      onClose()
      return
    }
    if (key.name === "r") {
      void load()
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
    if (key.name === "e") void toggleWorkspaceLease()
  })

  if (!workflow) {
    return (
      <EmptyState
        title="Workflow has not started"
        detail="Return to the board and start this prepared plan first."
        shortcut={{ key: "Esc", label: "back to board" }}
      />
    )
  }

  if (composer) {
    return (
      <DecisionComposer
        item={item}
        workflow={workflow}
        kind={composer}
        snapshot={snapshot}
        noteRef={noteRef}
        note={note}
        reviewOverride={reviewOverride}
        message={message}
        submitting={submitting}
        onNoteChange={setNote}
      />
    )
  }

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title={item.plan.title}
        description={`${item.project.name} · ${workflowStatusLabel(workflow.status)}`}
        meta={`${workflow.base_branch}@${workflow.base_commit_sha.slice(0, 8)}`}
      />
      <box flexDirection="row" gap={1} alignItems="center" flexWrap="wrap">
        <Chip label={workflowStatusLabel(workflow.status)} tone={workflowTone(workflow.status)} />
        <Chip label={`workflow v${workflow.version}`} dot={false} />
        <Chip label={`execution v${workflow.execution_version}`} dot={false} />
        {snapshot.review ? (
          <Chip
            label={`${snapshot.review.blocking_issues} blocking issue(s)`}
            tone={snapshot.review.blocking_issues > 0 ? "warning" : "success"}
          />
        ) : null}
      </box>

      {workflow.status === "FAILED" ? (
        <Banner tone={isExecutorPaused(workflow) ? "warning" : "danger"}>
          <text fg={isExecutorPaused(workflow) ? colors.warning : colors.danger} attributes={BOLD}>
            {isExecutorPaused(workflow) ? "Executor paused" : "Why the workflow stopped"}
          </text>
          <text fg={colors.text} wrapMode="word">
            {workflowFailureSummary(workflow)}
          </text>
          <text fg={colors.muted}>
            {isExecutorPaused(workflow)
              ? "Inspect the partial diff and iteration timeline, press Esc, then choose Continue +8 or +16 turns."
              : "Fix the cause, press Esc, then choose Retry workflow."}
          </text>
        </Banner>
      ) : null}

      {state === "loading" ? (
        <Card>
          <text fg={colors.warning}>Loading workflow evidence...</text>
        </Card>
      ) : null}
      {state === "error" ? (
        <Banner tone="danger">
          <text fg={colors.danger}>{message}</text>
        </Banner>
      ) : null}

      {state === "ready" ? (
        <scrollbox flexGrow={1} scrollY={true}>
          <box flexDirection="column" gap={1}>
            {failureEvidence.length > 0 ? (
              <Section title="Failure evidence" action="fix the cause, then retry from Board">
                <Card tone="danger">
                  {failureEvidence.map((failure) => (
                    <box
                      key={`${failure.source}:${failure.code ?? failure.message}`}
                      flexDirection="column"
                      gap={0}
                    >
                      <text fg={colors.danger} attributes={BOLD}>
                        {failure.code ? `${failure.source} · ${failure.code}` : failure.source}
                      </text>
                      <text fg={colors.text} wrapMode="word">
                        {failure.message}
                      </text>
                    </box>
                  ))}
                </Card>
              </Section>
            ) : null}

            <Section title="Progress" action="the card moves automatically as stages finish">
              <Card>
                <ProgressRow
                  state={stageState(
                    snapshot.execution?.status,
                    ["COMPLETED"],
                    ["FAILED", "CANCELLED"],
                  )}
                  title="Implementation"
                  detail={
                    snapshot.execution
                      ? `${snapshot.execution.tool_calls}/${workflow.limits.max_tool_calls} tool calls · ${snapshot.execution.usage.total_tokens} tokens`
                      : "Waiting for the Executor Agent"
                  }
                />
                <ProgressRow
                  state={stageState(snapshot.check?.status, ["PASSED"], ["FAILED", "CANCELLED"])}
                  title="Automated checks"
                  detail={
                    snapshot.check
                      ? `${snapshot.check.passed_steps} passed · ${snapshot.check.failed_steps} failed`
                      : "Runs after implementation"
                  }
                />
                <ProgressRow
                  state={stageState(
                    snapshot.review?.status,
                    ["COMPLETED"],
                    ["FAILED", "CANCELLED"],
                  )}
                  title="AI review"
                  detail={
                    snapshot.review
                      ? `${snapshot.review.total_issues} issue(s) · verdict ${snapshot.review.verdict ?? "pending"}`
                      : "Reviews the immutable diff and check evidence"
                  }
                />
                <ProgressRow
                  state={
                    workflow.status === "WAITING_FOR_APPROVAL"
                      ? "active"
                      : ["APPROVED", "PUBLISHING", "COMPLETED"].includes(workflow.status)
                        ? "done"
                        : ["REJECTED", "CANCELLED"].includes(workflow.status)
                          ? "failed"
                          : "todo"
                  }
                  title="Human decision and publication"
                  detail="Nothing is published without your explicit approval"
                />
              </Card>
            </Section>

            {snapshot.iterations.length > 0 ? (
              <Section title="Executor iteration timeline" action="latest 12 durable turns">
                <Card>
                  {snapshot.iterations.slice(-12).map((iteration) => (
                    <box key={iteration.id} flexDirection="column" gap={0}>
                      <box flexDirection="row" justifyContent="space-between" gap={1}>
                        <text fg={iteration.status === "FAILED" ? colors.danger : colors.text}>
                          {`${iteration.sequence}. ${String(iteration.action_summary.type ?? (iteration.action_type === "finish" ? "finish" : (iteration.tool ?? "tool")))}`}
                        </text>
                        <Chip
                          label={iteration.status.toLowerCase()}
                          tone={iteration.status === "FAILED" ? "danger" : "neutral"}
                        />
                      </box>
                      <text fg={colors.faint} wrapMode="word">
                        {iteration.error_message
                          ? `${iteration.error_code ?? "TOOL_FAILED"}: ${truncate(iteration.error_message, 180)}`
                          : truncate(String(iteration.action_summary.summary ?? "completed"), 180)}
                      </text>
                    </box>
                  ))}
                </Card>
              </Section>
            ) : null}

            {snapshot.execution || snapshot.review ? (
              <Section title="Agent handoff">
                <Card>
                  <text
                    fg={colors.accent}
                  >{`Executor · ${snapshot.execution?.provider || "pending"}/${snapshot.execution?.model || "pending"}`}</text>
                  <text fg={colors.faint}>↓ immutable patch and automated check evidence</text>
                  <text
                    fg={colors.accent}
                  >{`Reviewer · ${snapshot.review?.provider || "pending"}/${snapshot.review?.model || "pending"}`}</text>
                  <text fg={colors.faint}>↓ human approval remains required</text>
                  <text fg={colors.text}>Developer decision</text>
                </Card>
              </Section>
            ) : null}

            {snapshot.review?.summary ? (
              <Section title="Review summary">
                <Card tone={snapshot.review.blocking_issues > 0 ? "warning" : "success"}>
                  <text fg={colors.text} wrapMode="word">
                    {snapshot.review.summary}
                  </text>
                </Card>
              </Section>
            ) : null}

            {snapshot.previousReview && reviewComparison.length > 0 ? (
              <Section
                title="Previous review comparison"
                action={`execution v${snapshot.previousReview.execution_version} → v${snapshot.review?.execution_version ?? workflow.execution_version}`}
              >
                <Card>
                  {reviewComparison.map((item) => (
                    <box key={item.key} flexDirection="row" justifyContent="space-between" gap={1}>
                      <text
                        fg={
                          item.status === "RESOLVED"
                            ? colors.success
                            : item.status === "NEW"
                              ? colors.danger
                              : item.status === "PARTIALLY_RESOLVED"
                                ? colors.warning
                                : colors.muted
                        }
                      >
                        {item.status.toLowerCase().replaceAll("_", " ")}
                      </text>
                      <text fg={colors.text} wrapMode="word">
                        {item.current?.title ?? item.previous?.title ?? item.key}
                      </text>
                    </box>
                  ))}
                </Card>
              </Section>
            ) : null}

            {snapshot.checkSteps.length > 0 ? (
              <Section title="Checks">
                <Card>
                  {snapshot.checkSteps.map((step) => (
                    <box key={step.id} flexDirection="row" justifyContent="space-between" gap={1}>
                      <text fg={colors.text}>{step.profile}</text>
                      <Chip
                        label={step.status.toLowerCase()}
                        tone={
                          step.status === "PASSED"
                            ? "success"
                            : step.status === "FAILED"
                              ? "danger"
                              : "warning"
                        }
                      />
                    </box>
                  ))}
                </Card>
              </Section>
            ) : null}

            {snapshot.reviewIssues.length > 0 ? (
              <Section title="Review findings">
                <Card>
                  {snapshot.reviewIssues.map((issue) => (
                    <box key={issue.id} flexDirection="column">
                      <text
                        fg={severityColor(issue.severity)}
                        attributes={issue.blocking ? BOLD : 0}
                      >
                        {`${issue.severity}${issue.blocking ? " · BLOCKING" : ""} · ${issue.title}`}
                      </text>
                      <text fg={colors.muted} wrapMode="word">
                        {issue.description}
                      </text>
                      {issue.file_path ? (
                        <text fg={colors.faint}>
                          {`${issue.file_path}${issue.line_start ? `:${issue.line_start}` : ""}`}
                        </text>
                      ) : null}
                    </box>
                  ))}
                </Card>
              </Section>
            ) : null}

            <Section title="Changed files and diff">
              <Card>
                {snapshot.checkpoint?.changed_files.length ? (
                  <box flexDirection="column">
                    {snapshot.checkpoint.changed_files.slice(0, 12).map((path) => (
                      <text key={path} fg={colors.accent}>{`▸ ${path}`}</text>
                    ))}
                    {snapshot.checkpoint.changed_files.length > 12 ? (
                      <text fg={colors.faint}>
                        {`${snapshot.checkpoint.changed_files.length - 12} more file(s)`}
                      </text>
                    ) : null}
                  </box>
                ) : (
                  <text fg={colors.faint}>No changed-file checkpoint is available yet.</text>
                )}
                {snapshot.diffLines.length > 0 ? (
                  <scrollbox height={14} backgroundColor={colors.inset} padding={1}>
                    {keyedLines(snapshot.diffLines).map(({ key, line }) => (
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
                        {truncate(line, 110)}
                      </text>
                    ))}
                  </scrollbox>
                ) : null}
              </Card>
            </Section>

            <Section title="Technical details" action="kept secondary for non-technical users">
              <Card>
                <text fg={colors.faint}>{`Workflow ${workflow.id}`}</text>
                <text fg={colors.faint}>{`Base commit ${workflow.base_commit_sha}`}</text>
                <text fg={colors.faint}>
                  {snapshot.checkpoint
                    ? `Patch checksum ${snapshot.checkpoint.patch_checksum}`
                    : "Patch checksum pending"}
                </text>
                <text fg={colors.faint}>
                  {snapshot.workspace
                    ? `Workspace ${snapshot.workspace.status} · ${snapshot.workspace.path}`
                    : "Workspace pending"}
                </text>
              </Card>
            </Section>
          </box>
        </scrollbox>
      ) : null}

      {message && state !== "error" ? (
        <Banner tone={message.toLowerCase().includes("failed") ? "danger" : "accent"}>
          <text fg={colors.muted}>{message}</text>
        </Banner>
      ) : null}
      <KeyHints
        shortcuts={[
          { key: "A", label: "approve" },
          { key: "V", label: "request revision" },
          { key: "X", label: "reject" },
          { key: "E", label: manualLease ? "release workspace" : "manual workspace" },
          { key: "R", label: "reload" },
          { key: "Esc", label: "back to board" },
        ]}
      />
    </box>
  )
}

function DecisionComposer({
  item,
  workflow,
  kind,
  snapshot,
  noteRef,
  note,
  reviewOverride,
  message,
  submitting,
  onNoteChange,
}: {
  item: BoardItem
  workflow: WorkflowJob
  kind: ApprovalKind
  snapshot: DetailSnapshot
  noteRef: RefObject<TextareaRenderable | null>
  note: string
  reviewOverride: boolean
  message: string
  submitting: boolean
  onNoteChange: (value: string) => void
}) {
  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title={
          kind === "APPROVE"
            ? "Approve this result"
            : kind === "REQUEST_REVISION"
              ? "Request a revision"
              : "Reject this result"
        }
        description={`${item.plan.title} · execution v${snapshot.execution?.execution_version ?? 0}`}
        meta="decision is bound to this exact diff"
      />
      <box flexDirection="row" flexGrow={1} gap={1}>
        <box width="42%" flexDirection="column" gap={1}>
          <Section title="Verify the snapshot">
            <Card tone="warning">
              <text fg={colors.muted} wrapMode="word">
                Your decision is pinned to one execution, base commit, and patch checksum.
              </text>
              <text fg={colors.faint}>{`Workflow v${workflow.version}`}</text>
              <text fg={colors.faint}>
                {`Execution v${snapshot.execution?.execution_version ?? 0}`}
              </text>
              <text fg={colors.faint}>
                {`Base ${compactFingerprint(snapshot.execution?.base_commit_sha)}`}
              </text>
              <text fg={colors.faint}>
                {`Patch ${compactFingerprint(snapshot.checkpoint?.patch_checksum)}`}
              </text>
              <Chip
                label={`Reviewer ${snapshot.review?.verdict ?? "missing"} · ${snapshot.review?.blocking_issues ?? 0} blocking`}
                tone={snapshot.review?.verdict === "APPROVE" ? "success" : "warning"}
              />
            </Card>
          </Section>
        </box>
        <box flexGrow={1} flexDirection="column" gap={1}>
          <Section
            title={
              kind === "APPROVE"
                ? "Approval note"
                : kind === "REQUEST_REVISION"
                  ? "Revision instructions"
                  : "Rejection reason"
            }
          >
            <Card tone="accent" flexGrow={1}>
              <textarea
                ref={noteRef}
                width="100%"
                height={10}
                initialValue={note}
                placeholder={
                  kind === "APPROVE"
                    ? "Optional unless overriding a reviewer revision verdict"
                    : "Explain exactly what needs to change"
                }
                focused
                wrapMode="word"
                backgroundColor={colors.inset}
                focusedBackgroundColor={colors.raised}
                onContentChange={() => onNoteChange(noteRef.current?.plainText ?? "")}
              />
              {kind === "APPROVE" && snapshot.review?.verdict === "REQUEST_REVISION" ? (
                <text fg={reviewOverride ? colors.success : colors.danger}>
                  {`Reviewer override ${reviewOverride ? "acknowledged" : "not acknowledged"} · press O`}
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
          { key: "O", label: "reviewer override" },
          { key: "Esc", label: "cancel" },
        ]}
      />
    </box>
  )
}

function ProgressRow({
  state,
  title,
  detail,
}: {
  state: "done" | "active" | "failed" | "todo"
  title: string
  detail: string
}) {
  const symbol = state === "done" ? "✓" : state === "failed" ? "×" : state === "active" ? "◐" : "○"
  const color =
    state === "done"
      ? colors.success
      : state === "failed"
        ? colors.danger
        : state === "active"
          ? colors.warning
          : colors.faint
  return (
    <box flexDirection="row" gap={1}>
      <text fg={color}>{symbol}</text>
      <box flexDirection="column" flexGrow={1}>
        <text fg={state === "todo" ? colors.faint : colors.text}>{title}</text>
        <text fg={colors.faint}>{detail}</text>
      </box>
    </box>
  )
}

function stageState(
  status: string | undefined,
  completed: string[],
  failed: string[],
): "done" | "active" | "failed" | "todo" {
  if (!status) return "todo"
  if (completed.includes(status)) return "done"
  if (failed.includes(status)) return "failed"
  return "active"
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

function compactFingerprint(value?: string): string {
  if (!value) return "missing"
  if (value.length <= 28) return value
  return `${value.slice(0, 18)}…${value.slice(-8)}`
}

function keyedLines(lines: string[]): Array<{ key: string; line: string }> {
  const occurrences = new Map<string, number>()
  return lines.map((line) => {
    const count = (occurrences.get(line) ?? 0) + 1
    occurrences.set(line, count)
    return { key: `${line}-${count}`, line }
  })
}
