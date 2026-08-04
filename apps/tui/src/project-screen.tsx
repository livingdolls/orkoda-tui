/** @jsxImportSource @opentui/react */

import type { ScrollBoxRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/react"
import { useEffect, useRef, useState } from "react"

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
  listRepositoryBranches,
  type Project,
  type RepositoryBranch,
  refreshProject,
  updateRepositoryTrust,
} from "./projects"
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
  Section,
  type StatusTone,
  truncate,
} from "./ui"
import { createWorkflowJob, performWorkflowAction } from "./workflow-jobs"

type ProjectMode =
  | "list"
  | "name"
  | "picker"
  | "delete"
  | "branches"
  | "ignore"
  | "plan"
  | "questions"
type ProjectLoadState = "idle" | "loading" | "ready" | "error"

export function ProjectScreen({
  connection,
  onInteractionChange,
  helpOpen = false,
}: {
  connection: DaemonConnection
  onInteractionChange: (active: boolean) => void
  helpOpen?: boolean
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
  const [branches, setBranches] = useState<RepositoryBranch[]>([])
  const [branchIndex, setBranchIndex] = useState(0)
  const [branchesLoading, setBranchesLoading] = useState(false)
  const [ignoreDraft, setIgnoreDraft] = useState("{}")
  const detailScrollRef = useRef<ScrollBoxRenderable>(null)
  const listScrollRef = useRef<ScrollBoxRenderable>(null)

  const selectedProject = projectList[selectedIndex] ?? null
  const selectedRepository = selectedProject?.repositories[0] ?? null
  const latestPlan = planList[0] ?? null
  const pickerItems = pickerListing ? buildDirectoryPickerItems(pickerListing) : []
  const selectedRepositoryID = selectedRepository?.id ?? ""
  const latestPlanID = latestPlan?.id ?? ""
  const repositorySummaryID = repositorySummary?.id ?? ""
  const planningContextID = planningContext?.id ?? ""
  const selectedBranch = branches[branchIndex] ?? branches.find((branch) => branch.current)

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

  const openBranchSelector = async () => {
    if (!selectedRepository || busy || branchesLoading) return
    setMode("branches")
    setBranchesLoading(true)
    setMessage("")
    try {
      const items = await listRepositoryBranches(selectedRepository.id)
      setBranches(items)
      const current = items.findIndex((branch) => branch.current)
      setBranchIndex(current >= 0 ? current : 0)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to load branches")
      setMode("list")
    } finally {
      setBranchesLoading(false)
    }
  }

  const trustSelectedRepository = async () => {
    if (!selectedRepository || busy) return
    const nextLevel = selectedRepository.trust_level === "TRUSTED" ? "UNTRUSTED" : "TRUSTED"
    setBusy(true)
    setMessage(`${nextLevel === "TRUSTED" ? "Trusting" : "Removing trust from"} repository...`)
    try {
      const repository = await updateRepositoryTrust(
        selectedRepository.id,
        nextLevel,
        selectedRepository.ignore_policy ?? {},
      )
      setProjectList((current) =>
        current.map((project) => ({
          ...project,
          repositories: project.repositories.map((item) =>
            item.id === repository.id ? repository : item,
          ),
        })),
      )
      setMessage(`Repository trust level is now ${repository.trust_level}.`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to update repository trust")
    } finally {
      setBusy(false)
    }
  }

  const openIgnorePolicyEditor = () => {
    if (!selectedRepository || busy) return
    setIgnoreDraft(JSON.stringify(selectedRepository.ignore_policy ?? {}))
    setMode("ignore")
    setMessage("")
  }

  const submitIgnorePolicy = async () => {
    if (!selectedRepository || busy) return
    let parsed: unknown
    try {
      parsed = JSON.parse(ignoreDraft)
    } catch {
      setMessage("Ignore policy must be valid JSON.")
      return
    }
    if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
      setMessage("Ignore policy must be a JSON object.")
      return
    }
    setBusy(true)
    setMessage("Saving repository ignore policy...")
    try {
      const currentLevel =
        selectedRepository.trust_level === "TRUSTED"
          ? "TRUSTED"
          : selectedRepository.trust_level === "BLOCKED"
            ? "BLOCKED"
            : "UNTRUSTED"
      const repository = await updateRepositoryTrust(
        selectedRepository.id,
        currentLevel,
        parsed as Record<string, unknown>,
      )
      setProjectList((current) =>
        current.map((project) => ({
          ...project,
          repositories: project.repositories.map((item) =>
            item.id === repository.id ? repository : item,
          ),
        })),
      )
      setMode("list")
      setMessage("Repository ignore policy saved.")
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to save ignore policy")
    } finally {
      setBusy(false)
    }
  }

  const createSelectedWorkflow = async () => {
    if (!selectedProject || !selectedRepository || !latestPlan || busy) return
    if (latestPlan.status !== "READY" && latestPlan.status !== "APPROVED") {
      setMessage("The latest plan must be READY before creating a workflow.")
      return
    }
    const baseBranch = selectedBranch?.name || selectedRepository.current_branch
    if (!baseBranch || baseBranch === "HEAD") {
      setMessage("Select a concrete base branch before creating a workflow.")
      return
    }
    setBusy(true)
    setMessage(`Creating workflow from ${baseBranch}...`)
    try {
      const created = await createWorkflowJob(selectedProject.id, {
        plan_id: latestPlan.id,
        repository_id: selectedRepository.id,
        base_branch: baseBranch,
      })
      try {
        const started = await performWorkflowAction(created.id, "start", created.version, {
          requested_by: "tui",
          base_branch: baseBranch,
        })
        setMessage(
          `Workflow ${started.id.slice(0, 8)} started from ${baseBranch}. Open Jobs to follow it.`,
        )
      } catch (error) {
        setMessage(
          `Workflow ${created.id.slice(0, 8)} created READY; start failed: ${error instanceof Error ? error.message : "unknown error"}`,
        )
      }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to create workflow")
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
        setMessage(`Planning needs ${run.questions.length} open question(s). Press q to continue.`)
      } else if (run.status === "COMPLETED") {
        setMessage(`Planning COMPLETED with ${run.result?.steps.length ?? 0} step(s).`)
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
    if (helpOpen) {
      return
    }
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

    if (mode === "branches") {
      if (key.name === "escape") {
        setMode("list")
        setMessage("")
        return
      }
      if (branchesLoading) return
      if (key.name === "down" || key.name === "j") {
        setBranchIndex((current) => Math.min(current + 1, Math.max(branches.length - 1, 0)))
        return
      }
      if (key.name === "up" || key.name === "k") {
        setBranchIndex((current) => Math.max(current - 1, 0))
        return
      }
      if (key.name === "return" || key.name === "enter") {
        const branch = branches[branchIndex]
        if (branch) setMessage(`Base branch selected: ${branch.name}`)
        setMode("list")
      }
      return
    }

    if (mode === "ignore") {
      if (key.name === "escape") {
        setMode("list")
        setMessage("Ignore policy editing cancelled.")
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
    if (key.name === "b" && selectedRepository) {
      void openBranchSelector()
      return
    }
    if (key.name === "t" && selectedRepository) {
      void trustSelectedRepository()
      return
    }
    if (key.name === "i" && selectedRepository) {
      openIgnorePolicyEditor()
      return
    }
    if (key.name === "w") {
      void createSelectedWorkflow()
      return
    }
    if (key.name === "r") {
      void reloadProjects()
      return
    }
    if (key.name === "pageup") {
      detailScrollRef.current?.scrollBy(-1, "viewport")
      listScrollRef.current?.scrollBy(-1, "viewport")
      return
    }
    if (key.name === "pagedown") {
      detailScrollRef.current?.scrollBy(1, "viewport")
      listScrollRef.current?.scrollBy(1, "viewport")
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
    setBranches([])
    setBranchIndex(0)
    if (selectedRepositoryID === "") {
      setIgnoreDraft("{}")
    }
  }, [selectedRepositoryID])

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
            setMessage(`Planning COMPLETED with ${run.result?.steps.length ?? 0} step(s).`)
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
        <PageHeader
          title="New project"
          description="Connect a Git repository you already have on this computer. Give it a short name, then pick its folder."
          meta="step 1 of 2"
        />
        <Section title="Project name">
          <Card tone="accent">
            <text fg={colors.muted}>Short names are easiest to recognize later.</text>
            <input
              value={draftName}
              placeholder="Example: Orkoda Website"
              focused
              onInput={setDraftName}
              onSubmit={startDirectoryPicker}
            />
          </Card>
        </Section>
        <KeyHints
          shortcuts={[
            { key: "Enter", label: "next: choose folder" },
            { key: "Esc", label: "cancel" },
          ]}
        />
        {message ? (
          <Banner tone="danger">
            <text fg={colors.danger}>{message}</text>
          </Banner>
        ) : null}
      </box>
    )
  }

  if (mode === "picker") {
    const visibleItems = visibleDirectoryItems(pickerItems, pickerIndex)
    return (
      <box flexDirection="column" flexGrow={1} gap={1}>
        <PageHeader
          title="Choose the repository folder"
          description="Move into the folder that contains your project's code. Orkoda checks that it is a Git repository before adding it."
          meta={pickerListing?.currentPath ?? "Loading..."}
        />
        {pickerListing ? (
          <Chip
            label={
              pickerListing.isGitRepository ? "Git repository detected" : "No .git entry here yet"
            }
            tone={pickerListing.isGitRepository ? "success" : "warning"}
          />
        ) : null}

        {pickerLoading ? <text fg={colors.warning}>Reading folders...</text> : null}
        {!pickerLoading && pickerListing ? (
          <box
            flexDirection="column"
            flexGrow={1}
            gap={0}
            padding={1}
            backgroundColor={colors.raised}
            borderStyle="rounded"
            borderColor={colors.line}
          >
            {visibleItems.map(({ item, index }) => {
              const selected = index === pickerIndex
              const icon = item.kind === "select" ? "◎" : "▸"
              return (
                <box
                  key={`${item.kind}:${item.path}`}
                  flexDirection="row"
                  gap={1}
                  paddingLeft={1}
                  paddingRight={1}
                  backgroundColor={selected ? colors.accentTint : colors.raised}
                >
                  <text fg={selected ? colors.accent : colors.faint}>{icon}</text>
                  <text fg={selected ? colors.text : colors.muted} attributes={selected ? BOLD : 0}>
                    {item.label}
                  </text>
                  {item.kind === "select" ? <text fg={colors.faint}>use this folder</text> : null}
                </box>
              )
            })}
          </box>
        ) : null}

        <KeyHints
          shortcuts={[
            { key: "↑↓", label: "move" },
            { key: "Enter", label: "open / select" },
            { key: "←", label: "up one level" },
            { key: "S", label: "use this folder" },
            { key: "Esc", label: "back" },
          ]}
        />
        {message ? (
          <Banner tone={busy ? "warning" : "danger"}>
            <text fg={busy ? colors.warning : colors.danger}>{message}</text>
          </Banner>
        ) : null}
      </box>
    )
  }

  if (mode === "delete") {
    return (
      <box flexDirection="column" flexGrow={1} gap={1}>
        <PageHeader
          title="Remove this project from Orkoda?"
          description="This only removes the registration inside Orkoda. Your files and Git history stay exactly as they are."
        />
        <Banner tone="danger">
          <text fg={colors.danger} attributes={BOLD}>
            {selectedProject?.name ?? "Unknown project"}
          </text>
          <text fg={colors.muted}>This cannot be undone from the TUI.</text>
        </Banner>
        <KeyHints
          shortcuts={[
            { key: "Y", label: "yes, remove it" },
            { key: "N", label: "cancel" },
            { key: "Esc", label: "cancel" },
          ]}
        />
        {message ? <text fg={colors.muted}>{message}</text> : null}
      </box>
    )
  }

  if (mode === "branches") {
    return (
      <box flexDirection="column" flexGrow={1} gap={1}>
        <PageHeader
          title="Choose the base branch"
          description="The next workflow starts from this branch. Its current commit becomes the fixed starting point."
          meta={branchesLoading ? "loading" : `${branches.length} branch(es)`}
        />
        {branchesLoading ? <text fg={colors.warning}>Reading local branches...</text> : null}
        {!branchesLoading && branches.length === 0 ? (
          <EmptyState
            title="No local branch found"
            detail="Create a local branch in the repository first, then come back."
            shortcut={{ key: "Esc", label: "go back" }}
          />
        ) : null}
        {!branchesLoading && branches.length > 0 ? (
          <box
            flexDirection="column"
            flexGrow={1}
            padding={1}
            backgroundColor={colors.raised}
            borderStyle="rounded"
            borderColor={colors.line}
          >
            {branches.map((branch, index) => {
              const selected = index === branchIndex
              return (
                <box
                  key={branch.name}
                  flexDirection="row"
                  justifyContent="space-between"
                  paddingLeft={1}
                  paddingRight={1}
                  backgroundColor={selected ? colors.accentTint : colors.raised}
                >
                  <box flexDirection="row" gap={1}>
                    <text fg={selected ? colors.accent : colors.faint}>{selected ? "▸" : " "}</text>
                    <text
                      fg={selected ? colors.text : colors.muted}
                      attributes={selected ? BOLD : 0}
                    >
                      {branch.name}
                    </text>
                  </box>
                  <text fg={branch.current ? colors.success : colors.faint}>
                    {branch.current ? "current branch" : branch.head_sha.slice(0, 8)}
                  </text>
                </box>
              )
            })}
          </box>
        ) : null}
        {selectedBranch ? (
          <text fg={colors.muted}>{`Selected: ${selectedBranch.name}`}</text>
        ) : null}
        <KeyHints
          shortcuts={[
            { key: "↑↓", label: "select" },
            { key: "Enter", label: "use branch" },
            { key: "Esc", label: "cancel" },
          ]}
        />
      </box>
    )
  }

  if (mode === "ignore") {
    return (
      <box flexDirection="column" flexGrow={1} gap={1}>
        <PageHeader
          title="Edit the ignore policy"
          description="A small JSON object that tells scans and workflows which folders to skip."
          meta="JSON object"
        />
        <Section title="Ignore policy">
          <Card tone="accent">
            <input
              value={ignoreDraft}
              focused
              placeholder='{"directories":["node_modules"]}'
              onInput={setIgnoreDraft}
              onSubmit={() => void submitIgnorePolicy()}
            />
            <text fg={colors.faint}>Example: {`{"directories":["node_modules","vendor"]}`}</text>
          </Card>
        </Section>
        <KeyHints
          shortcuts={[
            { key: "Enter", label: "save policy" },
            { key: "Esc", label: "cancel" },
          ]}
        />
        {message ? (
          <Banner tone={busy ? "warning" : "danger"}>
            <text fg={busy ? colors.warning : colors.danger}>{message}</text>
          </Banner>
        ) : null}
      </box>
    )
  }

  if (connection.state !== "connected") {
    return (
      <EmptyState
        icon="◇"
        title="The daemon is not running"
        detail="Projects live inside the local daemon. Start it in another terminal with `make api`, then reconnect."
        shortcut={{ key: "R", label: "reconnect" }}
      />
    )
  }
  if (loadState === "loading") {
    return (
      <Card>
        <text fg={colors.warning}>Loading projects...</text>
        <text fg={colors.faint}>Reading repositories and their latest plans.</text>
      </Card>
    )
  }
  if (loadState === "error") {
    return (
      <box flexDirection="column" gap={1} flexGrow={1}>
        <Banner tone="danger">
          <text fg={colors.danger} attributes={BOLD}>
            Failed to load projects
          </text>
          <text fg={colors.muted}>{message}</text>
        </Banner>
        <KeyHints shortcuts={[{ key: "R", label: "try again" }]} />
      </box>
    )
  }
  if (projectList.length === 0) {
    return (
      <EmptyState
        icon="◇"
        title="No project yet"
        detail="Add a Git repository from this computer to start planning work with the agents."
        shortcut={{ key: "N", label: "add your first project" }}
      />
    )
  }

  const nextStep = computeNextStep({
    repositorySummary,
    latestPlan,
    planningContext,
    planningRun,
  })

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title={selectedProject?.name ?? "Projects"}
        description="Follow the setup progress below. Each step shows which key to press next."
        meta={`${selectedIndex + 1} of ${projectList.length}`}
      />
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
              {projectList.map((project, index) => {
                const selected = index === selectedIndex
                const repository = project.repositories[0]
                return (
                  <box
                    key={project.id}
                    flexDirection="column"
                    paddingLeft={1}
                    paddingRight={1}
                    backgroundColor={selected ? colors.accentTint : colors.raised}
                  >
                    <text
                      fg={selected ? colors.text : colors.muted}
                      attributes={selected ? BOLD : 0}
                    >
                      {`${selected ? "▸" : " "} ${truncate(project.name, 19)}`}
                    </text>
                    <text fg={colors.faint}>
                      {`  ${truncate(`${repository?.current_branch || "detached"}${repository?.dirty ? " · changes" : ""}`, 19)}`}
                    </text>
                  </box>
                )
              })}
            </box>
          </scrollbox>
        </box>
        <scrollbox ref={detailScrollRef} flexGrow={1} scrollY={true}>
          <box flexDirection="column" gap={1}>
            <box flexDirection="row" gap={1} alignItems="center" flexWrap="wrap">
              <Chip
                label={selectedRepository?.current_branch || "detached"}
                tone="accent"
                dot={false}
              />
              <Chip
                label={selectedRepository?.dirty ? "has changes" : "clean"}
                tone={selectedRepository?.dirty ? "warning" : "success"}
              />
              <Chip
                label={
                  selectedRepository?.trust_level === "TRUSTED"
                    ? "trusted"
                    : selectedRepository?.trust_level === "BLOCKED"
                      ? "blocked"
                      : "untrusted"
                }
                tone={
                  selectedRepository?.trust_level === "TRUSTED"
                    ? "success"
                    : selectedRepository?.trust_level === "BLOCKED"
                      ? "danger"
                      : "warning"
                }
              />
              {selectedBranch ? (
                <Chip label={`base ${selectedBranch.name}`} tone="neutral" dot={false} />
              ) : null}
            </box>
            <text fg={colors.faint}>
              {`${selectedRepository?.local_path ?? "No repository"} · ${selectedRepository ? `commit ${selectedRepository.head_sha.slice(0, 12)}` : ""}`}
            </text>

            <Section title="Setup progress">
              <Card>
                <StepRow state="done" label="Project registered" detail={selectedProject?.name} />
                <StepRow
                  state={repositorySummary ? "done" : nextStep === "scan" ? "next" : "todo"}
                  label={repositorySummary ? "Repository scanned" : "Scan the repository"}
                  detail={
                    repositorySummary
                      ? `${repositorySummary.summary.languages.join(" + ") || "unknown"} · ${repositorySummary.summary.file_count} files`
                      : "Lets the AI understand your codebase (safe, read-only)"
                  }
                  keyHint="S"
                />
                <StepRow
                  state={latestPlan ? "done" : nextStep === "plan" ? "next" : "todo"}
                  label={
                    latestPlan ? `Plan: ${truncate(latestPlan.title, 40)}` : "Describe the work"
                  }
                  detail={
                    latestPlan
                      ? `version ${latestPlan.current_version} · ${planStatusLabel(latestPlan.status)}`
                      : "Write what you want the agents to build"
                  }
                  keyHint="P"
                />
                <StepRow
                  state={planningContext ? "done" : nextStep === "normalize" ? "next" : "todo"}
                  label={planningContext ? "Plan locked to this code snapshot" : "Lock the plan"}
                  detail={
                    planningContext
                      ? `context for plan version ${planningContext.plan_version}`
                      : "Ties the plan to the scanned repository state"
                  }
                  keyHint="O"
                />
                <StepRow
                  state={
                    planningRun?.status === "COMPLETED"
                      ? "done"
                      : planningRun?.status === "NEEDS_INPUT"
                        ? "next"
                        : nextStep === "run"
                          ? "next"
                          : planningRun
                            ? "done"
                            : "todo"
                  }
                  label={
                    planningRun?.status === "COMPLETED"
                      ? `Plan prepared · ${planningRun.result?.steps.length ?? 0} steps`
                      : planningRun?.status === "NEEDS_INPUT"
                        ? "The AI has questions for you"
                        : "Let the AI prepare the plan"
                  }
                  detail={
                    planningRun?.status === "NEEDS_INPUT"
                      ? `${planningRun.questions.filter((question) => question.status === "OPEN").length} open question(s)`
                      : planningRun?.status === "COMPLETED"
                        ? "Review the steps below"
                        : "Turns your plan into concrete implementation steps"
                  }
                  keyHint={planningRun?.status === "NEEDS_INPUT" ? "Q" : "A"}
                />
                <StepRow
                  state={nextStep === "workflow" ? "next" : "todo"}
                  label="Start the work"
                  detail="Agents implement the plan in an isolated workspace"
                  keyHint="W"
                />
              </Card>
            </Section>

            {planList.length > 0 ? (
              <Section title="Plans" action="P new plan">
                <Card>
                  {planLoading ? <text fg={colors.warning}>Loading plans...</text> : null}
                  {!planLoading
                    ? planList.slice(0, 3).map((plan) => (
                        <box
                          key={plan.id}
                          flexDirection="row"
                          justifyContent="space-between"
                          gap={1}
                        >
                          <text fg={colors.text}>
                            {`v${plan.current_version} · ${truncate(plan.title, 48)}`}
                          </text>
                          <Chip
                            label={planStatusLabel(plan.status)}
                            tone={planStatusTone(plan.status)}
                          />
                        </box>
                      ))
                    : null}
                </Card>
              </Section>
            ) : null}

            {planningContext ? (
              <Section title="NORMALIZED CONTEXT">
                <Card tone="success">
                  <text fg={colors.text}>{planningContext.normalized_plan.goal}</text>
                  <text fg={colors.faint}>
                    {`scope ${planningContext.normalized_plan.scope.length} · areas ${planningContext.normalized_plan.affected_areas.length} · risks ${planningContext.normalized_plan.risks.length} · questions ${planningContext.normalized_plan.open_questions.length}`}
                  </text>
                </Card>
              </Section>
            ) : null}

            {planningRun ? (
              <Section title="PLANNING AGENT">
                <Card
                  tone={
                    planningRun.status === "COMPLETED"
                      ? "success"
                      : planningRun.status === "FAILED"
                        ? "danger"
                        : "warning"
                  }
                >
                  <box flexDirection="row" gap={1} alignItems="center">
                    <Chip
                      label={planningRun.status}
                      tone={
                        planningRun.status === "COMPLETED"
                          ? "success"
                          : planningRun.status === "FAILED"
                            ? "danger"
                            : "warning"
                      }
                    />
                    <text fg={colors.faint}>{`${planningRun.provider}/${planningRun.model}`}</text>
                  </box>
                  {planningRun.status === "NEEDS_INPUT" ? (
                    <box flexDirection="row" gap={1} alignItems="center">
                      <text fg={colors.warning}>
                        {`${planningRun.questions.filter((question) => question.status === "OPEN").length} open question(s)`}
                      </text>
                      <text fg={colors.faint}>Press Q to answer</text>
                    </box>
                  ) : null}
                  {planningRun.result ? (
                    <box flexDirection="column">
                      <text fg={colors.muted} wrapMode="word">
                        {planningRun.result.summary}
                      </text>
                      {planningRun.result.steps.slice(0, 3).map((step) => (
                        <text key={step.id} fg={colors.faint}>{`· ${step.title}`}</text>
                      ))}
                    </box>
                  ) : null}
                  {planningRun.error_message ? (
                    <text fg={colors.danger}>{planningRun.error_message}</text>
                  ) : null}
                  <text fg={colors.faint}>
                    {`tokens ${planningRun.usage.input_tokens + planningRun.usage.output_tokens}`}
                  </text>
                </Card>
              </Section>
            ) : null}
          </box>
        </scrollbox>
      </box>
      {message ? (
        <Banner tone={messageTone(message, busy)}>
          {planningRun ? (
            <text fg={toneFg(planningRunTone(planningRun.status))}>
              {`PLANNING AGENT · ${planningRun.status}`}
            </text>
          ) : planningContext ? (
            <text fg={colors.faint}>NORMALIZED CONTEXT</text>
          ) : null}
          <text fg={toneFg(messageTone(message, busy))}>{message}</text>
        </Banner>
      ) : null}
    </box>
  )
}

function StepRow({
  state,
  label,
  detail,
  keyHint,
}: {
  state: "done" | "next" | "todo"
  label: string
  detail?: string
  keyHint?: string
}) {
  const marker = state === "done" ? "✓" : state === "next" ? "▸" : "○"
  const markerColor =
    state === "done" ? colors.success : state === "next" ? colors.accent : colors.faint
  return (
    <box flexDirection="row" justifyContent="space-between" gap={1}>
      <box flexDirection="row" gap={1} flexGrow={1}>
        <text fg={markerColor}>{marker}</text>
        <box flexDirection="column" flexGrow={1}>
          <text
            fg={state === "todo" ? colors.faint : colors.text}
            attributes={state === "next" ? BOLD : 0}
          >
            {label}
          </text>
          {detail ? (
            <text fg={state === "todo" ? colors.faint : colors.muted}>{detail}</text>
          ) : null}
        </box>
      </box>
      {state !== "done" && keyHint ? <Key>{keyHint}</Key> : null}
    </box>
  )
}

function computeNextStep({
  repositorySummary,
  latestPlan,
  planningContext,
  planningRun,
}: {
  repositorySummary: RepositorySummary | null
  latestPlan: Plan | null
  planningContext: PlanningContext | null
  planningRun: PlanningRun | null
}): "scan" | "plan" | "normalize" | "run" | "workflow" | null {
  if (!repositorySummary) return "scan"
  if (!latestPlan) return "plan"
  if (!planningContext) return "normalize"
  if (!planningRun) return "run"
  if (planningRun.status === "NEEDS_INPUT") return "run"
  if (planningRun.status === "COMPLETED") return "workflow"
  return null
}

function messageTone(message: string, busy: boolean): StatusTone {
  if (busy) return "warning"
  const normalized = message.toLowerCase()
  if (
    normalized.startsWith("failed") ||
    normalized.includes("error") ||
    normalized.includes("required") ||
    normalized.includes("must be") ||
    normalized.includes("not found") ||
    normalized.includes("blocked") ||
    normalized.includes("cancelled")
  ) {
    return "danger"
  }
  return "success"
}

function toneFg(tone: StatusTone): string {
  switch (tone) {
    case "warning":
      return colors.warning
    case "danger":
      return colors.danger
    default:
      return colors.success
  }
}

function planStatusLabel(status: Plan["status"]): string {
  switch (status) {
    case "DRAFT":
      return "draft"
    case "READY":
      return "ready"
    case "PLANNING":
      return "planning"
    case "NEEDS_INPUT":
      return "needs input"
    case "APPROVED":
      return "approved"
    case "ARCHIVED":
      return "archived"
    default:
      return status
  }
}

function planStatusTone(status: Plan["status"]): StatusTone {
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

function planningRunTone(status: PlanningRun["status"]): StatusTone {
  if (status === "COMPLETED") return "success"
  if (status === "FAILED" || status === "CANCELLED") return "danger"
  return "warning"
}
