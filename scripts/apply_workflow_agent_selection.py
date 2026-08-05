from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    if old not in text:
        raise SystemExit(f"expected text not found in {path}: {old[:120]!r}")
    file.write_text(text.replace(old, new, 1))


def append_once(path: str, marker: str, content: str) -> None:
    file = Path(path)
    text = file.read_text()
    if marker in text:
        return
    file.write_text(text.rstrip() + "\n\n" + content.strip() + "\n")


# SQLite schema v5 and fresh-database foundation.
replace_once(
    "internal/database/migrate.go",
    "const latestSchemaVersion = 4",
    "const latestSchemaVersion = 5",
)
replace_once(
    "internal/database/workflow_migration.go",
    "\t\t\tbase_commit_sha TEXT NOT NULL,\n\t\t\tstatus TEXT NOT NULL CHECK (status IN (",
    "\t\t\tbase_commit_sha TEXT NOT NULL,\n"
    "\t\t\tagent_settings_version INTEGER NOT NULL DEFAULT 0 CHECK (agent_settings_version >= 0),\n"
    "\t\t\texecutor_provider TEXT NOT NULL DEFAULT '',\n"
    "\t\t\texecutor_model TEXT NOT NULL DEFAULT '',\n"
    "\t\t\treviewer_provider TEXT NOT NULL DEFAULT '',\n"
    "\t\t\treviewer_model TEXT NOT NULL DEFAULT '',\n"
    "\t\t\tstatus TEXT NOT NULL CHECK (status IN (",
)
replace_once(
    "internal/database/migrate.go",
    "\tif err := tx.Commit(); err != nil {",
    "\tif version < 5 {\n"
    "\t\tfor _, column := range []struct {\n"
    "\t\t\tname       string\n"
    "\t\t\tdefinition string\n"
    "\t\t}{\n"
    "\t\t\t{name: \"agent_settings_version\", definition: \"INTEGER NOT NULL DEFAULT 0 CHECK (agent_settings_version >= 0)\"},\n"
    "\t\t\t{name: \"executor_provider\", definition: \"TEXT NOT NULL DEFAULT ''\"},\n"
    "\t\t\t{name: \"executor_model\", definition: \"TEXT NOT NULL DEFAULT ''\"},\n"
    "\t\t\t{name: \"reviewer_provider\", definition: \"TEXT NOT NULL DEFAULT ''\"},\n"
    "\t\t\t{name: \"reviewer_model\", definition: \"TEXT NOT NULL DEFAULT ''\"},\n"
    "\t\t} {\n"
    "\t\t\tif err := ensureColumn(ctx, tx, \"workflow_jobs\", column.name, column.definition); err != nil {\n"
    "\t\t\t\treturn err\n"
    "\t\t\t}\n"
    "\t\t}\n"
    "\t\tif _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES (5, 'workflow-agent-selection', strftime('%s','now') * 1000)`); err != nil {\n"
    "\t\t\treturn fmt.Errorf(\"record workflow agent selection migration: %w\", err)\n"
    "\t\t}\n"
    "\t}\n\n"
    "\tif err := tx.Commit(); err != nil {",
)

