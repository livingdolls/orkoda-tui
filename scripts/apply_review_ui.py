from pathlib import Path

root = Path('.')


def replace_once(path: Path, old: str, new: str) -> None:
    text = path.read_text()
    if old not in text:
        raise SystemExit(f'marker not found in {path}: {old[:120]!r}')
    path.write_text(text.replace(old, new, 1))


(root / 'apps/tui/src/review-board-model.ts').write_text(r'''import type { ReviewIssue, ReviewRun } from "./reviews"
import type { WorkflowJob } from "./workflow-jobs"

export const reviewColumns = [
  "AWAITING_REVIEW",
  "AI_REVIEWING",
  "ISSUES_FOUND",
  "REVISION_IN_PROGRESS",
  "RE_REVIEW",
  "READY_FOR_APPROVAL",
  "APPROVED",
] as const

export type ReviewColumn = (typeof reviewColumns)[number]

export const reviewColumnLabels: Record<ReviewColumn, string> = {
  AWAITING_REVIEW: "Awaiting Review",
  AI_REVIEWING: "AI Reviewing",
  ISSUES_FOUND: "Issues Found",
  REVISION_IN_PROGRESS: "Revision",
  RE_REVIEW: "Re-review",
  READY_FOR_APPROVAL: "Approval",
  APPROVED: "Approved",
}

export const reviewColumnDescriptions: Record<ReviewColumn, string> = {
  AWAITING_REVIEW: "Implementation or checks are finishing before review starts.",
  AI_REVIEWING: "The configured Reviewer Agent is inspecting immutable evidence.",
  ISSUES_FOUND: "Blocking findings or a review-stage failure needs attention.",
  REVISION_IN_PROGRESS: "The Executor is applying requested changes.",
  RE_REVIEW: "A newer execution version is being checked against prior findings.",
  READY_FOR_APPROVAL: "Review evidence is ready for a human decision.",
  APPROVED: "The reviewed execution was approved or published.",
}

export function isReviewRelevant(workflow: WorkflowJob): boolean {
  if (["REJECTED", "CANCELLED"].includes(workflow.status)) return false
  if (["READY", "WORKSPACE_PREPARING"].includes(workflow.status)) return false
  return workflow.execution_version > 0 || ["CHECKING", "REVIEWING", "WAITING_FOR_APPROVAL", "APPROVED", "PUBLISHING", "COMPLETED", "FAILED"].includes(workflow.status)
}

export function resolveReviewColumn(workflow: WorkflowJob, review?: ReviewRun): ReviewColumn {
  if (["APPROVED", "PUBLISHING", "COMPLETED"].includes(workflow.status)) return "APPROVED"
  if (workflow.status === "WAITING_FOR_APPROVAL") {
    return review?.verdict === "REQUEST_REVISION" || (review?.blocking_issues ?? 0) > 0
      ? "ISSUES_FOUND"
      : "READY_FOR_APPROVAL"
  }
  if (workflow.status === "REVISION_REQUIRED") return "REVISION_IN_PROGRESS"
  if (["QUEUED", "EXECUTING", "CHECKING"].includes(workflow.status)) {
    return workflow.revision_count > 0 || workflow.execution_version > 1
      ? "REVISION_IN_PROGRESS"
      : "AWAITING_REVIEW"
  }
  if (workflow.status === "REVIEWING") {
    if (review?.status === "RUNNING") return "AI_REVIEWING"
    if (workflow.execution_version > 1 || workflow.revision_count > 0) return "RE_REVIEW"
    return "AWAITING_REVIEW"
  }
  if (workflow.status === "FAILED") return "ISSUES_FOUND"
  return "AWAITING_REVIEW"
}

export function reviewCardStatus(workflow: WorkflowJob, review?: ReviewRun): string {
  if (workflow.status === "FAILED") {
    return workflow.failure_message || "Review pipeline failed"
  }
  if (review?.status === "FAILED") return review.failure_message || "Reviewer failed"
  if (review?.status === "RUNNING") return "Reviewer Agent is checking the latest execution"
  if (review?.verdict === "REQUEST_REVISION") {
    return `${review.blocking_issues} blocking issue(s) require revision`
  }
  if (review?.verdict === "APPROVE") return "Reviewer recommends approval"
  if (workflow.status === "REVISION_REQUIRED") return "Revision feedback is queued for Executor"
  return reviewColumnDescriptions[resolveReviewColumn(workflow, review)]
}

export type IssueResolution = "NEW" | "STILL_PRESENT" | "PARTIALLY_RESOLVED" | "RESOLVED"

export type ReviewIssueComparison = {
  key: string
  status: IssueResolution
  current?: ReviewIssue
  previous?: ReviewIssue
}

const severityRank: Record<ReviewIssue["severity"], number> = {
  LOW: 1,
  MEDIUM: 2,
  HIGH: 3,
  CRITICAL: 4,
}

export function compareReviewIssues(
  previous: ReviewIssue[],
  current: ReviewIssue[],
): ReviewIssueComparison[] {
  const currentByKey = new Map(current.map((issue) => [issue.key, issue]))
  const previousByKey = new Map(previous.map((issue) => [issue.key, issue]))
  const result: ReviewIssueComparison[] = []
  for (const issue of current) {
    const before = previousByKey.get(issue.key)
    if (!before) {
      result.push({ key: issue.key, status: "NEW", current: issue })
      continue
    }
    const improved =
      (before.blocking && !issue.blocking) ||
      severityRank[issue.severity] < severityRank[before.severity]
    result.push({
      key: issue.key,
      status: improved ? "PARTIALLY_RESOLVED" : "STILL_PRESENT",
      current: issue,
      previous: before,
    })
  }
  for (const issue of previous) {
    if (!currentByKey.has(issue.key)) {
      result.push({ key: issue.key, status: "RESOLVED", previous: issue })
    }
  }
  return result
}
''')

