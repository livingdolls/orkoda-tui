import type { ActivityEvent } from "./events"

export type AgentLiveLine = {
  title: string
  detail: string
  tone: "neutral" | "warning" | "success" | "danger"
}

export function isAgentLiveEvent(event: ActivityEvent): boolean {
  return event.type.startsWith("executor.") || event.type.startsWith("execution.")
}

export function formatAgentLiveEvent(event: ActivityEvent): AgentLiveLine {
  const payload = event.payload ?? {}
  const turn = numberValue(payload.turn)
  const tool = textValue(payload.tool)
  const path = textValue(payload.path)
  const summary = textValue(payload.summary)
  const status = textValue(payload.status)
  const error = textValue(payload.error) || textValue(payload.failure_message)

  switch (event.type) {
    case "executor.dispatch.started":
      return { title: "Executor dispatch started", detail: attemptDetail(payload), tone: "neutral" }
    case "executor.workspace.ready":
      return {
        title: "Workspace ready",
        detail: textValue(payload.workspace_status) || "isolated workspace loaded",
        tone: "success",
      }
    case "executor.settings.ready":
      return { title: "Agent settings loaded", detail: versionDetail(payload), tone: "success" }
    case "executor.execution.ready":
      return { title: "Execution initialized", detail: providerDetail(payload), tone: "success" }
    case "executor.lease.acquired":
      return {
        title: "Workspace lease acquired",
        detail: "Executor has exclusive write access",
        tone: "success",
      }
    case "executor.context.selecting":
      return {
        title: "Preparing context",
        detail: status || "reading repository and plan context",
        tone: "neutral",
      }
    case "executor.context.ready":
      return { title: "Context ready", detail: byteDetail(payload.context_bytes), tone: "success" }
    case "executor.turn.started":
      return {
        title: `Turn ${turn || "?"} · waiting for model`,
        detail: status || "request sent",
        tone: "warning",
      }
    case "executor.turn.received":
      return {
        title: `Turn ${turn || "?"} · ${tool || textValue(payload.action_type) || "response"}`,
        detail:
          [path, summary, tokenDetail(payload)].filter(Boolean).join(" · ") ||
          "structured action received",
        tone: "neutral",
      }
    case "executor.tool.started":
      return {
        title: `Running ${tool || "tool"}`,
        detail: path || textValue(payload.query) || `turn ${turn || "?"}`,
        tone: "warning",
      }
    case "executor.tool.completed":
      return {
        title: `${tool || "Tool"} completed`,
        detail: resultDetail(payload.result),
        tone: "success",
      }
    case "executor.tool.failed":
      return {
        title: `${tool || "Tool"} failed`,
        detail: [textValue(payload.error_code), error].filter(Boolean).join(": "),
        tone: "danger",
      }
    case "executor.finalization.started":
      return {
        title: "Finalizing implementation",
        detail: status || "waiting for completion decision",
        tone: "warning",
      }
    case "executor.finalization.received":
      return {
        title: `Final decision · ${textValue(payload.decision) || "received"}`,
        detail: summary,
        tone: textValue(payload.decision) === "finish" ? "success" : "warning",
      }
    case "execution.started":
      return { title: "Executor running", detail: providerDetail(payload), tone: "warning" }
    case "execution.completed":
      return {
        title: "Implementation completed",
        detail: changedFilesDetail(payload),
        tone: "success",
      }
    case "execution.failed":
      return {
        title: "Executor failed",
        detail: error || textValue(payload.failure_code),
        tone: "danger",
      }
    case "execution.paused":
      return {
        title: "Executor paused",
        detail: error || textValue(payload.failure_code),
        tone: "warning",
      }
    default:
      return {
        title: event.type.replaceAll(".", " "),
        detail: summary || status || error || "progress recorded",
        tone: "neutral",
      }
  }
}

function textValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : ""
}

function numberValue(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined
}

function attemptDetail(payload: Record<string, unknown>): string {
  const attempt = numberValue(payload.attempt)
  const max = numberValue(payload.max_attempts)
  return attempt && max ? `attempt ${attempt}/${max}` : "dispatch accepted"
}

function versionDetail(payload: Record<string, unknown>): string {
  const version = numberValue(payload.agent_settings_version)
  return version ? `settings v${version}` : "Executor profile resolved"
}

function providerDetail(payload: Record<string, unknown>): string {
  return (
    [textValue(payload.provider), textValue(payload.model)].filter(Boolean).join(" / ") ||
    "execution record ready"
  )
}

function byteDetail(value: unknown): string {
  const bytes = numberValue(value)
  return bytes === undefined
    ? "repository context selected"
    : `${bytes.toLocaleString()} context bytes`
}

function tokenDetail(payload: Record<string, unknown>): string {
  const tokens = numberValue(payload.total_tokens)
  return tokens === undefined ? "" : `${tokens.toLocaleString()} tokens`
}

function resultDetail(value: unknown): string {
  if (!value || typeof value !== "object") return "tool result persisted"
  const record = value as Record<string, unknown>
  const path = textValue(record.path)
  const matches = numberValue(record.matches)
  const bytes = numberValue(record.bytes)
  if (path) return path
  if (matches !== undefined) return `${matches} matches`
  if (bytes !== undefined) return `${bytes.toLocaleString()} bytes`
  return "tool result persisted"
}

function changedFilesDetail(payload: Record<string, unknown>): string {
  const count = numberValue(payload.changed_file_count)
  return count === undefined ? "checkpoint saved" : `${count} changed file(s)`
}