# Workflow aggregate owns immutable executor/reviewer assignment.
replace_once(
    "internal/workflowjob/repository.go",
    "type Job struct {",
    "type AgentSelection struct {\n"
    "\tProvider string `json:\"provider\"`\n"
    "\tModel    string `json:\"model\"`\n"
    "}\n\n"
    "type Job struct {",
)
replace_once(
    "internal/workflowjob/repository.go",
    "\tBaseCommitSHA         string     `json:\"base_commit_sha\"`\n\tStatus",
    "\tBaseCommitSHA         string         `json:\"base_commit_sha\"`\n"
    "\tAgentSettingsVersion  int            `json:\"agent_settings_version\"`\n"
    "\tExecutor              AgentSelection `json:\"executor\"`\n"
    "\tReviewer              AgentSelection `json:\"reviewer\"`\n"
    "\tStatus",
)
replace_once(
    "internal/workflowjob/repository.go",
    "\tBaseBranch   string `json:\"base_branch\"`\n\tLimits",
    "\tBaseBranch           string         `json:\"base_branch\"`\n"
    "\tAgentSettingsVersion int            `json:\"agent_settings_version\"`\n"
    "\tExecutor             AgentSelection `json:\"executor\"`\n"
    "\tReviewer             AgentSelection `json:\"reviewer\"`\n"
    "\tLimits",
)
replace_once(
    "internal/workflowjob/repository.go",
    "\tinput.BaseBranch = strings.TrimSpace(input.BaseBranch)\n\tinput.Limits = normalizeLimits(input.Limits)",
    "\tinput.BaseBranch = strings.TrimSpace(input.BaseBranch)\n"
    "\tinput.Executor = normalizeAgentSelection(input.Executor)\n"
    "\tinput.Reviewer = normalizeAgentSelection(input.Reviewer)\n"
    "\tinput.Limits = normalizeLimits(input.Limits)\n"
    "\tif err := validateAgentSelections(input.AgentSettingsVersion, input.Executor, input.Reviewer); err != nil {\n"
    "\t\treturn Job{}, err\n"
    "\t}",
)
replace_once(
    "internal/workflowjob/repository.go",
    "\t\tBaseCommitSHA: headSHA,\n\t\tStatus:",
    "\t\tBaseCommitSHA:        headSHA,\n"
    "\t\tAgentSettingsVersion: input.AgentSettingsVersion,\n"
    "\t\tExecutor:             input.Executor,\n"
    "\t\tReviewer:             input.Reviewer,\n"
    "\t\tStatus:",
)
replace_once(
    "internal/workflowjob/repository.go",
    "\t\t\tid, project_id, plan_id, plan_version_id, repository_id,\n\t\t\tbase_branch, base_commit_sha, status, version,",
    "\t\t\tid, project_id, plan_id, plan_version_id, repository_id,\n"
    "\t\t\tbase_branch, base_commit_sha, agent_settings_version,\n"
    "\t\t\texecutor_provider, executor_model, reviewer_provider, reviewer_model,\n"
    "\t\t\tstatus, version,",
)
replace_once(
    "internal/workflowjob/repository.go",
    "\t\t) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?)\n\t`, job.ID, job.ProjectID, job.PlanID, job.PlanVersionID, job.RepositoryID,\n\t\tjob.BaseBranch, job.BaseCommitSHA, job.Status, job.Limits.MaxRevisions,",
    "\t\t) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?)\n"
    "\t`, job.ID, job.ProjectID, job.PlanID, job.PlanVersionID, job.RepositoryID,\n"
    "\t\tjob.BaseBranch, job.BaseCommitSHA, job.AgentSettingsVersion,\n"
    "\t\tjob.Executor.Provider, job.Executor.Model, job.Reviewer.Provider, job.Reviewer.Model,\n"
    "\t\tjob.Status, job.Limits.MaxRevisions,",
)
replace_once(
    "internal/workflowjob/repository.go",
    "\t\t\"plan_version_id\": job.PlanVersionID, \"repository_id\": job.RepositoryID,\n\t\t\"status\": job.Status, \"version\": job.Version,",
    "\t\t\"plan_version_id\": job.PlanVersionID, \"repository_id\": job.RepositoryID,\n"
    "\t\t\"agent_settings_version\": job.AgentSettingsVersion,\n"
    "\t\t\"executor\": job.Executor, \"reviewer\": job.Reviewer,\n"
    "\t\t\"status\": job.Status, \"version\": job.Version,",
)
replace_once(
    "internal/workflowjob/repository.go",
    "\tbase_branch, base_commit_sha, status, version,\n\tCOALESCE(current_dispatch_id, ''),",
    "\tbase_branch, base_commit_sha, agent_settings_version,\n"
    "\tCOALESCE(executor_provider, ''), COALESCE(executor_model, ''),\n"
    "\tCOALESCE(reviewer_provider, ''), COALESCE(reviewer_model, ''),\n"
    "\tstatus, version,\n\tCOALESCE(current_dispatch_id, ''),",
)
replace_once(
    "internal/workflowjob/repository.go",
    "\t\t&job.BaseBranch, &job.BaseCommitSHA, &job.Status, &job.Version,",
    "\t\t&job.BaseBranch, &job.BaseCommitSHA, &job.AgentSettingsVersion,\n"
    "\t\t&job.Executor.Provider, &job.Executor.Model,\n"
    "\t\t&job.Reviewer.Provider, &job.Reviewer.Model, &job.Status, &job.Version,",
)
replace_once(
    "internal/workflowjob/repository.go",
    "func validateLimits(limits Limits) error {",
    "func normalizeAgentSelection(selection AgentSelection) AgentSelection {\n"
    "\tselection.Provider = strings.ToLower(strings.TrimSpace(selection.Provider))\n"
    "\tselection.Model = strings.TrimSpace(selection.Model)\n"
    "\treturn selection\n"
    "}\n\n"
    "func validateAgentSelections(settingsVersion int, executor, reviewer AgentSelection) error {\n"
    "\tif settingsVersion < 0 {\n"
    "\t\treturn fmt.Errorf(\"%w: agent_settings_version must not be negative\", ErrInvalidJob)\n"
    "\t}\n"
    "\texecutorComplete := executor.Provider != \"\" && executor.Model != \"\"\n"
    "\treviewerComplete := reviewer.Provider != \"\" && reviewer.Model != \"\"\n"
    "\tif (executor.Provider == \"\") != (executor.Model == \"\") ||\n"
    "\t\t(reviewer.Provider == \"\") != (reviewer.Model == \"\") {\n"
    "\t\treturn fmt.Errorf(\"%w: each agent selection requires both provider and model\", ErrInvalidJob)\n"
    "\t}\n"
    "\tif executorComplete != reviewerComplete {\n"
    "\t\treturn fmt.Errorf(\"%w: executor and reviewer must both be selected or both omitted\", ErrInvalidJob)\n"
    "\t}\n"
    "\tif executorComplete && executor.Provider == reviewer.Provider && executor.Model == reviewer.Model {\n"
    "\t\treturn fmt.Errorf(\"%w: executor and reviewer must use different provider/model pairs\", ErrInvalidJob)\n"
    "\t}\n"
    "\treturn nil\n"
    "}\n\n"
    "func validateLimits(limits Limits) error {",
)

