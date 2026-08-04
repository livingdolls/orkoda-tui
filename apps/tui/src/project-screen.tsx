/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useEffect, useState } from "react"

import type { DaemonConnection } from "./daemon"
import {
  buildDirectoryPickerItems,
  type DirectoryListing,
  initialDirectory,
  listDirectories,
  visibleDirectoryItems,
} from "./directory-picker"
import { PlanEditor } from "./plan-editor"
import {
  generateRepositorySummary,
  getCurrentRepositorySummary,
  getPlanningContext,
  normalizePlan,
  type PlanningContext,
  type RepositorySummary,
} from "./planning"
import { getCurrentPlanningRun, type PlanningRun, startPlanningRun } from "./planning-agent"
import { PlanningQuestionEditor } from "./planning-question-editor"
import { listPlans, type Plan } from "./plans"
import {
  createProject,
  deleteProject,
  listProjects,
  type Project,
  refreshProject,
} from "./projects"
import {
  colors,
  EmptyState,
  Metric,
  PageIntro,
  Panel,
  SectionHeading,
  ShortcutBar,
  StatusBadge,
} from "./ui"

type ProjectMode = "list" | "name" | "picker" | "delete" | "plan" | "questions"
type ProjectLoadState = "idle" | "loading" | "ready" | "error"