(root / 'apps/tui/src/review-board-data.ts').write_text(r'''import { getAgentSettings, type AgentConfig, type AgentSettings } from "./agent-settings"
import { type CheckRun, listChecks } from "./checks"
import { type Execution, listExecutions } from "./executions"
import { listPlans, type Plan } from "./plans"
import { listProjects, type Project } from "./projects"
import {
  isReviewRelevant,
  resolveReviewColumn,
  reviewCardStatus,
  type ReviewColumn,
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

function configuredAgent(settings: AgentSettings | undefined, role: AgentConfig["role"]): AgentConfig | undefined {
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

async function loadProjectCards(project: Project): Promise<ReviewBoardCard[]> {
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
        updatedAt: review?.updated_at ?? check?.updated_at ?? execution?.updated_at ?? workflow.updated_at,
      }
    }),
  )
}

export async function loadReviewBoard(): Promise<{ projects: Project[]; cards: ReviewBoardCard[] }> {
  const projects = await listProjects()
  const cards = (await Promise.all(projects.map(loadProjectCards))).flat()
  cards.sort((left, right) => new Date(right.updatedAt).getTime() - new Date(left.updatedAt).getTime())
  return { projects, cards }
}
''')

(root / 'apps/tui/src/review-board-model.test.ts').write_text(r'''import { describe, expect, test } from "bun:test"

import { compareReviewIssues, resolveReviewColumn } from "./review-board-model"
import type { ReviewIssue, ReviewRun } from "./reviews"
import type { WorkflowJob, WorkflowStatus } from "./workflow-jobs"

function workflow(status: WorkflowStatus, executionVersion = 1, revisionCount = 0): WorkflowJob {
  return {
    id: "job-1", project_id: "project-1", plan_id: "plan-1", plan_version_id: "version-1",
    repository_id: "repo-1", base_branch: "main", base_commit_sha: "abc123", status,
    version: 3, execution_version: executionVersion, revision_count: revisionCount,
    limits: { max_revisions: 3, max_stage_attempts: 3, max_tool_calls: 50, wall_clock_seconds: 3600 },
    cancellation_requested: false, created_at: "2026-08-05T00:00:00Z", updated_at: "2026-08-05T00:00:00Z",
  }
}

function review(verdict: ReviewRun["verdict"], blocking = 0): ReviewRun {
  return {
    id: "review-1", workflow_job_id: "job-1", execution_id: "execution-1", execution_version: 1,
    check_run_id: "check-1", checkpoint_id: "checkpoint-1", agent_settings_version: 2,
    provider: "openai", model: "review-model", status: "COMPLETED", verdict,
    summary: "reviewed", total_issues: blocking, blocking_issues: blocking,
    created_at: "2026-08-05T00:00:00Z", updated_at: "2026-08-05T00:00:00Z",
  }
}

function issue(key: string, severity: ReviewIssue["severity"], blocking: boolean): ReviewIssue {
  return {
    id: key, review_run_id: "review-1", position: 0, key, severity, category: "CORRECTNESS",
    blocking, title: key, description: key, criteria_refs: [], created_at: "2026-08-05T00:00:00Z",
  }
}

describe("review board projection", () => {
  test("maps review and revision lifecycle", () => {
    expect(resolveReviewColumn(workflow("REVIEWING"))).toBe("AWAITING_REVIEW")
    expect(resolveReviewColumn(workflow("REVIEWING", 2), { ...review(undefined), status: "RUNNING" })).toBe("AI_REVIEWING")
    expect(resolveReviewColumn(workflow("WAITING_FOR_APPROVAL"), review("REQUEST_REVISION", 1))).toBe("ISSUES_FOUND")
    expect(resolveReviewColumn(workflow("EXECUTING", 2, 1))).toBe("REVISION_IN_PROGRESS")
    expect(resolveReviewColumn(workflow("REVIEWING", 2, 1))).toBe("RE_REVIEW")
    expect(resolveReviewColumn(workflow("WAITING_FOR_APPROVAL"), review("APPROVE"))).toBe("READY_FOR_APPROVAL")
    expect(resolveReviewColumn(workflow("COMPLETED"), review("APPROVE"))).toBe("APPROVED")
  })

  test("compares findings across review cycles", () => {
    const comparison = compareReviewIssues(
      [issue("fixed", "HIGH", true), issue("improved", "HIGH", true), issue("same", "MEDIUM", false)],
      [issue("improved", "LOW", false), issue("same", "MEDIUM", false), issue("new", "HIGH", true)],
    )
    expect(Object.fromEntries(comparison.map((item) => [item.key, item.status]))).toEqual({
      improved: "PARTIALLY_RESOLVED", same: "STILL_PRESENT", new: "NEW", fixed: "RESOLVED",
    })
  })
})
''')

(root / 'apps/tui/src/agent-profile-editor.tsx').write_text(r'''/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useMemo, useState } from "react"

import type { AgentConfig } from "./agent-settings"
import type { LLMProviderInfo } from "./llm-providers"
import { Banner, Card, colors, KeyHints, PageHeader } from "./ui"

export function AgentProfileEditor({
  agent,
  peer,
  providers,
  onApply,
  onCancel,
}: {
  agent: AgentConfig
  peer?: AgentConfig
  providers: LLMProviderInfo[]
  onApply: (agent: AgentConfig) => void
  onCancel: () => void
}) {
  const [field, setField] = useState<"provider" | "model">("provider")
  const [provider, setProvider] = useState(agent.provider)
  const [model, setModel] = useState(agent.model)
  const [message, setMessage] = useState("")
  const selectedProvider = useMemo(
    () => providers.find((item) => item.name === provider.trim().toLowerCase()),
    [providers, provider],
  )

  const apply = () => {
    const nextProvider = provider.trim().toLowerCase()
    const nextModel = model.trim()
    if ((nextProvider === "") !== (nextModel === "")) {
      setMessage("Provider and model must both be set, or both be empty to inherit the daemon default.")
      return
    }
    if (
      peer?.enabled &&
      agent.enabled &&
      nextProvider !== "" &&
      nextProvider === peer.provider &&
      nextModel === peer.model
    ) {
      setMessage("Executor and Reviewer cannot use the same explicit provider and model.")
      return
    }
    if (nextProvider && !providers.some((item) => item.name === nextProvider)) {
      setMessage(`Provider ${nextProvider} is not registered by the daemon.`)
      return
    }
    onApply({ ...agent, provider: nextProvider, model: nextModel })
  }

  useKeyboard((key) => {
    if (key.name === "escape") return onCancel()
    if (key.name === "tab") {
      setField((current) => (current === "provider" ? "model" : "provider"))
      return
    }
    if (key.ctrl && key.name === "s") {
      apply()
      return
    }
    if (key.name === "d" && selectedProvider) {
      setModel(selectedProvider.default_model)
    }
  })

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title={`Edit ${agent.role.toLowerCase()} model`}
        description="Assign a registered provider and model to this role. Empty fields inherit the daemon default."
        meta={`${providers.length} provider(s) registered`}
      />
      <Card>
        <text fg={field === "provider" ? colors.accent : colors.muted}>Provider</text>
        <input
          value={provider}
          focused={field === "provider"}
          placeholder="deepseek"
          onInput={setProvider}
          onSubmit={() => setField("model")}
        />
        <text fg={field === "model" ? colors.accent : colors.muted}>Model</text>
        <input
          value={model}
          focused={field === "model"}
          placeholder="executor-model"
          onInput={setModel}
          onSubmit={apply}
        />
      </Card>
      <Card>
        <text fg={colors.muted}>Registered providers</text>
        {providers.map((item) => (
          <text key={item.name} fg={item.name === provider ? colors.accent : colors.faint}>
            {`${item.name} · default ${item.default_model}${item.configured ? " · configured" : ""}`}
          </text>
        ))}
      </Card>
      {peer ? (
        <text fg={colors.faint}>{`Other role: ${peer.role.toLowerCase()} · ${peer.provider || "daemon default"}/${peer.model || "daemon default"}`}</text>
      ) : null}
      {message ? (
        <Banner tone="danger"><text fg={colors.danger}>{message}</text></Banner>
      ) : null}
      <KeyHints shortcuts={[{ key: "Tab", label: "field" }, { key: "D", label: "provider default model" }, { key: "Ctrl+S", label: "apply" }, { key: "Esc", label: "cancel" }]} />
    </box>
  )
}
''')