# HTTP request contract.
replace_once(
    "internal/httpapi/workflow_jobs.go",
    "\tBaseBranch   string             `json:\"base_branch\"`\n\tLimits",
    "\tBaseBranch           string                     `json:\"base_branch\"`\n"
    "\tAgentSettingsVersion int                        `json:\"agent_settings_version\"`\n"
    "\tExecutor             workflowjob.AgentSelection `json:\"executor\"`\n"
    "\tReviewer             workflowjob.AgentSelection `json:\"reviewer\"`\n"
    "\tLimits",
)
replace_once(
    "internal/httpapi/workflow_jobs.go",
    "\t\t\tBaseBranch:   request.BaseBranch,\n\t\t\tLimits:",
    "\t\t\tBaseBranch:           request.BaseBranch,\n"
    "\t\t\tAgentSettingsVersion: request.AgentSettingsVersion,\n"
    "\t\t\tExecutor:             request.Executor,\n"
    "\t\t\tReviewer:             request.Reviewer,\n"
    "\t\t\tLimits:",
)

# Stage handlers prefer the workflow snapshot while retaining legacy fallback.
replace_once(
    "internal/execution/handler.go",
    "\tprovider := agent.Provider\n\tif provider == \"\" {\n\t\tprovider = h.defaultProvider\n\t}\n\tmodel := agent.Model\n\tif model == \"\" {\n\t\tmodel = h.defaultModel\n\t}",
    "\tprovider := strings.TrimSpace(job.Executor.Provider)\n"
    "\tmodel := strings.TrimSpace(job.Executor.Model)\n"
    "\tif provider == \"\" {\n"
    "\t\tprovider = agent.Provider\n"
    "\t}\n"
    "\tif provider == \"\" {\n"
    "\t\tprovider = h.defaultProvider\n"
    "\t}\n"
    "\tif model == \"\" {\n"
    "\t\tmodel = agent.Model\n"
    "\t}\n"
    "\tif model == \"\" {\n"
    "\t\tmodel = h.defaultModel\n"
    "\t}\n"
    "\tsettingsVersion := job.AgentSettingsVersion\n"
    "\tif settingsVersion < 1 {\n"
    "\t\tsettingsVersion = settings.Version\n"
    "\t}",
)
replace_once(
    "internal/execution/handler.go",
    "\t\tAgentSettingsVersion: settings.Version, Provider: provider, Model: model,",
    "\t\tAgentSettingsVersion: settingsVersion, Provider: provider, Model: model,",
)
replace_once(
    "internal/reviewer/handler.go",
    "\treviewerConfig, err := resolveReviewerConfig(settings, h.defaultProvider, h.defaultModel)\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n\trun, _, err := h.reviews.CreateOrGet(ctx, CreateInput{",
    "\treviewerConfig, err := resolveReviewerConfig(settings, h.defaultProvider, h.defaultModel)\n"
    "\tif err != nil {\n"
    "\t\treturn \"\", err\n"
    "\t}\n"
    "\tif provider := strings.TrimSpace(job.Reviewer.Provider); provider != \"\" {\n"
    "\t\treviewerConfig.Provider = provider\n"
    "\t}\n"
    "\tif model := strings.TrimSpace(job.Reviewer.Model); model != \"\" {\n"
    "\t\treviewerConfig.Model = model\n"
    "\t}\n"
    "\tsettingsVersion := job.AgentSettingsVersion\n"
    "\tif settingsVersion < 1 {\n"
    "\t\tsettingsVersion = settings.Version\n"
    "\t}\n"
    "\trun, _, err := h.reviews.CreateOrGet(ctx, CreateInput{",
)
replace_once(
    "internal/reviewer/handler.go",
    "\t\tAgentSettingsVersion: settings.Version,",
    "\t\tAgentSettingsVersion: settingsVersion,",
)