export function ProjectScreen({
  connection,
  onInteractionChange,
}: {
  connection: DaemonConnection
  onInteractionChange: (active: boolean) => void
}) {
  const [projectList, setProjectList] = useState<Project[]>([])
  const [loadState, setLoadState] = useState<ProjectLoadState>("idle")
  const [message, setMessage] = useState("")
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [mode, setMode] = useState<ProjectMode>("list")
  const [draftName, setDraftName] = useState("")
  const [busy, setBusy] = useState(false)
  const [pickerLoading, setPickerLoading] = useState(false)
  const [pickerListing, setPickerListing] = useState<DirectoryListing | null>(null)
  const [pickerIndex, setPickerIndex] = useState(0)
  const [planList, setPlanList] = useState<Plan[]>([])
  const [planLoading, setPlanLoading] = useState(false)
  const [repositorySummary, setRepositorySummary] = useState<RepositorySummary | null>(null)
  const [planningContext, setPlanningContext] = useState<PlanningContext | null>(null)
  const [planningRun, setPlanningRun] = useState<PlanningRun | null>(null)

  const selectedProject = projectList[selectedIndex] ?? null
  const selectedRepository = selectedProject?.repositories[0] ?? null
  const latestPlan = planList[0] ?? null
  const pickerItems = pickerListing ? buildDirectoryPickerItems(pickerListing) : []
  const selectedRepositoryID = selectedRepository?.id ?? ""
  const latestPlanID = latestPlan?.id ?? ""
  const repositorySummaryID = repositorySummary?.id ?? ""
  const planningContextID = planningContext?.id ?? ""

  const applyPlanningRun = (run: PlanningRun) => {
    setPlanningRun(run)
    let status: Plan["status"] | null = null
    if (run.status === "NEEDS_INPUT") {
      status = "NEEDS_INPUT"
    } else if (run.status === "COMPLETED") {
      status = "READY"
    } else if (run.status === "RUNNING") {
      status = "PLANNING"
    } else if (run.status === "FAILED" || run.status === "CANCELLED") {
      status = "DRAFT"
    }
    if (status && latestPlanID) {
      setPlanList((current) =>
        current.map((plan) => (plan.id === latestPlanID ? { ...plan, status } : plan)),
      )
    }
  }

  const reloadProjects = async () => {
    if (connection.state !== "connected") {
      setLoadState("idle")
      setMessage("Start the daemon before loading projects.")
      return
    }
    setLoadState("loading")
    setMessage("")
    try {
      const projects = await listProjects()
      setProjectList(projects)
      setSelectedIndex((current) => Math.min(current, Math.max(projects.length - 1, 0)))
      setLoadState("ready")
    } catch (error) {
      setLoadState("error")
      setMessage(error instanceof Error ? error.message : "Failed to load projects")
    }
  }

  const openDirectory = async (directory: string) => {
    if (pickerLoading || busy) {
      return
    }

    setMode("picker")
    setPickerLoading(true)
    setMessage("")
    try {
      const listing = await listDirectories(directory)
      setPickerListing(listing)
      setPickerIndex(0)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to read directory")
    } finally {
      setPickerLoading(false)
    }
  }

  const startDirectoryPicker = () => {
    if (!draftName.trim()) {
      setMessage("Project name is required.")
      return
    }
    void openDirectory(pickerListing?.currentPath ?? initialDirectory())
  }

  const submitProject = async (repositoryPath: string) => {
    if (busy) {
      return
    }
    if (!draftName.trim()) {
      setMessage("Project name is required.")
      setMode("name")
      return
    }

    setBusy(true)
    setMessage("Inspecting Git repository...")
    try {
      const project = await createProject(draftName, repositoryPath)
      setProjectList((current) => [project, ...current])
      setSelectedIndex(0)
      setMode("list")
      setDraftName("")
      setPickerListing(null)
      setPickerIndex(0)
      setRepositorySummary(null)
      setPlanningContext(null)
      setPlanningRun(null)
      setMessage("Project created.")
      setLoadState("ready")
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to create project")
    } finally {
      setBusy(false)
    }
  }

  const removeSelectedProject = async () => {
    if (!selectedProject || busy) {
      return
    }
    setBusy(true)
    setMessage("Deleting project...")
    try {
      await deleteProject(selectedProject.id)
      setProjectList((current) => current.filter((project) => project.id !== selectedProject.id))
      setSelectedIndex((current) => Math.max(current - 1, 0))
      setPlanList([])
      setRepositorySummary(null)
      setPlanningContext(null)
      setPlanningRun(null)
      setMode("list")
      setMessage("Project deleted. Repository files were not changed.")
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to delete project")
    } finally {
      setBusy(false)
    }
  }

  const refreshSelectedProject = async () => {
    if (!selectedProject || busy) {
      return
    }
    setBusy(true)
    setMessage("Refreshing Git metadata...")
    try {
      const refreshed = await refreshProject(selectedProject.id)
      setProjectList((current) =>
        current.map((project) => (project.id === refreshed.id ? refreshed : project)),
      )
      setRepositorySummary(null)
      setPlanningContext(null)
      setPlanningRun(null)
      setMessage("Repository metadata refreshed. Scan the current HEAD before normalization.")
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to refresh project")
    } finally {
      setBusy(false)
    }
  }

  const scanSelectedRepository = async () => {
    if (!selectedRepository || busy) {
      return
    }
    setBusy(true)
    setMessage("Scanning repository metadata safely...")
    try {
      const summary = await generateRepositorySummary(selectedRepository.id)
      setRepositorySummary(summary)
      setPlanningContext(null)
      setPlanningRun(null)
      setMessage(`Repository summary ready for HEAD ${summary.head_sha.slice(0, 12)}.`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to scan repository")
    } finally {
      setBusy(false)
    }
  }

  const normalizeSelectedPlan = async () => {
    if (!latestPlan || busy) {
      return
    }
    setBusy(true)
    setMessage("Normalizing the latest plan version...")
    try {
      const context = await normalizePlan(latestPlan.id)
      setPlanningContext(context)
      setPlanningRun(null)
      setMessage(`Planning context created for plan version ${context.plan_version}.`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to normalize plan")
    } finally {
      setBusy(false)
    }
  }

  const runSelectedPlanningAgent = async () => {
    if (busy) {
      setMessage("A project operation is already running. Wait for it to finish.")
      return
    }
    if (!latestPlan) {
      setMessage("Create a plan first with P before running the planning agent.")
      return
    }
    if (!repositorySummary) {
      setMessage("Scan the current repository HEAD before running the planning agent. Press S.")
      return
    }
    if (!planningContext) {
      setMessage("Normalize the latest plan before running the planning agent. Press O.")
      return
    }
    setBusy(true)
    setMessage("Running the local planning agent...")
    try {
      const run = await startPlanningRun(latestPlan.id)
      applyPlanningRun(run)
      if (run.status === "NEEDS_INPUT") {
        setMessage(`Planning needs ${run.questions.length} answer(s). Press q to continue.`)
      } else if (run.status === "COMPLETED") {
        setMessage(`Planning completed with ${run.result?.steps.length ?? 0} step(s).`)
      } else {
        setMessage(`Planning run finished with status ${run.status}.`)
      }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to run planning agent")
    } finally {
      setBusy(false)
    }
  }

  useKeyboard((key) => {
    if (mode === "plan" || mode === "questions") {
      return
    }

    if (mode === "name") {
      if (key.name === "escape") {
        setMode("list")
        setMessage("Project creation cancelled.")
      }
      return
    }

    if (mode === "picker") {
      if (key.name === "escape") {
        setMode("name")
        setMessage("")
        return
      }
      if (pickerLoading || busy || !pickerListing) {
        return
      }
      if (key.name === "backspace" || key.name === "left" || key.name === "h") {
        if (pickerListing.parentPath) {
          void openDirectory(pickerListing.parentPath)
        }
        return
      }
      if (key.name === "down" || key.name === "j") {
        setPickerIndex((current) => Math.min(current + 1, pickerItems.length - 1))
        return
      }
      if (key.name === "up" || key.name === "k") {
        setPickerIndex((current) => Math.max(current - 1, 0))
        return
      }
      if (key.name === "s") {
        void submitProject(pickerListing.currentPath)
        return
      }
      if (key.name === "return" || key.name === "enter") {
        const item = pickerItems[pickerIndex]
        if (!item) {
          return
        }
        if (item.kind === "select") {
          void submitProject(item.path)
        } else {
          void openDirectory(item.path)
        }
      }
      return
    }

    if (mode === "delete") {
      if (key.name === "y") {
        void removeSelectedProject()
      } else if (key.name === "n" || key.name === "escape") {
        setMode("list")
        setMessage("Deletion cancelled.")
      }
      return
    }

    if (key.name === "n") {
      setMode("name")
      setDraftName("")
      setMessage("")
      return
    }
    if (key.name === "p" && selectedProject) {
      setMode("plan")
      setMessage("")
      return
    }
    if (key.name === "s" && selectedRepository) {
      void scanSelectedRepository()
      return
    }
    if (key.name === "o" && latestPlan) {
      void normalizeSelectedPlan()
      return
    }
    if (key.name === "a") {
      void runSelectedPlanningAgent()
      return
    }
    if (key.name === "q" && planningRun?.status === "NEEDS_INPUT") {
      setMode("questions")
      setMessage("")
      return
    }
    if (key.name === "d" && selectedProject) {
      setMode("delete")
      setMessage("")
      return
    }
    if (key.name === "g" && selectedProject) {
      void refreshSelectedProject()
      return
    }
    if (key.name === "r") {
      void reloadProjects()
      return
    }
    if ((key.name === "down" || key.name === "j") && projectList.length > 0) {
      setSelectedIndex((current) => Math.min(current + 1, projectList.length - 1))
      return
    }
    if ((key.name === "up" || key.name === "k") && projectList.length > 0) {
      setSelectedIndex((current) => Math.max(current - 1, 0))
    }
  })

  useEffect(() => {
    onInteractionChange(mode !== "list")
  }, [mode, onInteractionChange])

  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected") {
      setLoadState("idle")
      return
    }

    setLoadState("loading")
    void listProjects()
      .then((projects) => {
        if (!disposed) {
          setProjectList(projects)
          setSelectedIndex((current) => Math.min(current, Math.max(projects.length - 1, 0)))
          setLoadState("ready")
          setMessage("")
        }
      })
      .catch((error) => {
        if (!disposed) {
          setLoadState("error")
          setMessage(error instanceof Error ? error.message : "Failed to load projects")
        }
      })

    return () => {
      disposed = true
    }
  }, [connection.state])

  useEffect(() => {
    let disposed = false
    if (connection.state !== "connected" || !selectedProject) {
      setPlanList([])
      setPlanLoading(false)
      return
    }

    setPlanLoading(true)
    void listPlans(selectedProject.id)
      .then((plans) => {
        if (!disposed) {
          setPlanList(plans)
        }
      })
      .catch((error) => {
        if (!disposed) {
          setPlanList([])
          setMessage(error instanceof Error ? error.message : "Failed to load plans")
        }
      })
      .finally(() => {
        if (!disposed) {
          setPlanLoading(false)
        }
      })

    return () => {
      disposed = true
    }
  }, [connection.state, selectedProject])

  useEffect(() => {
    let disposed = false
    setRepositorySummary(null)
    setPlanningContext(null)
    setPlanningRun(null)
    if (connection.state !== "connected" || selectedRepositoryID === "") {
      return
    }

    void getCurrentRepositorySummary(selectedRepositoryID)
      .then((summary) => {
        if (!disposed) {
          setRepositorySummary(summary)
        }
      })
      .catch(() => {
        if (!disposed) {
          setRepositorySummary(null)
        }
      })

    return () => {
      disposed = true
    }
  }, [connection.state, selectedRepositoryID])

  useEffect(() => {
    let disposed = false
    setPlanningContext(null)
    setPlanningRun(null)
    if (connection.state !== "connected" || latestPlanID === "" || repositorySummaryID === "") {
      return
    }

    void getPlanningContext(latestPlanID)
      .then((context) => {
        if (!disposed) {
          setPlanningContext(context)
        }
      })
      .catch(() => {
        if (!disposed) {
          setPlanningContext(null)
        }
      })

    return () => {
      disposed = true
    }
  }, [connection.state, latestPlanID, repositorySummaryID])

  useEffect(() => {
    let disposed = false
    setPlanningRun(null)
    if (connection.state !== "connected" || latestPlanID === "" || planningContextID === "") {
      return
    }

    void getCurrentPlanningRun(latestPlanID)
      .then((run) => {
        if (!disposed) {
          setPlanningRun(run)
        }
      })
      .catch(() => {
        if (!disposed) {
          setPlanningRun(null)
        }
      })

    return () => {
      disposed = true
    }
  }, [connection.state, latestPlanID, planningContextID])

  if (mode === "plan" && selectedProject) {
    return (
      <PlanEditor
        projectID={selectedProject.id}
        projectName={selectedProject.name}
        onSaved={(plan) => {
          setPlanList((current) => [plan, ...current])
          setPlanningContext(null)
          setPlanningRun(null)
          setMode("list")
          setMessage("Plan draft created.")
        }}
        onCancel={() => {
          setMode("list")
          setMessage("Plan creation cancelled.")
        }}
      />
    )
  }

  if (mode === "questions" && planningRun) {
    return (
      <PlanningQuestionEditor
        run={planningRun}
        onSubmitted={(run) => {
          applyPlanningRun(run)
          setMode("list")
          if (run.status === "COMPLETED") {
            setMessage(`Planning completed with ${run.result?.steps.length ?? 0} step(s).`)
          } else {
            setMessage(`Planning returned status ${run.status}.`)
          }
        }}
        onCancel={() => {
          setMode("list")
          setMessage("Planning answers were not submitted.")
        }}
      />
    )
  }

  if (mode === "name") {
    return (
      <box flexDirection="column" flexGrow={1} gap={1}>
        <PageIntro
          kicker="NEW PROJECT"
          title="Register a local Git repository"
          description="Give this workspace a memorable name. The next step lets you choose the repository folder."
        />
        <Panel
          title="PROJECT NAME"
          borderColor={colors.accent}
          backgroundColor={colors.surfaceAccent}
        >
          <text fg={colors.dim}>A short name is easiest to scan in Jobs and Agents.</text>
          <text fg={colors.accent}>Name</text>
          <input
            value={draftName}
            placeholder="Example: Orkoda Website"
            focused
            onInput={setDraftName}
            onSubmit={startDirectoryPicker}
          />
        </Panel>
        <ShortcutBar
          shortcuts={[
            { key: "Enter", label: "choose folder" },
            { key: "Esc", label: "cancel" },
          ]}
        />
        {message ? <text fg={colors.danger}>{message}</text> : null}
      </box>
    )
  }

  if (mode === "picker") {
    const visibleItems = visibleDirectoryItems(pickerItems, pickerIndex)
    return (
      <box flexDirection="column" flexGrow={1} gap={1}>
        <PageIntro
          kicker="REPOSITORY FOLDER"
          title="Choose where Orkoda should work"
          description="Select a Git repository. The daemon will validate the folder before registering it."
          meta={pickerListing?.currentPath ?? "Loading..."}
        />
        {pickerListing ? (
          <StatusBadge
            label={pickerListing.isGitRepository ? "GIT REPOSITORY DETECTED" : "NO .GIT ENTRY"}
            tone={pickerListing.isGitRepository ? "success" : "warning"}
          />
        ) : null}

        {pickerLoading ? <text fg={colors.warning}>Reading directories...</text> : null}
        {!pickerLoading && pickerListing ? (
          <Panel title="FOLDERS" flexGrow={1} borderColor={colors.lineStrong}>
            {visibleItems.map(({ item, index }) => {
              let label = item.label
              if (item.kind === "select") {
                label = `◎ ${item.label}`
              } else if (item.kind === "directory") {
                label = `▸ ${item.label}`
              }
              return (
                <box
                  key={`${item.kind}:${item.path}`}
                  paddingLeft={1}
                  paddingRight={1}
                  backgroundColor={index === pickerIndex ? colors.surfaceAccent : colors.surface}
                  borderStyle="rounded"
                  borderColor={index === pickerIndex ? colors.accent : colors.line}
                >
                  <text fg={index === pickerIndex ? colors.accent : colors.muted}>
                    {index === pickerIndex ? `› ${label}` : `  ${label}`}
                  </text>
                </box>
              )
            })}
          </Panel>
        ) : null}

        <ShortcutBar
          shortcuts={[
            { key: "↑↓", label: "select" },
            { key: "Enter", label: "open / select" },
            { key: "←", label: "parent" },
            { key: "S", label: "use current" },
            { key: "Esc", label: "back" },
          ]}
        />
        {message ? <text fg={busy ? colors.warning : colors.danger}>{message}</text> : null}
      </box>
    )
  }

  if (mode === "delete") {
    return (
      <box flexDirection="column" flexGrow={1} gap={1}>
        <PageIntro
          kicker="REMOVE REGISTRATION"
          title="Delete this project from Orkoda?"
          description="Only the project registration is removed. Repository files and Git history stay untouched."
        />
        <Panel title="CONFIRMATION" borderColor={colors.danger} backgroundColor="#24151A">
          <text fg={colors.danger}>{selectedProject?.name ?? "Unknown project"}</text>
          <text fg={colors.muted}>This action cannot be undone from the TUI.</text>
        </Panel>
        <ShortcutBar
          shortcuts={[
            { key: "Y", label: "delete registration" },
            { key: "N / Esc", label: "cancel" },
          ]}
        />
        {message ? <text fg={colors.muted}>{message}</text> : null}
      </box>
    )
  }

  if (connection.state !== "connected") {
    return (
      <EmptyState
        title="Daemon is not connected"
        detail="Projects stay local to the daemon. Start it in another terminal and press R to reconnect."
        action="make api   →   R reconnect"
      />
    )
  }
  if (loadState === "loading") {
    return (
      <Panel title="PROJECTS" borderColor={colors.accent}>
        <text fg={colors.warning}>Loading project registry...</text>
        <text fg={colors.dim}>Reading repositories and their latest plans.</text>
      </Panel>
    )
  }
  if (loadState === "error") {
    return (
      <Panel title="PROJECTS" borderColor={colors.danger} backgroundColor="#24151A">
        <text fg={colors.danger}>Failed to load projects.</text>
        <text fg={colors.muted}>{message}</text>
        <ShortcutBar shortcuts={[{ key: "R", label: "try again" }]} />
      </Panel>
    )
  }
  if (projectList.length === 0) {
    return (
      <EmptyState
        title="No project registered yet"
        detail="Connect a local Git repository to start planning and running work."
        action="N new project"
      />
    )
  }

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageIntro
        kicker="PROJECTS"
        title={selectedProject?.name ?? "Project registry"}
        description="Choose a repository, inspect its current HEAD, then move through the plan pipeline."
        meta={`${selectedIndex + 1} / ${projectList.length}`}
      />
      <box flexDirection="row" flexGrow={1} gap={1}>
        <Panel
          width="30%"
          title={`PROJECTS  ${projectList.length}`}
          borderColor={colors.lineStrong}
        >
          {projectList.map((project, index) => (
            <box
              key={project.id}
              flexDirection="column"
              gap={1}
              padding={1}
              backgroundColor={index === selectedIndex ? colors.surfaceAccent : colors.surface}
              borderStyle="rounded"
              borderColor={index === selectedIndex ? colors.accent : colors.line}
            >
              <text fg={index === selectedIndex ? colors.accent : colors.text}>
                {`${index === selectedIndex ? "›" : " "} ${project.name}`}
              </text>
              <text fg={colors.dim}>{project.repositories[0]?.current_branch || "detached"}</text>
            </box>
          ))}
        </Panel>
        <box flexGrow={1} flexDirection="column" gap={1}>
          <box flexDirection="row" gap={1}>
            <Metric
              label="Repository"
              value={selectedRepository?.current_branch || "detached"}
              tone="accent"
            />
            <Metric
              label="Working tree"
              value={selectedRepository?.dirty ? "CHANGES" : "CLEAN"}
              tone={selectedRepository?.dirty ? "warning" : "success"}
            />
            <Metric label="HEAD" value={selectedRepository?.head_sha.slice(0, 12) ?? "unknown"} />
          </box>
          <text fg={colors.dim}>{selectedRepository?.local_path ?? "No repository"}</text>

          <SectionHeading
            title="Repository context"
            detail="latest HEAD"
            action="S scan   G refresh"
          />
          <Panel borderColor={colors.line}>
            {repositorySummary ? (
              <box flexDirection="row" gap={1}>
                <Metric
                  label="Languages"
                  value={repositorySummary.summary.languages.join(" + ") || "unknown"}
                  tone="success"
                />
                <Metric
                  label="Frameworks"
                  value={repositorySummary.summary.frameworks.join(" + ") || "none"}
                />
                <Metric label="Files" value={String(repositorySummary.summary.file_count)} />
              </box>
            ) : (
              <text fg={colors.dim}>No summary for the current HEAD. Press S to scan safely.</text>
            )}
          </Panel>

          <SectionHeading
            title="Plan pipeline"
            detail={`${planList.length} versioned plan(s)`}
            action="P new   O normalize   A run   Q answer"
          />
          <Panel borderColor={colors.line}>
            {planLoading ? <text fg={colors.warning}>Loading plans...</text> : null}
            {!planLoading && planList.length === 0 ? (
              <text fg={colors.dim}>No plan draft yet. Press P to create the first one.</text>
            ) : null}
            {!planLoading
              ? planList.slice(0, 3).map((plan) => (
                  <box key={plan.id} flexDirection="row" justifyContent="space-between" gap={1}>
                    <text fg={colors.text}>{`v${plan.current_version}  ${plan.title}`}</text>
                    <StatusBadge label={plan.status} tone={planStatusTone(plan.status)} />
                  </box>
                ))
              : null}
          </Panel>

          {planningContext ? (
            <Panel title="NORMALIZED CONTEXT" borderColor={colors.success}>
              <text fg={colors.success}>{planningContext.normalized_plan.goal}</text>
              <box flexDirection="row" gap={1}>
                <Metric
                  label="Scope"
                  value={String(planningContext.normalized_plan.scope.length)}
                />
                <Metric
                  label="Areas"
                  value={String(planningContext.normalized_plan.affected_areas.length)}
                />
                <Metric
                  label="Risks"
                  value={String(planningContext.normalized_plan.risks.length)}
                  tone={planningContext.normalized_plan.risks.length > 0 ? "warning" : "success"}
                />
                <Metric
                  label="Questions"
                  value={String(planningContext.normalized_plan.open_questions.length)}
                  tone={
                    planningContext.normalized_plan.open_questions.length > 0
                      ? "warning"
                      : "success"
                  }
                />
              </box>
            </Panel>
          ) : latestPlan ? (
            <text fg={colors.dim}>
              Latest plan is not normalized for the current repository HEAD.
            </text>
          ) : null}

          {planningRun ? (
            <Panel
              title="PLANNING AGENT"
              borderColor={planningRun.status === "COMPLETED" ? colors.success : colors.warning}
            >
              <box flexDirection="row" justifyContent="space-between">
                <text fg={planningRun.status === "COMPLETED" ? colors.success : colors.warning}>
                  {`${planningRun.provider}/${planningRun.model}`}
                </text>
                <StatusBadge
                  label={planningRun.status}
                  tone={planningRun.status === "COMPLETED" ? "success" : "warning"}
                />
              </box>
              {planningRun.status === "NEEDS_INPUT" ? (
                <text fg={colors.warning}>
                  {`${planningRun.questions.filter((question) => question.status === "OPEN").length} open question(s) • press Q`}
                </text>
              ) : null}
              {planningRun.result ? (
                <box flexDirection="column">
                  <text fg={colors.muted}>{planningRun.result.summary}</text>
                  {planningRun.result.steps.slice(0, 3).map((step) => (
                    <text key={step.id} fg={colors.dim}>{`• ${step.title}`}</text>
                  ))}
                </box>
              ) : null}
              {planningRun.error_message ? (
                <text fg={colors.danger}>{planningRun.error_message}</text>
              ) : null}
              <text fg={colors.dim}>
                {`tokens ${planningRun.usage.input_tokens + planningRun.usage.output_tokens}`}
              </text>
            </Panel>
          ) : planningContext ? (
            <text fg={colors.dim}>No planning agent run for this context. Press A to run it.</text>
          ) : null}

          {message ? <text fg={busy ? colors.warning : colors.success}>{message}</text> : null}
          <ShortcutBar
            shortcuts={[
              { key: "↑↓", label: "select" },
              { key: "N", label: "new" },
              { key: "P", label: "plan" },
              { key: "S", label: "scan" },
              { key: "O", label: "normalize" },
              { key: "A", label: "run agent" },
              { key: "Q", label: "answer" },
              { key: "D", label: "delete" },
              { key: "G", label: "refresh" },
              { key: "R", label: "reload" },
            ]}
          />
        </box>
      </box>
    </box>
  )
}

function planStatusTone(status: Plan["status"]): "neutral" | "accent" | "success" | "warning" {
  switch (status) {
    case "READY":
    case "APPROVED":
      return "success"
    case "PLANNING":
    case "NEEDS_INPUT":
      return "warning"
    case "DRAFT":
      return "accent"
    default:
      return "neutral"
  }
}