(root / 'apps/tui/src/review-board-screen.tsx').write_text(r'''/** @jsxImportSource @opentui/react */

import { useKeyboard, useOnResize } from "@opentui/react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { BoardDetail } from "./board-detail"
import { createBoardItem } from "./board-model"
import type { DaemonConnection } from "./daemon"
import type { ActivityEvent } from "./events"
import { loadReviewBoard, type ReviewBoardCard } from "./review-board-data"
import {
  reviewColumnDescriptions,
  reviewColumnLabels,
  reviewColumns,
  type ReviewColumn,
} from "./review-board-model"
import { BOLD, Banner, Card, Chip, colors, EmptyState, KeyHints, PageHeader, truncate } from "./ui"
import { performWorkflowAction, type WorkflowJob } from "./workflow-jobs"

const initialIndexes = Object.fromEntries(reviewColumns.map((column) => [column, 0])) as Record<ReviewColumn, number>

type Mode = "board" | "detail"

export function ReviewBoardScreen({
  connection,
  lastEvent,
  onInteractionChange,
}: {
  connection: DaemonConnection
  lastEvent: ActivityEvent | null
  onInteractionChange?: (active: boolean) => void
}) {
  const [projects, setProjects] = useState<Array<{ id: string; name: string }>>([])
  const [cards, setCards] = useState<ReviewBoardCard[]>([])
  const [state, setState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [message, setMessage] = useState("")
  const [projectIndex, setProjectIndex] = useState(0)
  const [columnIndex, setColumnIndex] = useState(0)
  const [selectedIndexes, setSelectedIndexes] = useState(initialIndexes)
  const [activeOnly, setActiveOnly] = useState(true)
  const [mode, setMode] = useState<Mode>("board")
  const [detailCard, setDetailCard] = useState<ReviewBoardCard | null>(null)
  const [busy, setBusy] = useState(false)
  const [terminalWidth, setTerminalWidth] = useState(0)
  const selectedID = useRef<string | null>(null)
  const renderer = useOnResize((width) => setTerminalWidth(width))

  const filters = useMemo(() => [{ id: "", name: "All projects" }, ...projects], [projects])
  const selectedProject = filters[projectIndex] ?? filters[0]
  const visibleCards = useMemo(
    () => cards.filter((card) => (!selectedProject?.id || card.project.id === selectedProject.id) && (!activeOnly || card.column !== "APPROVED")),
    [cards, selectedProject?.id, activeOnly],
  )
  const byColumn = useMemo(
    () => Object.fromEntries(reviewColumns.map((column) => [column, visibleCards.filter((card) => card.column === column)])) as Record<ReviewColumn, ReviewBoardCard[]>,
    [visibleCards],
  )
  const activeColumn = reviewColumns[columnIndex] ?? "AWAITING_REVIEW"
  const columnCards = byColumn[activeColumn]
  const selectedIndex = Math.min(selectedIndexes[activeColumn], Math.max(columnCards.length - 1, 0))
  const selectedCard = columnCards[selectedIndex] ?? null
  const hasActive = cards.some((card) => card.column !== "APPROVED")

  const restoreSelection = useCallback((nextCards: ReviewBoardCard[]) => {
    const wanted = selectedID.current ? nextCards.find((card) => card.id === selectedID.current) : undefined
    if (!wanted) return
    const nextColumn = reviewColumns.indexOf(wanted.column)
    const columnItems = nextCards.filter((card) => card.column === wanted.column)
    setColumnIndex(nextColumn)
    setSelectedIndexes((current) => ({ ...current, [wanted.column]: Math.max(columnItems.findIndex((card) => card.id === wanted.id), 0) }))
  }, [])

  const reload = useCallback(async () => {
    if (connection.state !== "connected") {
      setState("idle")
      setCards([])
      return
    }
    setState("loading")
    try {
      const result = await loadReviewBoard()
      setProjects(result.projects.map((project) => ({ id: project.id, name: project.name })))
      setCards(result.cards)
      setProjectIndex((current) => Math.min(current, result.projects.length))
      restoreSelection(result.cards)
      setState("ready")
    } catch (error) {
      setState("error")
      setMessage(error instanceof Error ? error.message : "Failed to load review board")
    }
  }, [connection.state, restoreSelection])

  useEffect(() => { void reload() }, [reload])
  useEffect(() => {
    if (!lastEvent || mode !== "board") return
    const timeout = setTimeout(() => void reload(), 150)
    return () => clearTimeout(timeout)
  }, [lastEvent, mode, reload])
  useEffect(() => {
    if (mode !== "board" || busy || state === "loading" || !hasActive) return
    const timeout = setTimeout(() => void reload(), 2000)
    return () => clearTimeout(timeout)
  }, [mode, busy, state, hasActive, reload])
  useEffect(() => {
    onInteractionChange?.(mode !== "board" || busy)
    return () => onInteractionChange?.(false)
  }, [mode, busy, onInteractionChange])
  useEffect(() => { if (selectedCard) selectedID.current = selectedCard.id }, [selectedCard])

  const retry = async (card: ReviewBoardCard) => {
    if (card.workflow.status !== "FAILED" || busy) return
    setBusy(true)
    setMessage("Retrying the failed review pipeline stage...")
    try {
      await performWorkflowAction(card.workflow.id, "retry", card.workflow.version, { requested_by: "review-board" })
      setMessage("Retry queued. The card will move as the stage advances.")
      await reload()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to retry workflow")
    } finally {
      setBusy(false)
    }
  }

  useKeyboard((key) => {
    if (mode !== "board" || busy) return
    if (key.name === "tab") {
      setProjectIndex((current) => (current + (key.shift ? -1 : 1) + filters.length) % filters.length)
      return
    }
    if (key.name === "f") { setActiveOnly((current) => !current); return }
    if (key.name === "r") { void reload(); return }
    if (key.name === "left" || key.name === "h") { setColumnIndex((current) => (current - 1 + reviewColumns.length) % reviewColumns.length); return }
    if (key.name === "right" || key.name === "l") { setColumnIndex((current) => (current + 1) % reviewColumns.length); return }
    if (key.name === "up" || key.name === "k") {
      setSelectedIndexes((current) => ({ ...current, [activeColumn]: Math.max((current[activeColumn] ?? 0) - 1, 0) })); return
    }
    if (key.name === "down" || key.name === "j") {
      setSelectedIndexes((current) => ({ ...current, [activeColumn]: Math.min((current[activeColumn] ?? 0) + 1, Math.max(columnCards.length - 1, 0)) })); return
    }
    if ((key.name === "return" || key.name === "enter") && selectedCard) {
      setDetailCard(selectedCard); setMode("detail"); setMessage(""); return
    }
    if (key.name === "t" && selectedCard?.workflow.status === "FAILED") void retry(selectedCard)
  })

  if (mode === "detail" && detailCard) {
    return (
      <BoardDetail
        item={createBoardItem(detailCard.project, detailCard.plan, detailCard.workflow)}
        onClose={() => { setDetailCard(null); setMode("board"); void reload() }}
        onWorkflowUpdated={(_workflow: WorkflowJob) => void reload()}
      />
    )
  }
  if (connection.state !== "connected") {
    return <EmptyState title="The local daemon is not running" detail="Start it with `make api`. Review state is durable and will return after reconnection." shortcut={{ key: "R", label: "reload" }} />
  }
  if (state === "loading" && cards.length === 0) return <Card><text fg={colors.warning}>Loading review pipeline...</text></Card>
  if (state === "error") return <Banner tone="danger"><text fg={colors.danger}>{message}</text></Banner>

  const width = terminalWidth || renderer.width
  const visibleCount = width >= 190 ? 7 : width >= 110 ? 3 : 1
  const startColumn = Math.max(0, Math.min(columnIndex - Math.floor(visibleCount / 2), reviewColumns.length - visibleCount))
  const shownColumns = reviewColumns.slice(startColumn, startColumn + visibleCount)
  const columnWidth = `${Math.floor(100 / visibleCount)}%` as `${number}%`

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader title="Review Pipeline" description="Track the handoff from Executor to checks, independent Reviewer, revision, and human approval." meta={`${selectedProject?.name ?? "All projects"} · ${activeOnly ? "active" : "all"}`} />
      <box flexDirection="row" gap={1} flexWrap="wrap">
        <Chip label={`${visibleCards.length} review item(s)`} dot={false} />
        <Chip label="Executor ≠ Reviewer" tone="success" dot={false} />
        {lastEvent ? <text fg={colors.faint}>{`live · ${lastEvent.type}`}</text> : null}
      </box>
      <box flexDirection="row" flexGrow={1} gap={1}>
        {shownColumns.map((column) => {
          const cardsInColumn = byColumn[column]
          const active = column === activeColumn
          return (
            <box key={column} width={columnWidth} flexDirection="column" borderStyle="rounded" borderColor={active ? colors.accent : colors.line} backgroundColor={colors.surface}>
              <box height={3} paddingLeft={1} paddingRight={1} flexDirection="row" justifyContent="space-between" alignItems="center" backgroundColor={active ? colors.accentTint : colors.raised}>
                <text fg={active ? colors.accent : colors.text} attributes={BOLD}>{reviewColumnLabels[column]}</text>
                <Chip label={String(cardsInColumn.length)} tone={column === "ISSUES_FOUND" && cardsInColumn.length ? "warning" : "neutral"} dot={false} />
              </box>
              <scrollbox flexGrow={1} scrollY={true} padding={1}>
                <box flexDirection="column" gap={1}>
                  {cardsInColumn.length === 0 ? <text fg={colors.faint} wrapMode="word">{reviewColumnDescriptions[column]}</text> : null}
                  {cardsInColumn.map((card, index) => <ReviewCard key={card.id} card={card} selected={active && index === selectedIndex} />)}
                </box>
              </scrollbox>
            </box>
          )
        })}
      </box>
      {message ? <Banner tone={message.toLowerCase().includes("failed") ? "danger" : busy ? "warning" : "accent"}><text fg={busy ? colors.warning : colors.muted}>{message}</text></Banner> : null}
      <KeyHints shortcuts={[{ key: "←→", label: "column" }, { key: "↑↓", label: "card" }, { key: "Enter", label: "evidence & decision" }, { key: "T", label: "retry failed stage" }, { key: "Tab", label: "project" }, { key: "F", label: "active/all" }]} />
    </box>
  )
}

function ReviewCard({ card, selected }: { card: ReviewBoardCard; selected: boolean }) {
  const tone = card.column === "ISSUES_FOUND" ? "danger" : card.column === "READY_FOR_APPROVAL" ? "warning" : card.column === "APPROVED" ? "success" : "accent"
  return (
    <box flexDirection="column" gap={1} padding={1} borderStyle="rounded" borderColor={selected ? colors.accent : colors.line} backgroundColor={selected ? colors.accentTint : colors.raised}>
      <text fg={colors.text} attributes={BOLD} wrapMode="word">{`${selected ? "▸ " : ""}${truncate(card.plan.title, 42)}`}</text>
      <text fg={colors.faint}>{truncate(card.project.name, 32)}</text>
      <Chip label={truncate(card.status, 48)} tone={tone} />
      <text fg={colors.muted}>{`Executor · ${truncate(`${card.executor.provider}/${card.executor.model}`, 38)}`}</text>
      <text fg={colors.muted}>{`Reviewer · ${truncate(`${card.reviewer.provider}/${card.reviewer.model}`, 38)}`}</text>
      <box flexDirection="row" justifyContent="space-between">
        <text fg={colors.faint}>{`exec v${card.workflow.execution_version} · cycle ${Math.max(card.review?.execution_version ?? 0, 1)}`}</text>
        <text fg={(card.review?.blocking_issues ?? 0) > 0 ? colors.danger : colors.faint}>{`${card.review?.blocking_issues ?? 0}/${card.review?.total_issues ?? 0} blocking`}</text>
      </box>
      {card.check ? <text fg={card.check.failed_steps ? colors.warning : colors.success}>{`checks ${card.check.passed_steps} passed · ${card.check.failed_steps} failed`}</text> : null}
    </box>
  )
}
''')