# TUI API types.
replace_once(
    "apps/tui/src/workflow-jobs.ts",
    "export type WorkflowJob = {",
    "export type WorkflowAgentSelection = {\n"
    "  provider: string\n"
    "  model: string\n"
    "}\n\n"
    "export type WorkflowJob = {",
)
replace_once(
    "apps/tui/src/workflow-jobs.ts",
    "  base_commit_sha: string\n  status:",
    "  base_commit_sha: string\n"
    "  agent_settings_version: number\n"
    "  executor: WorkflowAgentSelection\n"
    "  reviewer: WorkflowAgentSelection\n"
    "  status:",
)
replace_once(
    "apps/tui/src/workflow-jobs.ts",
    "    base_branch?: string\n    limits?:",
    "    base_branch?: string\n"
    "    agent_settings_version?: number\n"
    "    executor?: WorkflowAgentSelection\n"
    "    reviewer?: WorkflowAgentSelection\n"
    "    limits?:",
)

# Project screen opens a dedicated selector before creating the workflow.
replace_once(
    "apps/tui/src/project-screen.tsx",
    "import type { DaemonConnection } from \"./daemon\"",
    "import type { DaemonConnection } from \"./daemon\"\n"
    "import {\n"
    "  WorkflowAgentPicker,\n"
    "  type WorkflowAgentAssignment,\n"
    "} from \"./workflow-agent-picker\"",
)
replace_once(
    "apps/tui/src/project-screen.tsx",
    "  | \"plan\"\n  | \"questions\"",
    "  | \"plan\"\n  | \"questions\"\n  | \"workflow\"",
)
replace_once(
    "apps/tui/src/project-screen.tsx",
    "  const createSelectedWorkflow = async () => {",
    "  const openWorkflowAgentPicker = () => {\n"
    "    if (!selectedProject || !selectedRepository || !latestPlan || busy) return\n"
    "    if (latestPlan.status !== \"READY\" && latestPlan.status !== \"APPROVED\") {\n"
    "      setMessage(\"The latest plan must be READY before creating a workflow.\")\n"
    "      return\n"
    "    }\n"
    "    const baseBranch = selectedBranch?.name || selectedRepository.current_branch\n"
    "    if (!baseBranch || baseBranch === \"HEAD\") {\n"
    "      setMessage(\"Select a concrete base branch before creating a workflow.\")\n"
    "      return\n"
    "    }\n"
    "    setMode(\"workflow\")\n"
    "    setMessage(\"\")\n"
    "  }\n\n"
    "  const createSelectedWorkflow = async (assignment: WorkflowAgentAssignment) => {",
)
replace_once(
    "apps/tui/src/project-screen.tsx",
    "        base_branch: baseBranch,\n      })",
    "        base_branch: baseBranch,\n"
    "        agent_settings_version: assignment.agent_settings_version,\n"
    "        executor: assignment.executor,\n"
    "        reviewer: assignment.reviewer,\n"
    "      })",
)
replace_once(
    "apps/tui/src/project-screen.tsx",
    "        setMessage(\n          `Workflow ${started.id.slice(0, 8)} started from ${baseBranch}. Open Jobs to follow it.`,\n        )",
    "        setMode(\"list\")\n"
    "        setMessage(\n"
    "          `Workflow ${started.id.slice(0, 8)} started · executor ${assignment.executor.provider}/${assignment.executor.model} · reviewer ${assignment.reviewer.provider}/${assignment.reviewer.model}.`,\n"
    "        )",
)
replace_once(
    "apps/tui/src/project-screen.tsx",
    "        setMessage(\n          `Workflow ${created.id.slice(0, 8)} created READY; start failed: ${error instanceof Error ? error.message : \"unknown error\"}`,\n        )",
    "        setMode(\"list\")\n"
    "        setMessage(\n"
    "          `Workflow ${created.id.slice(0, 8)} created READY; start failed: ${error instanceof Error ? error.message : \"unknown error\"}`,\n"
    "        )",
)
replace_once(
    "apps/tui/src/project-screen.tsx",
    "    } catch (error) {\n      setMessage(error instanceof Error ? error.message : \"Failed to create workflow\")\n    } finally {",
    "    } catch (error) {\n"
    "      throw error instanceof Error ? error : new Error(\"Failed to create workflow\")\n"
    "    } finally {",
)
replace_once(
    "apps/tui/src/project-screen.tsx",
    "    if (mode === \"plan\" || mode === \"questions\") {",
    "    if (mode === \"plan\" || mode === \"questions\" || mode === \"workflow\") {",
)
replace_once(
    "apps/tui/src/project-screen.tsx",
    "    if (key.name === \"w\") {\n      void createSelectedWorkflow()\n      return\n    }",
    "    if (key.name === \"w\") {\n"
    "      openWorkflowAgentPicker()\n"
    "      return\n"
    "    }",
)
replace_once(
    "apps/tui/src/project-screen.tsx",
    "  if (mode === \"questions\" && planningRun) {",
    "  if (\n"
    "    mode === \"workflow\" &&\n"
    "    selectedProject &&\n"
    "    selectedRepository &&\n"
    "    latestPlan\n"
    "  ) {\n"
    "    const baseBranch = selectedBranch?.name || selectedRepository.current_branch\n"
    "    return (\n"
    "      <WorkflowAgentPicker\n"
    "        projectID={selectedProject.id}\n"
    "        projectName={selectedProject.name}\n"
    "        planTitle={latestPlan.title}\n"
    "        baseBranch={baseBranch}\n"
    "        onConfirm={createSelectedWorkflow}\n"
    "        onCancel={() => {\n"
    "          setMode(\"list\")\n"
    "          setMessage(\"Workflow creation cancelled.\")\n"
    "        }}\n"
    "      />\n"
    "    )\n"
    "  }\n\n"
    "  if (mode === \"questions\" && planningRun) {",
)

