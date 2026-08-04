import { mkdir } from "node:fs/promises"
import { join } from "node:path"

const root = new URL("..", import.meta.url)
const schemaURL = new URL("packages/protocol/schemas/envelope.schema.json", root)
const schema = await Bun.file(schemaURL).json()
if (schema.title !== "Orkoda API Envelope" || schema.type !== "object") {
  throw new Error("unexpected protocol envelope schema")
}

const output = `/* eslint-disable */
// Generated from schemas/envelope.schema.json by scripts/generate-protocol.ts.

export const protocolVersion = "v1" as const

export type ProtocolMeta = {
  protocol_version?: string
  request_id?: string
  correlation_id?: string
  [key: string]: unknown
}

export type ProtocolError = {
  code?: string
  message: string
  details?: Record<string, unknown>
}

export type ApiEnvelope<T> = {
  data?: T
  meta?: ProtocolMeta
  error?: ProtocolError
  request_id?: string
}

export type ActivityEvent = {
  sequence: number
  job_id?: string
  type: string
  payload: Record<string, unknown>
  created_at: string
}
`

const outputURL = new URL("packages/protocol/src/generated.ts", root)
await mkdir(new URL("packages/protocol/src/", root), { recursive: true })
await Bun.write(outputURL, output)