agent_settings = root / 'apps/tui/src/agent-settings.ts'
replace_once(
    agent_settings,
    '''export function cloneAgentSettings(settings: AgentSettings): AgentSettings {
''',
    '''export function validateDistinctAgentPair(settings: AgentSettings): string | undefined {
  const executor = settings.agents.find((agent) => agent.role === "EXECUTOR")
  const reviewer = settings.agents.find((agent) => agent.role === "REVIEWER")
  if (!executor || !reviewer || !executor.enabled || !reviewer.enabled) return undefined
  if (!executor.provider || !reviewer.provider) return undefined
  if (executor.provider === reviewer.provider && executor.model === reviewer.model) {
    return "Executor and Reviewer must not use the same explicit provider and model."
  }
  return undefined
}

export function cloneAgentSettings(settings: AgentSettings): AgentSettings {
''',
)

(root / 'apps/tui/src/agent-separation.test.ts').write_text(r'''import { describe, expect, test } from "bun:test"
import { validateDistinctAgentPair, type AgentSettings } from "./agent-settings"

const settings = {
  project_id: "project-1", version: 1,
  agents: [
    { role: "PLANNER", provider: "", model: "", temperature: 0, max_output_tokens: 1000, enabled: true, system_instruction: "" },
    { role: "EXECUTOR", provider: "deepseek", model: "coder", temperature: 0, max_output_tokens: 1000, enabled: true, system_instruction: "" },
    { role: "REVIEWER", provider: "openai", model: "reviewer", temperature: 0, max_output_tokens: 1000, enabled: true, system_instruction: "" },
  ],
  tool_policies: [], created_at: "", updated_at: "",
} satisfies AgentSettings

describe("agent separation", () => {
  test("accepts different assignments", () => expect(validateDistinctAgentPair(settings)).toBeUndefined())
  test("rejects the same explicit assignment", () => {
    const duplicate: AgentSettings = { ...settings, agents: settings.agents.map((agent) => agent.role === "REVIEWER" ? { ...agent, provider: "deepseek", model: "coder" } : { ...agent }) }
    expect(validateDistinctAgentPair(duplicate)).toContain("must not use")
  })
})
''')