# Documentation.
append_once(
    "docs/provider-setup.md",
    "## Per-workflow agent selection",
    """
## Per-workflow agent selection

The Agents screen defines project defaults. Press `W` from Projects to create a workflow, then choose the Executor and Reviewer for that specific run. The selector starts from the project defaults, allows cycling through registered provider/model choices, and rejects an identical Executor/Reviewer pair.

The selected provider/model pairs and the source agent-settings version are persisted on the workflow job. Execution, revision, review, and re-review keep using that immutable assignment even when project defaults change later. Older workflow jobs without a stored assignment continue using the legacy project-default behavior.
""",
)

# Backend persistence and validation tests.
append_once(
    "internal/workflowjob/repository_test.go",
    "func TestCreatePersistsWorkflowAgentSelection",
    r'''
func TestCreatePersistsWorkflowAgentSelection(t *testing.T) {
	repository, _, db, _, input := openWorkflowRepository(t, "READY")
	defer db.Close()
	input.AgentSettingsVersion = 7
	input.Executor = AgentSelection{Provider: "deepseek", Model: "deepseek-coder"}
	input.Reviewer = AgentSelection{Provider: "openai", Model: "gpt-reviewer"}

	created, err := repository.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	loaded, err := repository.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if loaded.AgentSettingsVersion != 7 || loaded.Executor != input.Executor || loaded.Reviewer != input.Reviewer {
		t.Fatalf("agent selection = version %d executor %#v reviewer %#v", loaded.AgentSettingsVersion, loaded.Executor, loaded.Reviewer)
	}
}

func TestCreateRejectsIdenticalWorkflowAgents(t *testing.T) {
	repository, _, db, _, input := openWorkflowRepository(t, "READY")
	defer db.Close()
	input.Executor = AgentSelection{Provider: "openai", Model: "same-model"}
	input.Reviewer = input.Executor

	_, err := repository.Create(context.Background(), input)
	if !errors.Is(err, ErrInvalidJob) || !strings.Contains(err.Error(), "different provider/model") {
		t.Fatalf("Create() error = %v", err)
	}
}
''',
)

