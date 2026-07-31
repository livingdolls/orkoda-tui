export type DaemonConnectionState = "checking" | "connected" | "disconnected"

export type DaemonConnection = {
  state: DaemonConnectionState
  protocolVersion: string
  message: string
}

type HealthResponse = {
  status?: unknown
  protocol_version?: unknown
}

export const daemonBaseURL = (process.env.ORKODA_DAEMON_URL ?? "http://127.0.0.1:8181").replace(/\/$/, "")

export const initialDaemonConnection: DaemonConnection = {
  state: "checking",
  protocolVersion: "v1",
  message: "Checking local daemon...",
}

export async function probeDaemon(
  baseURL = daemonBaseURL,
  fetcher: typeof fetch = fetch,
): Promise<DaemonConnection> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 1500)

  try {
    const response = await fetcher(`${baseURL}/health/live`, {
      headers: { accept: "application/json" },
      signal: controller.signal,
    })

    if (!response.ok) {
      return {
        state: "disconnected",
        protocolVersion: "v1",
        message: `Daemon returned HTTP ${response.status}`,
      }
    }

    const payload = (await response.json()) as HealthResponse
    if (payload.status !== "ok") {
      return {
        state: "disconnected",
        protocolVersion: "v1",
        message: "Daemon health response is invalid",
      }
    }

    return {
      state: "connected",
      protocolVersion: typeof payload.protocol_version === "string" ? payload.protocol_version : "v1",
      message: `Connected to ${baseURL}`,
    }
  } catch (error) {
    const message = error instanceof Error && error.name === "AbortError" ? "Daemon health check timed out" : "Daemon is not running"

    return {
      state: "disconnected",
      protocolVersion: "v1",
      message,
    }
  } finally {
    clearTimeout(timeout)
  }
}
