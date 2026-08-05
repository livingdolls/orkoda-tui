/** @jsxImportSource @opentui/react */

import { useKeyboard, useOnResize } from "@opentui/react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { BoardDetail } from "./board-detail"
import { createBoardItem } from "./board-model"
import type { DaemonConnection } from "./daemon"
import type { ActivityEvent } from "./events"
import { loadReviewBoard, type ReviewBoardCard } from "./review-board-data"
import {
  type ReviewColumn,
  reviewColumnDescriptions,
  reviewColumnLabels,
  reviewColumns,
} from "./review-board-model"
import { Banner, BOLD, Card, Chip, colors, EmptyState, KeyHints, PageHeader, truncate } from "./ui"
import { performWorkflowAction, type WorkflowJob } from "./workflow-jobs"

const initialIndexes = Object.fromEntries(reviewColumns.map((column) => [column, 0])) as Record<
  ReviewColumn,
  number
>

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
    () =>
      cards.filter(
        (card) =>
          (!selectedProject?.id || card.project.id === selectedProject.id) &&
          (!activeOnly || card.column !== "APPROVED"),
      ),
    [cards, selectedProject?.id, activeOnly],
  )
  const byColumn = useMemo(
    () =>
      Object.fromEntries(
        reviewColumns.map((column) => [
          column,
          visibleCards.filter((card) => card.column === column),
        ]),
      ) as Record<ReviewColumn, ReviewBoardCard[]>,
    [visibleCards],
  )
  const activeColumn = reviewColumns[columnIndex] ?? "AWAITING_REVIEW"
  const columnCards = byColumn[activeColumn]
  const selectedIndex = Math.min(selectedIndexes[activeColumn], Math.max(columnCards.length - 1, 0))
  const selectedCard = columnCards[selectedIndex] ?? null
  const hasActive = cards.some((card) => card.column !== "APPROVED")

  const restoreSelection = useCallback((nextCards: ReviewBoardCard[]) => {
    const wanted = selectedID.current
      ? nextCards.find((card) => card.id === selectedID.current)
      : undefined
    if (!wanted) return
    const nextColumn = reviewColumns.indexOf(wanted.column)
    const columnItems = nextCards.filter((card) => card.column === wanted.column)
    setColumnIndex(nextColumn)
    setSelectedIndexes((current) => ({
      ...current,
      [wanted.column]: Math.max(
        columnItems.findIndex((card) => card.id === wanted.id),
        0,
      ),
    }))
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

  useEffect(() => {
    void reload()
  }, [reload])
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
  useEffect(() => {
    if (selectedCard) selectedID.current = selectedCard.id
  }, [selectedCard])

  const retry = async (card: ReviewBoardCard) => {
    if (card.workflow.status !== "FAILED" || busy) return
    setBusy(true)
    setMessage("Retrying the failed review pipeline stage...")
    try {
      await performWorkflowAction(card.workflow.id, "retry", card.workflow.version, {
        requested_by: "review-board",
      })
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
      setProjectIndex(
        (current) => (current + (key.shift ? -1 : 1) + filters.length) % filters.length,
      )
      return
    }
    if (key.name === "f") {
      setActiveOnly((current) => !current)
      return
    }
    if (key.name === "r") {
      void reload()
      return
    }
    if (key.name === "left" || key.name === "h") {
      setColumnIndex((current) => (current - 1 + reviewColumns.length) % reviewColumns.length)
      return
    }
    if (key.name === "right" || key.name === "l") {
      setColumnIndex((current) => (current + 1) % reviewColumns.length)
      return
    }
    if (key.name === "up" || key.name === "k") {
      setSelectedIndexes((current) => ({
        ...current,
        [activeColumn]: Math.max((current[activeColumn] ?? 0) - 1, 0),
      }))
      return
    }
    if (key.name === "down" || key.name === "j") {
      setSelectedIndexes((current) => ({
        ...current,
        [activeColumn]: Math.min(
          (current[activeColumn] ?? 0) + 1,
          Math.max(columnCards.length - 1, 0),
        ),
      }))
      return
    }
    if ((key.name === "return" || key.name === "enter") && selectedCard) {
      setDetailCard(selectedCard)
      setMode("detail")
      setMessage("")
      return
    }
    if (key.name === "t" && selectedCard?.workflow.status === "FAILED") void retry(selectedCard)
  })

  if (mode === "detail" && detailCard) {
    return (
      <BoardDetail
        item={createBoardItem(detailCard.project, detailCard.plan, detailCard.workflow)}
        onClose={() => {
          setDetailCard(null)
          setMode("board")
          void reload()
        }}
        onWorkflowUpdated={(_workflow: WorkflowJob) => void reload()}
      />
    )
  }
  if (connection.state !== "connected") {
    return (
      <EmptyState
        title="The local daemon is not running"
        detail="Start it with `make api`. Review state is durable and will return after reconnection."
        shortcut={{ key: "R", label: "reload" }}
      />
    )
  }
  if (state === "loading" && cards.length === 0)
    return (
      <Card>
        <text fg={colors.warning}>Loading review pipeline...</text>
      </Card>
    )
  if (state === "error")
    return (
      <Banner tone="danger">
        <text fg={colors.danger}>{message}</text>
      </Banner>
    )

  const width = terminalWidth || renderer.width
  const visibleCount = width >= 190 ? 7 : width >= 110 ? 3 : 1
  const startColumn = Math.max(
    0,
    Math.min(columnIndex - Math.floor(visibleCount / 2), reviewColumns.length - visibleCount),
  )
  const shownColumns = reviewColumns.slice(startColumn, startColumn + visibleCount)
  const columnWidth = `${Math.floor(100 / visibleCount)}%` as `${number}%`

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title="Review Pipeline"
        description="Track the handoff from Executor to checks, independent Reviewer, revision, and human approval."
        meta={`${selectedProject?.name ?? "All projects"} · ${activeOnly ? "active" : "all"}`}
      />
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
            <box
              key={column}
              width={columnWidth}
              flexDirection="column"
              borderStyle="rounded"
              borderColor={active ? colors.accent : colors.line}
              backgroundColor={colors.surface}
            >
              <box
                height={3}
                paddingLeft={1}
                paddingRight={1}
                flexDirection="row"
                justifyContent="space-between"
                alignItems="center"
                backgroundColor={active ? colors.accentTint : colors.raised}
              >
                <text fg={active ? colors.accent : colors.text} attributes={BOLD}>
                  {reviewColumnLabels[column]}
                </text>
                <Chip
                  label={String(cardsInColumn.length)}
                  tone={column === "ISSUES_FOUND" && cardsInColumn.length ? "warning" : "neutral"}
                  dot={false}
                />
              </box>
              <scrollbox flexGrow={1} scrollY={true} padding={1}>
                <box flexDirection="column" gap={1}>
                  {cardsInColumn.length === 0 ? (
                    <text fg={colors.faint} wrapMode="word">
                      {reviewColumnDescriptions[column]}
                    </text>
                  ) : null}
                  {cardsInColumn.map((card, index) => (
                    <ReviewCard
                      key={card.id}
                      card={card}
                      selected={active && index === selectedIndex}
                    />
                  ))}
                </box>
              </scrollbox>
            </box>
          )
        })}
      </box>
      {message ? (
        <Banner
          tone={message.toLowerCase().includes("failed") ? "danger" : busy ? "warning" : "accent"}
        >
          <text fg={busy ? colors.warning : colors.muted}>{message}</text>
        </Banner>
      ) : null}
      <KeyHints
        shortcuts={[
          { key: "←→", label: "column" },
          { key: "↑↓", label: "card" },
          { key: "Enter", label: "evidence & decision" },
          { key: "T", label: "retry failed stage" },
          { key: "Tab", label: "project" },
          { key: "F", label: "active/all" },
        ]}
      />
    </box>
  )
}