# New pure TUI selection model.
Path("apps/tui/src/workflow-agent-selection.ts").write_text(r'''import type { AgentConfig, AgentSettings } from "./agent-settings"
import type { LLMProviderInfo } from "./llm-providers"
import type { WorkflowAgentSelection } from "./workflow-jobs"

export type WorkflowAgentAssignment = {
  agent_settings_version: number
  executor: WorkflowAgentSelection
  reviewer: WorkflowAgentSelection
}

export type WorkflowAgentSelectionState = {
  choices: WorkflowAgentSelection[]
  executorIndex: number
  reviewerIndex: number
}

export function buildWorkflowAgentSelectionState(
  settings: AgentSettings,
  providers: LLMProviderInfo[],
): WorkflowAgentSelectionState {
  const configured = providers.filter(
    (provider) => provider.configured && provider.name.trim() && provider.default_model.trim(),
  )
  const choices: WorkflowAgentSelection[] = []
  const add = (choice: WorkflowAgentSelection | undefined) => {
    if (!choice?.provider.trim() || !choice.model.trim()) return
    const normalized = { provider: choice.provider.trim().toLowerCase(), model: choice.model.trim() }
    if (
      !choices.some(
        (existing) =>
          existing.provider === normalized.provider && existing.model === normalized.model,
      )
    ) {
      choices.push(normalized)
    }
  }

  add(explicitChoice(settings.agents, "EXECUTOR", configured))
  add(explicitChoice(settings.agents, "REVIEWER", configured))
  for (const provider of configured) {
    add({ provider: provider.name, model: provider.default_model })
  }

  const executor = resolveRoleChoice(settings.agents, "EXECUTOR", configured, choices)
  const reviewer = resolveRoleChoice(settings.agents, "REVIEWER", configured, choices)
  const executorIndex = Math.max(indexOfChoice(choices, executor), 0)
  let reviewerIndex = Math.max(indexOfChoice(choices, reviewer), 0)
  if (reviewerIndex === executorIndex && choices.length > 1) {
    reviewerIndex = (executorIndex + 1) % choices.length
  }
  return { choices, executorIndex, reviewerIndex }
}

export function validateWorkflowAgentAssignment(
  executor: WorkflowAgentSelection | undefined,
  reviewer: WorkflowAgentSelection | undefined,
): string | undefined {
  if (!executor || !reviewer) {
    return "Configure at least two provider/model choices before creating a workflow."
  }
  if (!executor.provider || !executor.model || !reviewer.provider || !reviewer.model) {
    return "Executor and Reviewer both require a provider and model."
  }
  if (executor.provider === reviewer.provider && executor.model === reviewer.model) {
    return "Executor and Reviewer must use different provider/model pairs."
  }
  return undefined
}

export function cycleChoice(current: number, delta: number, length: number): number {
  if (length < 1) return 0
  return (current + delta + length) % length
}

function explicitChoice(
  agents: AgentConfig[],
  role: "EXECUTOR" | "REVIEWER",
  providers: LLMProviderInfo[],
): WorkflowAgentSelection | undefined {
  const agent = agents.find((item) => item.role === role && item.enabled)
  if (!agent?.provider || !agent.model) return undefined
  if (!providers.some((provider) => provider.name === agent.provider && provider.configured)) {
    return undefined
  }
  return { provider: agent.provider, model: agent.model }
}

function resolveRoleChoice(
  agents: AgentConfig[],
  role: "EXECUTOR" | "REVIEWER",
  providers: LLMProviderInfo[],
  choices: WorkflowAgentSelection[],
): WorkflowAgentSelection | undefined {
  const explicit = explicitChoice(agents, role, providers)
  if (explicit) return explicit
  const defaultProvider = providers.find((provider) => provider.default) ?? providers[0]
  if (defaultProvider) {
    return { provider: defaultProvider.name, model: defaultProvider.default_model }
  }
  return choices[0]
}

function indexOfChoice(
  choices: WorkflowAgentSelection[],
  target: WorkflowAgentSelection | undefined,
): number {
  if (!target) return -1
  return choices.findIndex(
    (choice) => choice.provider === target.provider && choice.model === target.model,
  )
}
''')

