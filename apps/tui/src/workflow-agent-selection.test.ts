import { describe, expect, test } from "bun:test"

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
