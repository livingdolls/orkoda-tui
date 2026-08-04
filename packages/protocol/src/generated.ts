/* eslint-disable */
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