(root / 'apps/tui/src/agent-settings-screen.tsx').write_text(r'''/** @jsxImportSource @opentui/react */

import type { ScrollBoxRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useCallback, useEffect, useRef, useState } from "react"

import { AgentProfileEditor } from "./agent-profile-editor"
import {
  type AgentConfig,
  type AgentRole,
  type AgentSettings,
  cloneAgentSettings,
  getAgentSettings,
  updateAgentSettings,
  validateDistinctAgentPair,
} from "./agent-settings"
import type { DaemonConnection } from "./daemon"
import { listLLMProviders, type LLMProviderInfo } from "./llm-providers"
import { listProjects, type Project } from "./projects"
import { BOLD, Banner, Card, Chip, colors, Info, KeyHints, PageHeader, Section, truncate } from "./ui"

const roles: AgentRole[] = ["PLANNER", "EXECUTOR", "REVIEWER"]
const roleDescriptions: Record<AgentRole, string> = {
  PLANNER: "Turns your requirement into a step-by-step plan.",
  EXECUTOR: "Implements the plan inside an isolated workspace.",
  REVIEWER: "Independently checks immutable changes before human approval.",
}

export function AgentSettingsScreen({ connection }: { connection: DaemonConnection }) {
  const [projects, setProjects] = useState<Project[]>([])
  const [providers, setProviders] = useState<LLMProviderInfo[]>([])
  const [projectIndex, setProjectIndex] = useState(0)
  const [roleIndex, setRoleIndex] = useState(0)
  const [settings, setSettings] = useState<AgentSettings | null>(null)
  const [editorAgent, setEditorAgent] = useState<AgentConfig | null>(null)
  const [message, setMessage] = useState("")
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const detailScrollRef = useRef<ScrollBoxRenderable>(null)

  const selectedProject = projects[projectIndex] ?? null
  const selectedProjectID = selectedProject?.id ?? ""
  const selectedRole = roles[roleIndex] ?? "PLANNER"
  const selectedAgent = settings?.agents.find((agent) => agent.role === selectedRole) ?? null
  const selectedPolicy = settings?.tool_policies.find((policy) => policy.role === selectedRole) ?? null
  const separationError = settings ? validateDistinctAgentPair(settings) : undefined

  const loadProjects = useCallback(async () => {
    if (connection.state !== "connected") {
      setProjects([]); setSettings(null); setProviders([])
      setMessage("Start the daemon before loading agent settings.")
      return
    }
    setLoading(true); setMessage("")
    try {
      const [items, providerItems] = await Promise.all([listProjects(), listLLMProviders()])
      setProjects(items); setProviders(providerItems)
      setProjectIndex((current) => Math.min(current, Math.max(items.length - 1, 0)))
      if (items.length === 0) setMessage("Create a project before configuring agents.")
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to load projects and providers")
    } finally { setLoading(false) }
  }, [connection.state])

  const mutateSettings = (mutate: (draft: AgentSettings) => void) => {
    setSettings((current) => {
      if (!current) return current
      const draft = cloneAgentSettings(current); mutate(draft); return draft
    })
    setMessage("Unsaved changes. Press S to persist them.")
  }

  const saveSettings = async () => {
    if (!settings || saving) return
    const problem = validateDistinctAgentPair(settings)
    if (problem) { setMessage(problem); return }
    setSaving(true); setMessage("Saving versioned agent settings...")
    try {
      const updated = await updateAgentSettings(settings)
      setSettings(updated); setMessage(`Agent settings saved as version ${updated.version}.`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to save agent settings")
    } finally { setSaving(false) }
  }

  useKeyboard((key) => {
    if (editorAgent || loading || saving) return
    if (key.name === "r") { void loadProjects(); return }
    if (key.name === "pageup") { detailScrollRef.current?.scrollBy(-1, "viewport"); return }
    if (key.name === "pagedown") { detailScrollRef.current?.scrollBy(1, "viewport"); return }
    if ((key.name === "down" || key.name === "j") && projects.length) { setProjectIndex((current) => Math.min(current + 1, projects.length - 1)); return }
    if ((key.name === "up" || key.name === "k") && projects.length) { setProjectIndex((current) => Math.max(current - 1, 0)); return }
    if (key.name === "tab") { setRoleIndex((current) => key.shift ? (current - 1 + roles.length) % roles.length : (current + 1) % roles.length); return }
    if (!settings || !selectedAgent || !selectedPolicy) return
    if (key.name === "return" || key.name === "enter") { setEditorAgent({ ...selectedAgent }); return }
    if (key.name === "e") { mutateSettings((draft) => { const agent = draft.agents.find((item) => item.role === selectedRole); if (agent) agent.enabled = !agent.enabled }); return }
    if (key.name === "n" && selectedRole === "EXECUTOR") { mutateSettings((draft) => { const policy = draft.tool_policies.find((item) => item.role === selectedRole); if (policy) policy.network_access = policy.network_access === "DISABLED" ? "LOOPBACK" : policy.network_access === "LOOPBACK" ? "OUTBOUND" : "DISABLED" }); return }
    if (key.name === "f" && selectedRole === "EXECUTOR") { mutateSettings((draft) => { const policy = draft.tool_policies.find((item) => item.role === selectedRole); if (policy) policy.filesystem_access = policy.filesystem_access === "READ_ONLY" ? "WORKSPACE_WRITE" : "READ_ONLY" }); return }
    if (key.name === "s") void saveSettings()
  })

  useEffect(() => { void loadProjects() }, [loadProjects])
  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected" || !selectedProjectID) { setSettings(null); return }
    setLoading(true); setMessage("")
    void getAgentSettings(selectedProjectID)
      .then((next) => { if (!disposed) setSettings(next) })
      .catch((error) => { if (!disposed) { setSettings(null); setMessage(error instanceof Error ? error.message : "Failed to load agent settings") } })
      .finally(() => { if (!disposed) setLoading(false) })
    return () => { disposed = true }
  }, [connection.state, selectedProjectID])

  if (editorAgent && settings) {
    const peerRole = editorAgent.role === "EXECUTOR" ? "REVIEWER" : editorAgent.role === "REVIEWER" ? "EXECUTOR" : undefined
    return <AgentProfileEditor agent={editorAgent} peer={peerRole ? settings.agents.find((agent) => agent.role === peerRole) : undefined} providers={providers} onCancel={() => setEditorAgent(null)} onApply={(updated) => { mutateSettings((draft) => { const index = draft.agents.findIndex((agent) => agent.role === updated.role); if (index >= 0) draft.agents[index] = updated }); setEditorAgent(null) }} />
  }

  const registered = selectedAgent?.provider ? providers.some((provider) => provider.name === selectedAgent.provider) : true
  return (
    <box flexDirection="column" gap={1} flexGrow={1}>
      <PageHeader title={selectedProject ? `Agents · ${selectedProject.name}` : "Agents"} description="Assign independent providers and models to Planner, Executor, and Reviewer while preserving role safety." meta={settings ? `settings v${settings.version}` : "no project selected"} />
      <box flexDirection="row" gap={1} flexGrow={1}>
        <box width={26} flexDirection="column" backgroundColor={colors.raised} borderStyle="rounded" borderColor={colors.line}>
          <scrollbox flexGrow={1} scrollY={true} padding={1}><box flexDirection="column">
            {projects.length === 0 ? <text fg={colors.faint}>No projects available.</text> : null}
            {projects.map((project, index) => <box key={project.id} paddingLeft={1} paddingRight={1} backgroundColor={index === projectIndex ? colors.accentTint : colors.raised}><text fg={index === projectIndex ? colors.text : colors.muted} attributes={index === projectIndex ? BOLD : 0}>{`${index === projectIndex ? "▸" : " "} ${truncate(project.name, 19)}`}</text></box>)}
          </box></scrollbox>
        </box>
        <scrollbox ref={detailScrollRef} flexGrow={1} scrollY={true}><box flexDirection="column" gap={1}>
          <Section title="Role" action="Tab switches role"><Card>
            <box flexDirection="row" gap={1}>{roles.map((role, index) => <Chip key={role} label={role.toLowerCase()} tone={index === roleIndex ? "accent" : "neutral"} dot={false} />)}</box>
            <text fg={colors.muted}>{roleDescriptions[selectedRole]}</text>
            {loading ? <text fg={colors.warning}>Loading agent settings...</text> : null}
          </Card></Section>
          {selectedAgent && selectedPolicy ? <Section title={`${selectedRole} profile`} action="Enter edits provider/model"><Card>
            <box flexDirection="row" gap={1}><Chip label={selectedAgent.enabled ? "enabled" : "disabled"} tone={selectedAgent.enabled ? "success" : "danger"} /><Chip label={registered ? "provider registered" : "provider missing"} tone={registered ? "success" : "danger"} /></box>
            <Info label="Provider" value={selectedAgent.provider || "daemon default"} tone="accent" />
            <Info label="Model" value={selectedAgent.model || "daemon default"} />
            <Info label="Temperature" value={String(selectedAgent.temperature)} />
            <Info label="Max output" value={`${selectedAgent.max_output_tokens.toLocaleString("en-US")} tokens`} />
            <box flexDirection="row" gap={1} flexWrap="wrap"><Chip label={`network: ${selectedPolicy.network_access.toLowerCase()}`} tone={selectedPolicy.network_access === "DISABLED" ? "success" : "warning"} /><Chip label={`filesystem: ${selectedPolicy.filesystem_access.toLowerCase()}`} tone={selectedPolicy.filesystem_access === "READ_ONLY" ? "success" : "accent"} /></box>
            <text fg={colors.muted}>{`Tools: ${selectedPolicy.allowed_tools.length ? selectedPolicy.allowed_tools.join(", ") : "none"}`}</text>
            <text fg={colors.faint}>{`Limits: ${formatBytes(selectedPolicy.max_file_bytes)} file · ${formatBytes(selectedPolicy.max_patch_bytes)} patch · ${formatDuration(selectedPolicy.command_timeout_ms)} command`}</text>
          </Card></Section> : null}
          <Section title="Separation policy"><Card tone={separationError ? "danger" : "success"}><text fg={separationError ? colors.danger : colors.success}>{separationError || "Executor and Reviewer assignments are independent."}</text><text fg={colors.faint}>Reviewer remains read-only and cannot receive mutation or command tools.</text></Card></Section>
          {message ? <Banner tone={message.toLowerCase().includes("failed") || message.includes("must not") ? "danger" : "warning"}><text fg={colors.muted}>{message}</text></Banner> : null}
        </box></scrollbox>
      </box>
      <KeyHints shortcuts={[{ key: "↑↓", label: "project" }, { key: "Tab", label: "role" }, { key: "Enter", label: "edit model" }, { key: "E", label: "enable/disable" }, { key: "S", label: "save" }, { key: "R", label: "reload" }]} />
    </box>
  )
}

function formatBytes(value: number): string { if (value >= 1024 * 1024 && value % (1024 * 1024) === 0) return `${value / (1024 * 1024)} MiB`; if (value >= 1024 && value % 1024 === 0) return `${value / 1024} KiB`; return `${value} B` }
function formatDuration(milliseconds: number): string { if (milliseconds >= 60000 && milliseconds % 60000 === 0) return `${milliseconds / 60000}m`; if (milliseconds >= 1000 && milliseconds % 1000 === 0) return `${milliseconds / 1000}s`; return `${milliseconds}ms` }
''')

