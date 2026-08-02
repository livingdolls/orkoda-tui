import { describe, expect, test } from "bun:test"

import {
  cloneAgentSettings,
  type AgentSettings,
  type AgentSettingsFetch,
  getAgentSettings,
  updateAgentSettings,
} from "./agent-settings"

function settingsFixture(): AgentSettings {
  return {
    project_id: "project-1",
    version: 3,
    agents: [
      {
        role: "PLANNER",
        provider: "",
        model: "",
        temperature: 0.1,
        max_output_tokens: 4096,
        enabled: true,
        system_instruction: "",
      },
      {
        role: "EXECUTOR",
        provider: "openrouter",
        model: "example/model",
        temperature: 0.1,
        max_output_tokens: 8192,
        enabled: true,
        system_instruction: "",
      },
      {
        role: "REVIEWER",
        provider: "",
        model: "",
        temperature: 0,
        max_output_tokens: 4096,
        enabled: true,
        system_instruction: "",
      },
    ],
    tool_policies: [
      {
        role: "PLANNER",
        allowed_tools: [],
        allowed_command_profiles: [],
        network_access: "DISABLED",
        filesystem_access: "READ_ONLY",
        command_timeout_ms: 30000,
        max_command_output_bytes: 262144,
        max_file_bytes: 1048576,
        max_patch_bytes: 1048576,
      },
      {
        role: "EXECUTOR",
        allowed_tools: ["file_read", "git_diff"],
        allowed_command_profiles: [],
        network_access: "DISABLED",
        filesystem_access: "WORKSPACE_WRITE",
        command_timeout_ms: 120000,
        max_command_output_bytes: 1048576,
        max_file_bytes: 2097152,
        max_patch_bytes: 4194304,
      },
      {
        role: "REVIEWER",
        allowed_tools: ["git_diff"],
        allowed_command_profiles: [],
        network_access: "DISABLED",
        filesystem_access: "READ_ONLY",
        command_timeout_ms: 30000,
        max_command_output_bytes: 262144,
        max_file_bytes: 2097152,
        max_patch_bytes: 4194304,
      },
    ],
    created_at: "2026-08-02T00:00:00Z",
    updated_at: "2026-08-02T00:00:00Z",
  }
}

describe("agent settings API client", () => {
  test("loads project-scoped settings", async () => {
    let requestedURL = ""
    const fetcher: AgentSettingsFetch = async (input, init) => {
      requestedURL = String(input)
      expect(init?.method).toBe("GET")
      return new Response(JSON.stringify({ data: settingsFixture() }), { status: 200 })
    }

    const settings = await getAgentSettings("project-1", fetcher)
    expect(requestedURL).toEndWith("/api/v1/projects/project-1/agent-settings")
    expect(settings.version).toBe(3)
    expect(settings.tool_policies[1]?.network_access).toBe("DISABLED")
  })

  test("updates the complete versioned aggregate", async () => {
    let payload: unknown
    const fetcher: AgentSettingsFetch = async (_input, init) => {
      payload = JSON.parse(String(init?.body))
      return new Response(JSON.stringify({ data: { ...settingsFixture(), version: 4 } }), {
        status: 200,
      })
    }

    const updated = await updateAgentSettings(settingsFixture(), fetcher)
    expect(updated.version).toBe(4)
    expect(payload).toEqual({
      expected_version: 3,
      agents: settingsFixture().agents,
      tool_policies: settingsFixture().tool_policies,
    })
  })

  test("deep clones mutable policy arrays", () => {
    const original = settingsFixture()
    const cloned = cloneAgentSettings(original)
    cloned.agents[0]!.enabled = false
    cloned.tool_policies[1]!.allowed_tools.push("file_search")

    expect(original.agents[0]?.enabled).toBe(true)
    expect(original.tool_policies[1]?.allowed_tools).toEqual(["file_read", "git_diff"])
  })

  test("surfaces version conflicts from the daemon", async () => {
    const fetcher: AgentSettingsFetch = async () =>
      new Response(
        JSON.stringify({ error: { message: "agent settings changed; reload before saving" } }),
        { status: 409 },
      )

    await expect(updateAgentSettings(settingsFixture(), fetcher)).rejects.toThrow(
      "agent settings changed; reload before saving",
    )
  })
})
