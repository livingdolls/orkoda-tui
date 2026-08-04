import { daemonBaseURL, type Fetcher, requestWithDaemonAuth } from "./daemon"

export type ActivityEvent = {
  sequence: number
  job_id?: string
  type: string
  payload: Record<string, unknown>
  created_at: string
}

type StreamOptions = {
  jobID?: string
  afterSequence?: number
  onEvent: (event: ActivityEvent) => void
  onState?: (state: "connected" | "reconnecting" | "closed") => void
  fetcher?: Fetcher
}

export function subscribeToEvents(options: StreamOptions): () => void {
  let stopped = false
  let controller: AbortController | undefined
  let lastSequence = options.afterSequence ?? 0
  let retry = 0

  const run = async () => {
    while (!stopped) {
      controller = new AbortController()
      const query = new URLSearchParams({ stream: "true", after_sequence: String(lastSequence) })
      if (options.jobID) query.set("job_id", options.jobID)
      const path = options.jobID
        ? `/api/v1/jobs/${options.jobID}/events?${query.toString()}`
        : `/api/v1/events?${query.toString()}`
      try {
        const response = await requestWithDaemonAuth(
          options.fetcher ?? fetch,
          `${daemonBaseURL}${path}`,
          {
            method: "GET",
            headers: { accept: "text/event-stream" },
            signal: controller.signal,
          },
        )
        if (!response.ok || !response.body) {
          throw new Error(`event stream returned HTTP ${response.status}`)
        }
        retry = 0
        options.onState?.("connected")
        await consumeSSE(
          response.body,
          (event) => {
            if (event.sequence <= lastSequence) return
            lastSequence = event.sequence
            options.onEvent(event)
          },
          controller.signal,
        )
      } catch {
        if (stopped) break
        options.onState?.("reconnecting")
        const delay = Math.min(5000, 250 * 2 ** retry)
        retry += 1
        await new Promise((resolve) => setTimeout(resolve, delay))
      }
    }
    options.onState?.("closed")
  }

  void run()
  return () => {
    stopped = true
    controller?.abort()
  }
}

async function consumeSSE(
  body: ReadableStream<Uint8Array>,
  onEvent: (event: ActivityEvent) => void,
  signal: AbortSignal,
): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ""
  let eventID = ""
  let eventType = "message"
  let data: string[] = []
  try {
    while (!signal.aborted) {
      const chunk = await reader.read()
      if (chunk.done) return
      buffer += decoder.decode(chunk.value, { stream: true })
      const lines = buffer.split(/\r?\n/)
      buffer = lines.pop() ?? ""
      for (const line of lines) {
        if (line === "") {
          if (data.length > 0) {
            try {
              const parsed = JSON.parse(data.join("\n")) as ActivityEvent
              if (!parsed.sequence && eventID) parsed.sequence = Number(eventID)
              if (parsed.type === undefined) parsed.type = eventType
              if (parsed.payload === undefined) parsed.payload = {}
              onEvent(parsed)
            } catch {
              // Ignore malformed or keep-alive frames; reconnect will replay.
            }
          }
          eventID = ""
          eventType = "message"
          data = []
          continue
        }
        if (line.startsWith(":")) continue
        const separator = line.indexOf(":")
        const field = separator >= 0 ? line.slice(0, separator) : line
        const value = separator >= 0 ? line.slice(separator + 1).replace(/^ /, "") : ""
        if (field === "id") eventID = value
        else if (field === "event") eventType = value
        else if (field === "data") data.push(value)
      }
    }
  } finally {
    reader.releaseLock()
  }
}