function ReviewCard({ card, selected }: { card: ReviewBoardCard; selected: boolean }) {
  const tone =
    card.column === "ISSUES_FOUND"
      ? "danger"
      : card.column === "READY_FOR_APPROVAL"
        ? "warning"
        : card.column === "APPROVED"
          ? "success"
          : "accent"
  return (
    <box
      flexDirection="column"
      gap={1}
      padding={1}
      borderStyle="rounded"
      borderColor={selected ? colors.accent : colors.line}
      backgroundColor={selected ? colors.accentTint : colors.raised}
    >
      <text
        fg={colors.text}
        attributes={BOLD}
        wrapMode="word"
      >{`${selected ? "▸ " : ""}${truncate(card.plan.title, 42)}`}</text>
      <text fg={colors.faint}>{truncate(card.project.name, 32)}</text>
      <Chip label={truncate(card.status, 48)} tone={tone} />
      <text
        fg={colors.muted}
      >{`Executor · ${truncate(`${card.executor.provider}/${card.executor.model}`, 38)}`}</text>
      <text
        fg={colors.muted}
      >{`Reviewer · ${truncate(`${card.reviewer.provider}/${card.reviewer.model}`, 38)}`}</text>
      <box flexDirection="row" justifyContent="space-between">
        <text
          fg={colors.faint}
        >{`exec v${card.workflow.execution_version} · cycle ${Math.max(card.review?.execution_version ?? 0, 1)}`}</text>
        <text
          fg={(card.review?.blocking_issues ?? 0) > 0 ? colors.danger : colors.faint}
        >{`${card.review?.blocking_issues ?? 0}/${card.review?.total_issues ?? 0} blocking`}</text>
      </box>
      {card.check ? (
        <text
          fg={card.check.failed_steps ? colors.warning : colors.success}
        >{`checks ${card.check.passed_steps} passed · ${card.check.failed_steps} failed`}</text>
      ) : null}
    </box>
  )
}