Path("apps/tui/src/workflow-agent-selection.test.ts").write_text(r'''import { describe, expect, test } from "bun:test"

import type { AgentSettings } from "./agent-settings"
import type { LLMProviderInfo } from "./llm-providers"
import {
  buildWorkflowAgentSelectionState,
  cycleChoice,
  validateWorkflowAgentAssignment,
} from "./workflow-agent-selection"

const settings: AgentSettings = {
  project_id: "project-1",
  version: 4,
  agents: [
    {
      role: "EXECUTOR",
      provider: "deepseek",
      model: "deepseek-coder",
      temperature: 0,
      max_output_tokens: 8000,
      enabled: true,
      system_instruction: "",
    },
    {
      role: "REVIEWER",
      provider: "openai",
      model: "gpt-reviewer",
      temperature: 0,
      max_output_tokens: 8000,
      enabled: true,
      system_instruction: "",
    },
  ],
  tool_policies: [],
  created_at: "2026-08-05T00:00:00Z",
  updated_at: "2026-08-05T00:00:00Z",
}

const providers: LLMProviderInfo[] = [
  {
    name: "deepseek",
    default_model: "deepseek-chat",
    configured: true,
    structured_output: true,
    default: true,
    base_url: "https://api.deepseek.com",
    json_mode: "json_object",
    timeout_ms: 60000,
    credential_stored: true,
    source: "tui",
    editable: true,
    deletable: true,
  },
  {
    name: "openai",
    default_model: "gpt-default",
    configured: true,
    structured_output: true,
    default: false,
    base_url: "https://api.openai.com/v1",
    json_mode: "json_schema",
    timeout_ms: 60000,
    credential_stored: true,
    source: "tui",
    editable: true,
    deletable: true,
  },
]

describe("workflow agent selection", () => {
  test("starts from project role defaults and preserves custom models", () => {
    const state = buildWorkflowAgentSelectionState(settings, providers)
    expect(state.choices[state.executorIndex]).toEqual({
      provider: "deepseek",
      model: "deepseek-coder",
    })
    expect(state.choices[state.reviewerIndex]).toEqual({
      provider: "openai",
      model: "gpt-reviewer",
    })
  })

  test("rejects identical executor and reviewer pairs", () => {
    expect(
      validateWorkflowAgentAssignment(
        { provider: "openai", model: "same" },
        { provider: "openai", model: "same" },
      ),
    ).toContain("different")
  })

  test("cycles choices in both directions", () => {
    expect(cycleChoice(0, -1, 3)).toBe(2)
    expect(cycleChoice(2, 1, 3)).toBe(0)
  })
})
''')