reviews_path = root / 'apps/tui/src/reviews.ts'
replace_once(
    reviews_path,
    '''  blocking_issues: number
  failure_code?: string
''',
    '''  blocking_issues: number
  usage?: { input_tokens: number; output_tokens: number; total_tokens: number; estimated_cost_usd?: number; attempt_count?: number }
  started_at?: string
  completed_at?: string
  failure_code?: string
''',
)

navigation = root / 'apps/tui/src/navigation.ts'
navigation.write_text('''export const screenDefinitions = [
  { id: "board", label: "Board", description: "Work from plan to approval" },
  { id: "review", label: "Review", description: "Executor and Reviewer handoff" },
  { id: "agents", label: "Agents", description: "AI roles & limits" },
  { id: "settings", label: "Settings", description: "Providers & budgets" },
  { id: "system", label: "System", description: "Daemon health" },
] as const

export const screens = screenDefinitions.map((screen) => screen.id)
export type Screen = (typeof screenDefinitions)[number]["id"]
export function isScreen(value: string): value is Screen { return screens.includes(value as Screen) }
export function moveScreen(current: Screen, offset: number): Screen { const currentIndex = screens.indexOf(current); const nextIndex = (currentIndex + offset + screens.length) % screens.length; return screens[nextIndex] ?? screens[0] }
export function screenFromShortcut(keyName: string): Screen | undefined { const index = Number.parseInt(keyName, 10) - 1; if (!Number.isInteger(index) || index < 0 || index >= screens.length) return undefined; return screens[index] }
export function screenLabel(screen: Screen): string { return screenDefinitions.find((item) => item.id === screen)?.label ?? screen }
''')

