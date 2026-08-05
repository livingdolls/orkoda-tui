import { describe, expect, test } from "bun:test"
import { type AgentSettings, validateDistinctAgentPair } from "./agent-settings"

const settings = {
  project_id: "project-1",
  version: 1,
  agents: [
    {
      role: "PLANNER",
      provider: "",
      model: "",
      temperature: 0,
      max_output_tokens: 1000,
      enabled: true,
      system_instruction: "",
    },
    {
      role: "EXECUTOR",
      provider: "deepseek",
      model: "coder",
      temperature: 0,
      max_output_tokens: 1000,
      enabled: true,
      system_instruction: "",
    },
    {
      role: "REVIEWER",
      provider: "openai",
      model: "reviewer",
      temperature: 0,
      max_output_tokens: 1000,
      enabled: true,
      system_instruction: "",
    },
  ],
  tool_policies: [],
  created_at: "",
  updated_at: "",
} satisfies AgentSettings

describe("agent separation", () => {
  test("accepts different assignments", () =>
    expect(validateDistinctAgentPair(settings)).toBeUndefined())
  test("rejects the same explicit assignment", () => {
    const duplicate: AgentSettings = {
      ...settings,
      agents: settings.agents.map((agent) =>
        agent.role === "REVIEWER"
          ? { ...agent, provider: "deepseek", model: "coder" }
          : { ...agent },
      ),
    }
    expect(validateDistinctAgentPair(duplicate)).toContain("must not use")
  })
})
