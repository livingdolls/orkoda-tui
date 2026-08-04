/** @jsxImportSource @opentui/react */

import { useKeyboard, useOnResize } from "@opentui/react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { loadBoardItems } from "./board-data"
import { BoardDetail } from "./board-detail"
import {
  type BoardAction,
  type BoardColumn,
  type BoardItem,
  boardActions,
  boardColumns,
  columnDescriptions,
  columnLabels,
  createBoardItem,
  workflowTone,
} from "./board-model"
import { BoardNewProject } from "./board-new-project"
import type { DaemonConnection } from "./daemon"
import type { ActivityEvent } from "./events"
import { PlanEditor } from "./plan-editor"
import {
  generateRepositorySummary,
  getCurrentRepositorySummary,
  getPlanningContext,
  normalizePlan,
} from "./planning"
import { getCurrentPlanningRun, type PlanningRun, startPlanningRun } from "./planning-agent"
import { PlanningQuestionEditor } from "./planning-question-editor"
import type { Project } from "./projects"
import {
  Banner,
  BOLD,
  Card,
  Chip,
  colors,
  EmptyState,
  Key,
  KeyHints,
  PageHeader,
  truncate,
} from "./ui"
import { createWorkflowJob, performWorkflowAction, type WorkflowJob } from "./workflow-jobs"

type BoardMode = "board" | "new-project" | "new-plan" | "questions" | "detail"
type LoadState = "idle" | "loading" | "ready" | "error"

const initialIndexes: Record<BoardColumn, number> = {
  PLANNING: 0,
  READY: 0,
  WORKING: 0,
  NEEDS_USER: 0,
  DONE: 0,
}