(root / 'apps/tui/src/navigation.test.ts').write_text(r'''import { describe, expect, test } from "bun:test"
import { isScreen, moveScreen, screenFromShortcut, screenLabel } from "./navigation"

describe("navigation", () => {
  test("accepts product areas", () => { expect(isScreen("board")).toBe(true); expect(isScreen("review")).toBe(true); expect(isScreen("system")).toBe(true) })
  test("rejects removed screens", () => { expect(isScreen("projects")).toBe(false); expect(isScreen("jobs")).toBe(false) })
  test("moves and wraps", () => { expect(moveScreen("board", 1)).toBe("review"); expect(moveScreen("system", 1)).toBe("board"); expect(moveScreen("review", -1)).toBe("board") })
  test("maps numeric shortcuts", () => { expect(screenFromShortcut("1")).toBe("board"); expect(screenFromShortcut("2")).toBe("review"); expect(screenFromShortcut("5")).toBe("system"); expect(screenFromShortcut("6")).toBeUndefined() })
  test("returns labels", () => { expect(screenLabel("review")).toBe("Review"); expect(screenLabel("settings")).toBe("Settings") })
})
''')

app = root / 'apps/tui/src/app.tsx'
replace_once(app, 'import { BoardScreen } from "./board-screen"\n', 'import { BoardScreen } from "./board-screen"\nimport { ReviewBoardScreen } from "./review-board-screen"\n')
replace_once(app, '  const [boardInteractionActive, setBoardInteractionActive] = useState(false)\n', '  const [boardInteractionActive, setBoardInteractionActive] = useState(false)\n  const [reviewInteractionActive, setReviewInteractionActive] = useState(false)\n')
replace_once(app, '    if (boardInteractionActive) return\n', '    if (boardInteractionActive || reviewInteractionActive) return\n')
replace_once(app, '    if (activeScreen === "board") return\n', '    if (activeScreen === "board" || activeScreen === "review") return\n')
replace_once(app, '    activeScreen === "board"\n', '    activeScreen === "board" || activeScreen === "review"\n')
replace_once(app, '{ key: "1–4", label: "switch area" }', '{ key: "1–5", label: "switch area" }')
replace_once(app, '{ key: "1–4", label: "jump" }', '{ key: "1–5", label: "jump" }')
replace_once(
    app,
    '''          ) : activeScreen === "agents" ? (
            <AgentSettingsScreen connection={connection} />
''',
    '''          ) : activeScreen === "review" ? (
            <ReviewBoardScreen
              connection={connection}
              lastEvent={lastEvent}
              onInteractionChange={setReviewInteractionActive}
            />
          ) : activeScreen === "agents" ? (
            <AgentSettingsScreen connection={connection} />
''',
)
replace_once(app, '{ key: "1–4", label: "jump to Board, Agents, Settings, or System" }', '{ key: "1–5", label: "jump to Board, Review, Agents, Settings, or System" }')
replace_once(
    app,
    '''  const general: Shortcut[] = [
''',
    '''  const reviewShortcuts: Shortcut[] = [
    { key: "←→", label: "move between review stages" },
    { key: "↑↓", label: "select a review card" },
    { key: "Enter", label: "open evidence and human decision" },
    { key: "T", label: "retry a failed review stage" },
    { key: "Tab", label: "cycle project filter" },
    { key: "F", label: "toggle active-only / history" },
  ]
  const general: Shortcut[] = [
''',
)
replace_once(
    app,
    '''      <Section title={screen === "board" ? "Board" : "Current area"}>
        <Card>
          <KeyHints
            shortcuts={screen === "board" ? boardShortcuts : [{ key: "←→", label: "switch area" }]}
          />
''',
    '''      <Section title={screen === "board" ? "Board" : screen === "review" ? "Review" : "Current area"}>
        <Card>
          <KeyHints
            shortcuts={screen === "board" ? boardShortcuts : screen === "review" ? reviewShortcuts : [{ key: "←→", label: "switch area" }]}
          />
''',
)

