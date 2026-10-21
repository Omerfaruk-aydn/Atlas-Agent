// Pure state transitions for the event stream. Kept free of Solid so they
// can be tested directly.

import type { FinishReason, Message, PermissionRequest, QuestionRequest, RunComplete, Session } from "../api/types"

/** upsertMessage inserts or replaces a message. Events carry the whole
 * message, so applying one twice is harmless, and applying the stream in
 * order after a snapshot always ends at the newest state.
 *
 * Creation times have one-second resolution, so ties are common: a new
 * message goes after every message created in the same second, which is
 * the order the server creates and lists them in. */
export function upsertMessage(list: readonly Message[], msg: Message): Message[] {
  const i = list.findIndex((m) => m.id === msg.id)
  if (i >= 0) {
    const next = list.slice()
    // Keep the original position: created_at never changes for a message.
    next[i] = msg
    return next
  }
  let at = list.length
  while (at > 0 && list[at - 1].created_at > msg.created_at) at--
  const next = list.slice()
  next.splice(at, 0, msg)
  return next
}

export function removeMessage(list: readonly Message[], id: string): Message[] {
  return list.filter((m) => m.id !== id)
}

/** sortMessages orders a snapshot by creation time, keeping the server's
 * order among messages created in the same second (the sort is stable). */
export function sortMessages(list: readonly Message[]): Message[] {
  return list.slice().sort((a, b) => a.created_at - b.created_at)
}

export function upsertSession(list: readonly Session[], s: Session): Session[] {
  const i = list.findIndex((x) => x.id === s.id)
  const next = list.slice()
  if (i >= 0) next[i] = { ...next[i], ...s }
  else next.push(s)
  return sortSessions(next)
}

export function removeSession(list: readonly Session[], id: string): Session[] {
  return list.filter((s) => s.id !== id)
}

/** Most recently updated first; subagent sessions are not listed. */
export function sortSessions(list: Session[]): Session[] {
  return list
    .filter((s) => !s.parent_session_id && !s.id.includes("$$"))
    .sort((a, b) => b.updated_at - a.updated_at || b.created_at - a.created_at)
}

export function addPermission(list: readonly PermissionRequest[], p: PermissionRequest): PermissionRequest[] {
  return list.some((x) => x.id === p.id) ? list.slice() : list.concat(p)
}

/** A notification with a decision closes the request for that tool call,
 * whichever client made the decision. One without a decision only says a
 * request is coming. */
export function settlePermission(
  list: readonly PermissionRequest[],
  n: { tool_call_id: string; granted?: boolean; denied?: boolean },
): PermissionRequest[] {
  if (!n.granted && !n.denied) return list.slice()
  return list.filter((p) => p.tool_call_id !== n.tool_call_id)
}

export function addQuestion(list: readonly QuestionRequest[], q: QuestionRequest): QuestionRequest[] {
  return list.some((x) => x.id === q.id) ? list.slice() : list.concat(q)
}

export function settleQuestion(list: readonly QuestionRequest[], batchID: string): QuestionRequest[] {
  return list.filter((q) => q.id !== batchID)
}

/** The state a session is in, as the composer and session list show it. */
export type RunState = "idle" | "starting" | "running" | "question" | "permission" | "cancelled" | "failed" | "done"

export interface RunInputs {
  busy: boolean
  starting: boolean
  hasQuestion: boolean
  hasPermission: boolean
  lastRun?: RunComplete
  lastFinish?: FinishReason
  error?: string
}

/** runState derives a session's state from server facts only. Busy comes
 * from the gateway's session_status, which it reads back from the server,
 * so a late message event can never make a finished run look active. */
export function runState(i: RunInputs): RunState {
  if (i.hasPermission) return "permission"
  if (i.hasQuestion) return "question"
  if (i.busy) return "running"
  if (i.starting) return "starting"
  if (i.lastRun) {
    if (i.lastRun.cancelled) return "cancelled"
    if (i.lastRun.error) return "failed"
    return "done"
  }
  if (i.error) return "failed"
  if (i.lastFinish === "canceled") return "cancelled"
  if (i.lastFinish === "error") return "failed"
  return "idle"
}

/** lastFinish returns the finish reason of the newest assistant message. */
export function lastFinish(list: readonly Message[]): FinishReason | undefined {
  for (let i = list.length - 1; i >= 0; i--) {
    const m = list[i]
    if (m.role !== "assistant") continue
    for (const p of m.parts) if (p.type === "finish") return p.data.reason
    return undefined
  }
  return undefined
}