export function BoardScreen({
  connection,
  lastEvent,
  onInteractionChange,
}: {
  connection: DaemonConnection
  lastEvent: ActivityEvent | null
  onInteractionChange?: (active: boolean) => void
}) {
  const [projects, setProjects] = useState<Project[]>([])
  const [items, setItems] = useState<BoardItem[]>([])
  const [loadState, setLoadState] = useState<LoadState>("idle")
  const [message, setMessage] = useState("")
  const [mode, setMode] = useState<BoardMode>("board")
  const [projectFilterIndex, setProjectFilterIndex] = useState(0)
  const [activeColumnIndex, setActiveColumnIndex] = useState(0)
  const [selectedIndexes, setSelectedIndexes] = useState(initialIndexes)
  const [activeOnly, setActiveOnly] = useState(false)
  const [actionMenuOpen, setActionMenuOpen] = useState(false)
  const [actionIndex, setActionIndex] = useState(0)
  const [questionRun, setQuestionRun] = useState<PlanningRun | null>(null)
  const [detailItem, setDetailItem] = useState<BoardItem | null>(null)
  const [editorProject, setEditorProject] = useState<Project | null>(null)
  const [busy, setBusy] = useState(false)
  const [terminalWidth, setTerminalWidth] = useState(0)
  const selectedItemIDRef = useRef<string | null>(null)
  const activeFilterIDRef = useRef("")
  const activeOnlyRef = useRef(false)
  const renderer = useOnResize((width) => setTerminalWidth(width))

  const filters = useMemo(
    () => [
      { id: "", name: "All projects" },
      ...projects.map((project) => ({ id: project.id, name: project.name })),
    ],
    [projects],
  )
  const activeFilter = filters[projectFilterIndex] ?? filters[0]
  const filteredItems = useMemo(
    () =>
      items.filter(
        (item) =>
          (!activeFilter?.id || item.project.id === activeFilter.id) &&
          (!activeOnly || item.column !== "DONE"),
      ),
    [items, activeFilter?.id, activeOnly],
  )
  const itemsByColumn = useMemo(
    () =>
      Object.fromEntries(
        boardColumns.map((column) => [
          column,
          filteredItems.filter((item) => item.column === column),
        ]),
      ) as Record<BoardColumn, BoardItem[]>,
    [filteredItems],
  )

  const activeColumn = boardColumns[activeColumnIndex] ?? "PLANNING"
  const columnItems = itemsByColumn[activeColumn]
  const selectedIndex = Math.min(selectedIndexes[activeColumn], Math.max(columnItems.length - 1, 0))
  const selectedItem = columnItems[selectedIndex] ?? null
  const selectedActions = selectedItem ? boardActions(selectedItem) : []

  const restoreSelection = useCallback((nextItems: BoardItem[]) => {
    const wantedID = selectedItemIDRef.current
    const filterID = activeFilterIDRef.current
    const onlyActive = activeOnlyRef.current
    const wanted = wantedID ? nextItems.find((item) => item.id === wantedID) : undefined
    if (wanted) {
      const nextColumnIndex = boardColumns.indexOf(wanted.column)
      const nextColumnItems = nextItems.filter(
        (item) =>
          item.column === wanted.column &&
          (!filterID || item.project.id === filterID) &&
          (!onlyActive || item.column !== "DONE"),
      )
      const nextIndex = Math.max(
        nextColumnItems.findIndex((item) => item.id === wanted.id),
        0,
      )
      setActiveColumnIndex(nextColumnIndex)
      setSelectedIndexes((current) => ({ ...current, [wanted.column]: nextIndex }))
      return
    }

    const first = nextItems.find(
      (item) =>
        (!filterID || item.project.id === filterID) && (!onlyActive || item.column !== "DONE"),
    )
    if (first) {
      selectedItemIDRef.current = first.id
      setActiveColumnIndex(boardColumns.indexOf(first.column))
      setSelectedIndexes((current) => ({ ...current, [first.column]: 0 }))
    } else {
      selectedItemIDRef.current = null
    }
  }, [])

  const reload = useCallback(async () => {
    if (connection.state !== "connected") {
      setLoadState("idle")
      setProjects([])
      setItems([])
      return
    }
    setLoadState("loading")
    try {
      const result = await loadBoardItems()
      setProjects(result.projects)
      setItems(result.items)
      setProjectFilterIndex((current) => Math.min(current, result.projects.length))
      restoreSelection(result.items)
      setLoadState("ready")
    } catch (error) {
      setLoadState("error")
      setMessage(error instanceof Error ? error.message : "Failed to load the board")
    }
  }, [connection.state, restoreSelection])

  useEffect(() => {
    void reload()
  }, [reload])

  useEffect(() => {
    if (!lastEvent || mode !== "board") return
    const timeout = setTimeout(() => void reload(), 120)
    return () => clearTimeout(timeout)
  }, [lastEvent, mode, reload])

  useEffect(() => {
    onInteractionChange?.(mode !== "board" || actionMenuOpen || busy)
    return () => onInteractionChange?.(false)
  }, [mode, actionMenuOpen, busy, onInteractionChange])

  useEffect(() => {
    activeFilterIDRef.current = activeFilter?.id ?? ""
  }, [activeFilter?.id])

  useEffect(() => {
    activeOnlyRef.current = activeOnly
  }, [activeOnly])

  useEffect(() => {
    if (selectedItem) selectedItemIDRef.current = selectedItem.id
  }, [selectedItem])

  const contextProject = (): Project | null => {
    if (selectedItem) return selectedItem.project
    if (activeFilter?.id) {
      return projects.find((project) => project.id === activeFilter.id) ?? null
    }
    return projects[0] ?? null
  }

  const openNewPlan = () => {
    const project = contextProject()
    if (!project) {
      setMessage("Add a project before creating work.")
      return
    }
    setEditorProject(project)
    setMode("new-plan")
    setMessage("")
  }

  const preparePlan = async (item: BoardItem) => {
    const repository = item.repository
    if (!repository || busy) {
      setMessage("This project does not have a repository to scan.")
      return
    }
    setBusy(true)
    setMessage("Preparing the plan: scanning the repository...")
    try {
      await getCurrentRepositorySummary(repository.id).catch(() =>
        generateRepositorySummary(repository.id),
      )
      setMessage("Preparing the plan: locking it to the current code snapshot...")
      await getPlanningContext(item.plan.id).catch(() => normalizePlan(item.plan.id))
      setMessage("The Planning Agent is preparing implementation steps...")
      const run = await startPlanningRun(item.plan.id)
      if (run.status === "NEEDS_INPUT") {
        setQuestionRun(run)
        setMode("questions")
        setMessage("")
      } else {
        setMessage(
          run.status === "COMPLETED"
            ? `Plan prepared with ${run.result?.steps.length ?? 0} implementation step(s).`
            : `Planning finished with status ${run.status}.`,
        )
        await reload()
      }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to prepare the plan")
    } finally {
      setBusy(false)
    }
  }

  const answerQuestions = async (item: BoardItem) => {
    setBusy(true)
    setMessage("Loading the Planning Agent questions...")
    try {
      const run = await getCurrentPlanningRun(item.plan.id)
      if (run.status !== "NEEDS_INPUT") {
        setMessage(`Planning no longer needs input; current status is ${run.status}.`)
        await reload()
        return
      }
      setQuestionRun(run)
      setMode("questions")
      setMessage("")
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to load planning questions")
    } finally {
      setBusy(false)
    }
  }

  const startWork = async (item: BoardItem) => {
    const repository = item.repository
    if (!repository || busy) return
    const baseBranch = repository.current_branch
    if (!baseBranch || baseBranch === "HEAD") {
      setMessage("Choose a concrete local branch before starting this work.")
      return
    }
    setBusy(true)
    setMessage(`Creating an isolated workflow from ${baseBranch}...`)
    try {
      const created =
        item.workflow?.status === "READY"
          ? item.workflow
          : await createWorkflowJob(item.project.id, {
              plan_id: item.plan.id,
              repository_id: repository.id,
              base_branch: baseBranch,
            })
      const started = await performWorkflowAction(created.id, "start", created.version, {
        requested_by: "kanban-board",
        base_branch: baseBranch,
      })
      setItems((current) =>
        current.map((candidate) =>
          candidate.id === item.id
            ? createBoardItem(candidate.project, candidate.plan, started)
            : candidate,
        ),
      )
      setMessage("Work started. The card will move automatically as each stage finishes.")
      await reload()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to start the workflow")
    } finally {
      setBusy(false)
    }
  }

  const transitionWorkflow = async (item: BoardItem, action: "retry" | "cancel") => {
    if (!item.workflow || busy) return
    setBusy(true)
    setMessage(`${action === "retry" ? "Retrying" : "Cancelling"} workflow...`)
    try {
      const workflow = await performWorkflowAction(
        item.workflow.id,
        action,
        item.workflow.version,
        { requested_by: "kanban-board" },
      )
      setItems((current) =>
        current.map((candidate) =>
          candidate.id === item.id
            ? createBoardItem(candidate.project, candidate.plan, workflow)
            : candidate,
        ),
      )
      setMessage(`Workflow is now ${workflow.status.toLowerCase().replaceAll("_", " ")}.`)
      await reload()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : `Failed to ${action} the workflow`)
    } finally {
      setBusy(false)
    }
  }

  const executeAction = async (action: BoardAction, item: BoardItem) => {
    setActionMenuOpen(false)
    setActionIndex(0)
    switch (action.id) {
      case "prepare-plan":
        await preparePlan(item)
        break
      case "answer-questions":
        await answerQuestions(item)
        break
      case "start-work":
        await startWork(item)
        break
      case "open-details":
        setDetailItem(item)
        setMode("detail")
        setMessage("")
        break
      case "retry":
        await transitionWorkflow(item, "retry")
        break
      case "cancel":
        await transitionWorkflow(item, "cancel")
        break
    }
  }

  const moveColumn = (offset: number) => {
    const next = (activeColumnIndex + offset + boardColumns.length) % boardColumns.length
    setActiveColumnIndex(next)
    const nextColumn = boardColumns[next] ?? "PLANNING"
    const nextItems = itemsByColumn[nextColumn]
    const nextIndex = Math.min(selectedIndexes[nextColumn], Math.max(nextItems.length - 1, 0))
    selectedItemIDRef.current = nextItems[nextIndex]?.id ?? null
  }

  const moveCard = (offset: number) => {
    if (columnItems.length === 0) return
    const next = Math.max(0, Math.min(selectedIndex + offset, columnItems.length - 1))
    setSelectedIndexes((current) => ({ ...current, [activeColumn]: next }))
    selectedItemIDRef.current = columnItems[next]?.id ?? null
  }

  useKeyboard((key) => {
    if (mode !== "board" || busy || loadState === "loading") return

    if (actionMenuOpen) {
      if (key.name === "escape") {
        setActionMenuOpen(false)
        return
      }
      if (key.name === "down" || key.name === "j") {
        setActionIndex((current) => Math.min(current + 1, Math.max(selectedActions.length - 1, 0)))
        return
      }
      if (key.name === "up" || key.name === "k") {
        setActionIndex((current) => Math.max(current - 1, 0))
        return
      }
      if (key.name === "return" || key.name === "enter") {
        const action = selectedActions[actionIndex]
        if (action && selectedItem) void executeAction(action, selectedItem)
      }
      return
    }

    if (key.shift && key.name === "n") {
      setMode("new-project")
      setMessage("")
      return
    }
    if (key.name === "n") {
      openNewPlan()
      return
    }
    if (key.name === "tab") {
      setProjectFilterIndex(
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
      moveColumn(-1)
      return
    }
    if (key.name === "right" || key.name === "l") {
      moveColumn(1)
      return
    }
    if (key.name === "up" || key.name === "k") {
      moveCard(-1)
      return
    }
    if (key.name === "down" || key.name === "j") {
      moveCard(1)
      return
    }
    if (key.name === "space" || key.name === " ") {
      if (selectedActions.length > 0) {
        setActionIndex(0)
        setActionMenuOpen(true)
      }
      return
    }
    if (key.name === "return" || key.name === "enter") {
      const action = selectedActions[0]
      if (action && selectedItem) void executeAction(action, selectedItem)
    }
  })

  if (mode === "new-project") {
    return (
      <BoardNewProject
        onCreated={(project) => {
          setMode("board")
          setMessage(`Project ${project.name} added. Create its first work item with N.`)
          void reload().then(() => setProjectFilterIndex(projects.length + 1))
        }}
        onCancel={() => setMode("board")}
      />
    )
  }

  if (mode === "new-plan" && editorProject) {
    return (
      <PlanEditor
        projectID={editorProject.id}
        projectName={editorProject.name}
        onSaved={(plan) => {
          setMode("board")
          setMessage(`Work item “${plan.title}” created. Press Enter on its card to prepare it.`)
          void reload()
        }}
        onCancel={() => setMode("board")}
      />
    )
  }

  if (mode === "questions" && questionRun) {
    return (
      <PlanningQuestionEditor
        run={questionRun}
        onSubmitted={(run) => {
          setQuestionRun(null)
          setMode("board")
          setMessage(
            run.status === "COMPLETED"
              ? `Plan prepared with ${run.result?.steps.length ?? 0} step(s).`
              : `Planning returned ${run.status}.`,
          )
          void reload()
        }}
        onCancel={() => {
          setQuestionRun(null)
          setMode("board")
        }}
      />
    )
  }

  if (mode === "detail" && detailItem) {
    return (
      <BoardDetail
        item={detailItem}
        onClose={() => {
          setDetailItem(null)
          setMode("board")
          void reload()
        }}
        onWorkflowUpdated={(workflow: WorkflowJob) => {
          setDetailItem((current) =>
            current ? createBoardItem(current.project, current.plan, workflow) : current,
          )
          setItems((current) =>
            current.map((candidate) =>
              candidate.id === detailItem.id
                ? createBoardItem(candidate.project, candidate.plan, workflow)
                : candidate,
            ),
          )
        }}
      />
    )
  }

  if (connection.state !== "connected") {
    return (
      <EmptyState
        title="The local daemon is not running"
        detail="Start it with `make api`. The board reconnects automatically when the daemon becomes available."
        shortcut={{ key: "R", label: "reload board" }}
      />
    )
  }

  if (loadState === "loading" && projects.length === 0) {
    return (
      <Card>
        <text fg={colors.warning}>Loading the kanban board...</text>
        <text fg={colors.faint}>Reading projects, plans, and current workflow summaries.</text>
      </Card>
    )
  }

  if (loadState === "error") {
    return (
      <box flexDirection="column" flexGrow={1} gap={1}>
        <Banner tone="danger">
          <text fg={colors.danger} attributes={BOLD}>
            Unable to load the board
          </text>
          <text fg={colors.muted}>{message}</text>
        </Banner>
        <KeyHints shortcuts={[{ key: "R", label: "try again" }]} />
      </box>
    )
  }

  if (projects.length === 0) {
    return (
      <EmptyState
        title="No project yet"
        detail="Add a Git repository. After that, every planning, execution, review, and approval step stays on this board."
        shortcut={{ key: "Shift+N", label: "add your first project" }}
      />
    )
  }

  const width = terminalWidth || renderer.width
  const visibleCount = width >= 150 ? 5 : width >= 100 ? 3 : 1
  const startColumn = Math.max(
    0,
    Math.min(activeColumnIndex - Math.floor(visibleCount / 2), boardColumns.length - visibleCount),
  )
  const visibleColumns = boardColumns.slice(startColumn, startColumn + visibleCount)
  const columnWidth = `${Math.floor(100 / visibleCount)}%` as `${number}%`

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title="Board"
        description="One card follows each piece of work from planning to implementation, review, approval, and publication."
        meta={`${activeFilter?.name ?? "All projects"} · ${activeOnly ? "active only" : "all work"}`}
      />
      <box flexDirection="row" gap={1} alignItems="center" flexWrap="wrap">
        <Chip
          label={`project: ${activeFilter?.name ?? "All projects"}`}
          tone="accent"
          dot={false}
        />
        <Chip label={`${filteredItems.length} work item(s)`} dot={false} />
        {lastEvent ? <text fg={colors.faint}>{`live · ${lastEvent.type}`}</text> : null}
      </box>

      <box flexDirection="row" flexGrow={1} gap={1}>
        {visibleColumns.map((column) => {
          const columnIndex = boardColumns.indexOf(column)
          const cards = itemsByColumn[column]
          const isActive = columnIndex === activeColumnIndex
          return (
            <box
              key={column}
              width={columnWidth}
              flexDirection="column"
              borderStyle="rounded"
              borderColor={isActive ? colors.accent : colors.line}
              backgroundColor={colors.surface}
            >
              <box
                height={3}
                paddingLeft={1}
                paddingRight={1}
                flexDirection="row"
                justifyContent="space-between"
                alignItems="center"
                backgroundColor={isActive ? colors.accentTint : colors.raised}
              >
                <text fg={isActive ? colors.accent : colors.text} attributes={BOLD}>
                  {columnLabels[column]}
                </text>
                <Chip
                  label={String(cards.length)}
                  tone={column === "NEEDS_USER" && cards.length > 0 ? "warning" : "neutral"}
                  dot={false}
                />
              </box>
              <scrollbox flexGrow={1} scrollY={true} padding={1}>
                <box flexDirection="column" gap={1}>
                  {cards.length === 0 ? (
                    <box padding={1}>
                      <text fg={colors.faint} wrapMode="word">
                        {columnDescriptions[column]}
                      </text>
                    </box>
                  ) : null}
                  {cards.map((item, index) => (
                    <BoardCard
                      key={item.id}
                      item={item}
                      selected={isActive && index === selectedIndex}
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
          { key: "Enter", label: "next action" },
          { key: "Space", label: "all actions" },
          { key: "N", label: "new work" },
          { key: "Shift+N", label: "new project" },
          { key: "Tab", label: "project filter" },
          { key: "F", label: "active / all" },
        ]}
      />

      {actionMenuOpen && selectedItem ? (
        <box
          position="absolute"
          left="20%"
          right="20%"
          top="22%"
          padding={2}
          flexDirection="column"
          gap={1}
          borderStyle="rounded"
          borderColor={colors.accent}
          backgroundColor={colors.canvas}
        >
          <PageHeader
            title="What do you want to do?"
            description={selectedItem.plan.title}
            meta={`${selectedActions.length} action(s)`}
          />
          {selectedActions.map((action, index) => {
            const selected = index === actionIndex
            return (
              <box
                key={action.id}
                flexDirection="column"
                padding={1}
                backgroundColor={selected ? colors.accentTint : colors.surface}
              >
                <box flexDirection="row" gap={1}>
                  <text fg={selected ? colors.accent : colors.faint}>{selected ? "▸" : " "}</text>
                  <text fg={selected ? colors.text : colors.muted} attributes={selected ? BOLD : 0}>
                    {action.label}
                  </text>
                </box>
                <text fg={colors.faint}>{action.description}</text>
              </box>
            )
          })}
          <KeyHints
            shortcuts={[
              { key: "↑↓", label: "select" },
              { key: "Enter", label: "run action" },
              { key: "Esc", label: "close" },
            ]}
          />
        </box>
      ) : null}
    </box>
  )
}

function BoardCard({ item, selected }: { item: BoardItem; selected: boolean }) {
  const tone = item.workflow
    ? workflowTone(item.workflow.status)
    : item.column === "NEEDS_USER"
      ? "warning"
      : item.column === "READY"
        ? "accent"
        : item.column === "DONE"
          ? "success"
          : "neutral"
  return (
    <box
      flexDirection="column"
      gap={1}
      padding={1}
      borderStyle="rounded"
      borderColor={selected ? colors.accent : colors.line}
      backgroundColor={selected ? colors.accentTint : colors.raised}
    >
      <box flexDirection="row" gap={1}>
        <text fg={selected ? colors.accent : colors.faint}>{selected ? "▸" : " "}</text>
        <text fg={colors.text} attributes={BOLD} wrapMode="word">
          {truncate(item.plan.title, 42)}
        </text>
      </box>
      <text fg={colors.faint}>{truncate(item.project.name, 34)}</text>
      <Chip label={truncate(item.displayStatus, 38)} tone={tone} />
      {item.attentionReason ? (
        <text fg={colors.warning} wrapMode="word">
          {truncate(item.attentionReason, 64)}
        </text>
      ) : null}
      <box flexDirection="row" justifyContent="space-between">
        <text fg={colors.faint}>{item.repository?.current_branch || "no branch"}</text>
        {item.workflow ? (
          <text fg={colors.faint}>{`v${item.workflow.execution_version}`}</text>
        ) : (
          <Key>{item.plan.status === "NEEDS_INPUT" ? "Enter" : "Space"}</Key>
        )}
      </box>
    </box>
  )
}