board_detail = root / 'apps/tui/src/board-detail.tsx'
replace_once(
    board_detail,
    '''  review?: ReviewRun
  reviewIssues: ReviewIssue[]
  workspace?: Workspace
''',
    '''  review?: ReviewRun
  reviewIssues: ReviewIssue[]
  previousReview?: ReviewRun
  previousReviewIssues: ReviewIssue[]
  workspace?: Workspace
''',
)
replace_once(board_detail, 'import { type ReviewIssue, type ReviewRun, type ReviewSeverity, listReviewIssues, listReviews } from "./reviews"\n', 'import { type ReviewIssue, type ReviewRun, type ReviewSeverity, listReviewIssues, listReviews } from "./reviews"\nimport { compareReviewIssues } from "./review-board-model"\n')
replace_once(
    board_detail,
    '''    reviewIssues: [],
    diffLines: [],
''',
    '''    reviewIssues: [],
    previousReviewIssues: [],
    diffLines: [],
''',
)
replace_once(board_detail, '      const review = reviews[0]\n', '      const review = reviews[0]\n      const previousReview = reviews[1]\n')
replace_once(
    board_detail,
    '''        review ? listReviewIssues(review.id) : Promise.resolve([]),
        execution ? listCheckpoints(execution.id) : Promise.resolve([]),
''',
    '''        review ? listReviewIssues(review.id) : Promise.resolve([]),
        previousReview ? listReviewIssues(previousReview.id) : Promise.resolve([]),
        execution ? listCheckpoints(execution.id) : Promise.resolve([]),
''',
)
replace_once(
    board_detail,
    '''      const [checkSteps, reviewIssues, checkpoints, workspace] = await Promise.all([
''',
    '''      const [checkSteps, reviewIssues, previousReviewIssues, checkpoints, workspace] = await Promise.all([
''',
)
replace_once(
    board_detail,
    '''        review,
        reviewIssues,
        workspace,
''',
    '''        review,
        reviewIssues,
        previousReview,
        previousReviewIssues,
        workspace,
''',
)
replace_once(
    board_detail,
    '''  const canDecide =
''',
    '''  const reviewComparison = compareReviewIssues(snapshot.previousReviewIssues, snapshot.reviewIssues)

  const canDecide =
''',
)
replace_once(
    board_detail,
    '''            {snapshot.review?.summary ? (
''',
    '''            {snapshot.execution || snapshot.review ? (
              <Section title="Agent handoff">
                <Card>
                  <text fg={colors.accent}>{`Executor · ${snapshot.execution?.provider || "pending"}/${snapshot.execution?.model || "pending"}`}</text>
                  <text fg={colors.faint}>↓ immutable patch and automated check evidence</text>
                  <text fg={colors.accent}>{`Reviewer · ${snapshot.review?.provider || "pending"}/${snapshot.review?.model || "pending"}`}</text>
                  <text fg={colors.faint}>↓ human approval remains required</text>
                  <text fg={colors.text}>Developer decision</text>
                </Card>
              </Section>
            ) : null}

            {snapshot.review?.summary ? (
''',
)
replace_once(
    board_detail,
    '''            {snapshot.checkSteps.length > 0 ? (
''',
    '''            {snapshot.previousReview && reviewComparison.length > 0 ? (
              <Section title="Previous review comparison" action={`execution v${snapshot.previousReview.execution_version} → v${snapshot.review?.execution_version ?? workflow.execution_version}`}>
                <Card>
                  {reviewComparison.map((item) => (
                    <box key={item.key} flexDirection="row" justifyContent="space-between" gap={1}>
                      <text fg={item.status === "RESOLVED" ? colors.success : item.status === "NEW" ? colors.danger : item.status === "PARTIALLY_RESOLVED" ? colors.warning : colors.muted}>
                        {item.status.toLowerCase().replaceAll("_", " ")}
                      </text>
                      <text fg={colors.text} wrapMode="word">{item.current?.title ?? item.previous?.title ?? item.key}</text>
                    </box>
                  ))}
                </Card>
              </Section>
            ) : null}

            {snapshot.checkSteps.length > 0 ? (
''',
)

(root / 'docs/review-kanban.md').write_text('''# Review Kanban

The Review screen is a read-only projection of the durable workflow, execution, check, review, and approval records. It does not introduce a second state machine.

Columns:

1. Awaiting Review
2. AI Reviewing
3. Issues Found
4. Revision
5. Re-review
6. Approval
7. Approved

Each card shows the immutable Executor and Reviewer provider/model snapshots, execution version, review cycle, blocking issue count, and check summary. Enter opens the existing evidence and approval detail. A failed review stage can be retried with `T`; the workflow retry target determines whether workspace preparation, execution, checks, review, or publication resumes.

For revision cycles, findings are compared by their stable issue key and shown as new, still present, partially resolved, or resolved. Reviewer access remains read-only. Human approval remains bound to the latest execution version, base commit, checkpoint, and patch checksum.
''')