Path("apps/tui/src/workflow-agent-picker.tsx").write_text(r'''/** @jsxImportSource @opentui/react */

import { useKeyboard } from "@opentui/react"
import { useCallback, useEffect, useState } from "react"

import { getAgentSettings } from "./agent-settings"
import { listLLMProviders } from "./llm-providers"
import {
  buildWorkflowAgentSelectionState,
  cycleChoice,
  type WorkflowAgentAssignment,
  validateWorkflowAgentAssignment,
} from "./workflow-agent-selection"
import type { WorkflowAgentSelection } from "./workflow-jobs"
import { Banner, BOLD, Card, Chip, colors, KeyHints, PageHeader, Section } from "./ui"

export type { WorkflowAgentAssignment } from "./workflow-agent-selection"

type ActiveRole = "executor" | "reviewer"

export function WorkflowAgentPicker({
  projectID,
  projectName,
  planTitle,
  baseBranch,
  onConfirm,
  onCancel,
}: {
  projectID: string
  projectName: string
  planTitle: string
  baseBranch: string
  onConfirm: (assignment: WorkflowAgentAssignment) => Promise<void>
  onCancel: () => void
}) {
  const [activeRole, setActiveRole] = useState<ActiveRole>("executor")
  const [choices, setChoices] = useState<WorkflowAgentSelection[]>([])
  const [executorIndex, setExecutorIndex] = useState(0)
  const [reviewerIndex, setReviewerIndex] = useState(0)
  const [settingsVersion, setSettingsVersion] = useState(0)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState("")

  const load = useCallback(async () => {
    setLoading(true)
    setMessage("")
    try {
      const [settings, providers] = await Promise.all([
        getAgentSettings(projectID),
        listLLMProviders(),
      ])
      const state = buildWorkflowAgentSelectionState(settings, providers)
      setChoices(state.choices)
      setExecutorIndex(state.executorIndex)
      setReviewerIndex(state.reviewerIndex)
      setSettingsVersion(settings.version)
      if (state.choices.length < 2) {
        setMessage(
          "Add another provider/model in Settings or Agents so Executor and Reviewer can be separated.",
        )
      }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to load workflow agents")
    } finally {
      setLoading(false)
    }
  }, [projectID])

  useEffect(() => {
    void load()
  }, [load])

  const executor = choices[executorIndex]
  const reviewer = choices[reviewerIndex]

  const moveChoice = (delta: number) => {
    if (busy || loading || choices.length === 0) return
    if (activeRole === "executor") {
      setExecutorIndex((current) => cycleChoice(current, delta, choices.length))
    } else {
      setReviewerIndex((current) => cycleChoice(current, delta, choices.length))
    }
    setMessage("")
  }

  const confirm = async () => {
    if (busy || loading) return
    const validation = validateWorkflowAgentAssignment(executor, reviewer)
    if (validation) {
      setMessage(validation)
      return
    }
    setBusy(true)
    setMessage("Creating workflow with the selected agents...")
    try {
      await onConfirm({
        agent_settings_version: settingsVersion,
        executor: executor as WorkflowAgentSelection,
        reviewer: reviewer as WorkflowAgentSelection,
      })
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to create workflow")
      setBusy(false)
    }
  }

  useKeyboard((key) => {
    if (key.name === "escape" && !busy) {
      onCancel()
      return
    }
    if (key.name === "tab") {
      setActiveRole((current) => (current === "executor" ? "reviewer" : "executor"))
      return
    }
    if (key.name === "left" || key.name === "h" || key.name === "up" || key.name === "k") {
      moveChoice(-1)
      return
    }
    if (key.name === "right" || key.name === "l" || key.name === "down" || key.name === "j") {
      moveChoice(1)
      return
    }
    if (key.name === "r" && !busy) {
      void load()
      return
    }
    if (key.name === "return" || key.name === "enter" || (key.ctrl && key.name === "s")) {
      void confirm()
    }
  })

  return (
    <box flexDirection="column" flexGrow={1} gap={1}>
      <PageHeader
        title="Choose workflow agents"
        description="These provider/model pairs are frozen for execution, revision, review, and re-review. Project defaults are used only as the starting selection."
        meta={`${projectName} · ${baseBranch}`}
      />

      <Section title={`Plan · ${planTitle}`}>
        <box flexDirection="row" gap={1}>
          <AgentChoiceCard role="Executor" active={activeRole === "executor"} choice={executor} />
          <AgentChoiceCard role="Reviewer" active={activeRole === "reviewer"} choice={reviewer} />
        </box>
      </Section>

      <Card>
        <text fg={colors.muted}>Available provider/model choices</text>
        {loading ? <text fg={colors.warning}>Loading registered providers...</text> : null}
        {!loading && choices.length === 0 ? (
          <text fg={colors.danger}>No configured provider/model choices are available.</text>
        ) : null}
        {choices.map((choice, index) => (
          <text
            key={`${choice.provider}:${choice.model}`}
            fg={index === executorIndex || index === reviewerIndex ? colors.accent : colors.faint}
          >
            {`${index + 1}. ${choice.provider}/${choice.model}`}
          </text>
        ))}
        <text fg={colors.faint}>{`Agent settings snapshot version: ${settingsVersion || "unavailable"}`}</text>
      </Card>

      <KeyHints
        shortcuts={[
          { key: "Tab", label: "switch Executor / Reviewer" },
          { key: "←→", label: "choose provider/model" },
          { key: "Enter", label: "create and start workflow" },
          { key: "R", label: "reload defaults" },
          { key: "Esc", label: "cancel" },
        ]}
      />
      {message ? (
        <Banner tone={busy || loading ? "warning" : "danger"}>
          <text fg={busy || loading ? colors.warning : colors.danger}>{message}</text>
        </Banner>
      ) : null}
    </box>
  )
}

function AgentChoiceCard({
  role,
  active,
  choice,
}: {
  role: "Executor" | "Reviewer"
  active: boolean
  choice?: WorkflowAgentSelection
}) {
  return (
    <box
      width="50%"
      flexDirection="column"
      gap={1}
      padding={1}
      borderStyle="rounded"
      borderColor={active ? colors.accent : colors.line}
      backgroundColor={active ? colors.accentTint : colors.raised}
    >
      <text fg={active ? colors.text : colors.muted} attributes={active ? BOLD : 0}>
        {role}
      </text>
      {choice ? (
        <>
          <Chip label={choice.provider} tone={active ? "accent" : "neutral"} />
          <text fg={colors.text}>{choice.model}</text>
        </>
      ) : (
        <text fg={colors.danger}>No selection</text>
      )}
      <text fg={colors.faint}>{active ? "←/→ to change" : "Tab to select"}</text>
    </box>
  )
}
''')

print("workflow agent selection patch applied")
