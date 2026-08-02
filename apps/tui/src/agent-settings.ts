import { daemonBaseURL } from "./daemon"

export type AgentRole = "PLANNER" | "EXECUTOR" | "REVIEWER"
export type NetworkAccess = "DISABLED" | "LOOPBACK" | "OUTBOUND"
export type FilesystemAccess = "READ_ONLY" | "WORKSPACE_WRITE"

export type AgentConfig = {
  role: AgentRole
  provider: string
  model: string
  temperature: number
  max_output_tokens: number
  enabled: boolean
  system_instruction: string
}

export type ToolPolicy = {
  role: AgentRole
  allowed_tools: string[]
  allowed_command_profiles: string[]
  network_access: NetworkAccess
  filesystem_access: FilesystemAccess
  command_timeout_ms: number
  max_command_output_bytes: number
  max_file_bytes: number
  max_patch_bytes: number
}

export type AgentSettings = {
  project_id: string
  version: number
  agents: AgentConfig[]
  tool_policies: ToolPolicy[]
  created_at: string
  updated_at: string
}

export type AgentSettingsFetch = (
  input: string | URL | Request,
  init?: RequestInit,
) => Promise<Response>

type DataResponse<T> = { data: T }
type ErrorResponse = { error?: { message?: string } }

async function request<T>(
  path: string,
  init: RequestInit,
  fetcher: AgentSettingsFetch,
): Promise<T> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 10000)
  const headers = new Headers(init.headers)
  headers.set("accept", "application/json")
  if (init.body) {
    headers.set("content-type", "application/json")
  }

  try {
    const response = await fetcher(`${daemonBaseURL}${path}`, {
      ...init,
      headers,
      signal: controller.signal,
    })
    if (!response.ok) {
      let message = `Daemon returned HTTP ${response.status}`
      try {
        const payload = (await response.json()) as ErrorResponse
        if (payload.error?.message) {
          message = payload.error.message
        }
      } catch {
        // Keep the HTTP fallback for non-JSON errors.
      }
      throw new Error(message)
    }
    const payload = (await response.json()) as DataResponse<T>
    return payload.data
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error("Agent settings request timed out")
    }
    throw error
  } finally {
    clearTimeout(timeout)
  }
}

export function getAgentSettings(
  projectID: string,
  fetcher: AgentSettingsFetch = fetch,
): Promise<AgentSettings> {
  return request<AgentSettings>(
    `/api/v1/projects/${projectID}/agent-settings`,
    { method: "GET" },
    fetcher,
  )
}

export function updateAgentSettings(
  settings: AgentSettings,
  fetcher: AgentSettingsFetch = fetch,
): Promise<AgentSettings> {
  return request<AgentSettings>(
    `/api/v1/projects/${settings.project_id}/agent-settings`,
    {
      method: "PUT",
      body: JSON.stringify({
        expected_version: settings.version,
        agents: settings.agents,
        tool_policies: settings.tool_policies,
      }),
    },
    fetcher,
  )
}

export function cloneAgentSettings(settings: AgentSettings): AgentSettings {
  return {
    ...settings,
    agents: settings.agents.map((agent) => ({ ...agent })),
    tool_policies: settings.tool_policies.map((policy) => ({
      ...policy,
      allowed_tools: [...policy.allowed_tools],
      allowed_command_profiles: [...policy.allowed_command_profiles],
    })),
  }
}
