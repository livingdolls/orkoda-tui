import type { AgentConfig, AgentSettings } from "./agent-settings"
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
    const normalized = {
      provider: choice.provider.trim().toLowerCase(),
      model: choice.model.trim(),
    }
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
