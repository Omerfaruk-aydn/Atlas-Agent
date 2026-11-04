// Atlas Dots API. Workspace, session and event calls reuse the web
// client's own api module, so the wire contract stays in one place; this
// file adds only the /api/dots endpoints.

import { api, ApiError } from "../api/client"

export type Shape = "orb" | "pebble" | "bloom" | "hex" | "drop" | "cloud" | "star" | "shield"

export interface Personality {
  warmth: number
  humor: number
  formality: number
  proactivity: number
}

export interface Rules {
  always_ask: string[] | null
  auto_allow: string[] | null
}

export interface FullAccess {
  granted_at?: string
  until?: string
  permanent?: boolean
}

export interface AccessWindow {
  from: string
  to?: string
  planned?: string
  permanent?: boolean
  end?: "revoked" | "expired" | "replaced"
}

export interface Dot {
  access_log: AccessWindow[] | null
  skills: string[] | null
  id: string
  name: string
  role: string
  shape: Shape
  color: string
  voice_id: string
  voice_name: string
  system_prompt: string
  personality: Personality
  rules: Rules
  full_access: FullAccess
  session_id: string
  workspace: string
  created_at: string
  updated_at: string
}

export interface DotInput {
  name?: string
  role?: string
  shape?: Shape
  color?: string
  voice_id?: string
  voice_name?: string
  system_prompt?: string
  personality?: Personality
  rules?: { always_ask: string[]; auto_allow: string[] }
  skills?: string[]
  session_id?: string
}

export interface DotMemory {
  content: string
  used: number
  limit: number
}

export interface DotSkill {
  name: string
  description: string
  source: string
  enabled: boolean
}

export interface Spending {
  days: { day: string; usd: number }[]
  budget: Budget
  run_cost: number
  runs: number
}

export interface DotsList {
  dots: Dot[]
  shapes: Shape[]
  colors: string[]
  voice_configured: boolean
}

export interface Voice {
  id: string
  name: string
  category?: string
  description?: string
  gender?: string
  accent?: string
  preview_url?: string
}

export interface Trigger {
  id: string
  dot_id: string
  name: string
  kind: TriggerKind
  cron?: string
  timezone?: string
  prompt: string
  enabled: boolean
  token?: string
  path?: string
  url?: string
  every?: number
  checked_at?: string
  created_at: string
  last_fired_at?: string
  next_fire_at?: string
}

export type TriggerKind = "schedule" | "webhook" | "file" | "page"

export interface TriggerInput {
  name?: string
  kind?: TriggerKind
  cron?: string
  timezone?: string
  prompt?: string
  enabled?: boolean
  path?: string
  url?: string
  every?: number
}

export type RunStatus = "running" | "waiting_approval" | "complete" | "failed" | "cancelled" | "budget" | "skipped"

export interface Run {
  id: string
  dot_id: string
  trigger_id?: string
  trigger_name?: string
  source: "schedule" | "webhook" | "manual" | "watch"
  goal: string
  status: RunStatus
  error?: string
  summary?: string
  session_id?: string
  started_at: string
  ended_at?: string
  cost: number
}

export interface Budget {
  limit_usd: number
  spent_usd: number
  day: string
}

export interface ScreenState {
  running: boolean
  starting?: boolean
  taken_over: boolean
  url?: string
  title?: string
  width: number
  height: number
  isolation: string
  note: string
}

let csrf = ""

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  if (!csrf) csrf = (await api.bootstrap()).csrf
  const headers: Record<string, string> = { Accept: "application/json" }
  if (body !== undefined) headers["Content-Type"] = "application/json"
  if (method !== "GET") headers["X-Atlas-CSRF"] = csrf
  let rsp: Response
  try {
    rsp = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), credentials: "same-origin", cache: "no-store" })
  } catch {
    throw new ApiError(0, "Atlas'a ulaşılamıyor.")
  }
  if (!rsp.ok) {
    let message = rsp.statusText
    try {
      const data = await rsp.json()
      if (typeof data?.message === "string") message = data.message
    } catch {
      // Keep the status text.
    }
    throw new ApiError(rsp.status, message)
  }
  if (rsp.status === 204) return undefined as T
  return (await rsp.json()) as T
}

/** fullAccessActive reports whether a grant is in force now. */
export function fullAccessActive(f: FullAccess | undefined, now = Date.now()): boolean {
  if (!f?.granted_at) return false
  if (f.permanent) return true
  return !!f.until && Date.parse(f.until) > now
}

export const dotsApi = {
  setCsrf(token: string) {
    csrf = token
  },
  list: () => call<DotsList>("GET", "/api/dots"),
  get: (id: string) => call<Dot>("GET", `/api/dots/${encodeURIComponent(id)}`),
  create: (input: DotInput) => call<Dot>("POST", "/api/dots", input),
  update: (id: string, input: DotInput) => call<Dot>("PATCH", `/api/dots/${encodeURIComponent(id)}`, input),
  remove: (id: string) => call<void>("DELETE", `/api/dots/${encodeURIComponent(id)}`),
  grantFullAccess: (id: string, body: { minutes: number; permanent: boolean; confirm_name: string; accepted: boolean }) =>
    call<Dot>("POST", `/api/dots/${encodeURIComponent(id)}/full-access`, body),
  revokeFullAccess: (id: string) => call<Dot>("DELETE", `/api/dots/${encodeURIComponent(id)}/full-access`),
  triggers: (id: string) => call<Trigger[]>("GET", `/api/dots/${encodeURIComponent(id)}/triggers`),
  createTrigger: (id: string, input: TriggerInput) => call<Trigger>("POST", `/api/dots/${encodeURIComponent(id)}/triggers`, input),
  updateTrigger: (id: string, tid: string, input: TriggerInput) =>
    call<Trigger>("PATCH", `/api/dots/${encodeURIComponent(id)}/triggers/${encodeURIComponent(tid)}`, input),
  removeTrigger: (id: string, tid: string) => call<void>("DELETE", `/api/dots/${encodeURIComponent(id)}/triggers/${encodeURIComponent(tid)}`),
  runTrigger: (id: string, tid: string) => call<Run>("POST", `/api/dots/${encodeURIComponent(id)}/triggers/${encodeURIComponent(tid)}/run`),
  runs: (id: string) => call<Run[]>("GET", `/api/dots/${encodeURIComponent(id)}/runs`),
  startRun: (id: string, prompt: string) => call<Run>("POST", `/api/dots/${encodeURIComponent(id)}/runs`, { prompt }),
  cancelRun: (id: string, rid: string) => call<void>("POST", `/api/dots/${encodeURIComponent(id)}/runs/${encodeURIComponent(rid)}/cancel`),
  budget: (id: string) => call<Budget>("GET", `/api/dots/${encodeURIComponent(id)}/budget`),
  setBudget: (id: string, limit_usd: number) => call<Budget>("PUT", `/api/dots/${encodeURIComponent(id)}/budget`, { limit_usd }),
  memory: (id: string) => call<DotMemory>("GET", `/api/dots/${encodeURIComponent(id)}/memory`),
  setMemory: (id: string, content: string) => call<DotMemory>("PUT", `/api/dots/${encodeURIComponent(id)}/memory`, { content }),
  skills: (id: string) => call<DotSkill[]>("GET", `/api/dots/${encodeURIComponent(id)}/skills`),
  spending: (id: string, days = 14) => call<Spending>("GET", `/api/dots/${encodeURIComponent(id)}/spending?days=${days}`),
  screen: (id: string) => call<ScreenState>("GET", `/api/dots/${encodeURIComponent(id)}/screen`),
  startScreen: (id: string) => call<ScreenState>("POST", `/api/dots/${encodeURIComponent(id)}/screen`),
  stopScreen: (id: string) => call<ScreenState>("DELETE", `/api/dots/${encodeURIComponent(id)}/screen`),
  takeScreen: (id: string, on: boolean) => call<ScreenState>("POST", `/api/dots/${encodeURIComponent(id)}/screen/takeover`, { on }),
  screenInput: (id: string, body: { kind: string; x?: number; y?: number; text?: string; key?: string }) =>
    call<void>("POST", `/api/dots/${encodeURIComponent(id)}/screen/input`, body),
  voices: () => call<Voice[]>("GET", "/api/dots/voices"),
  /** speak returns MP3 audio of text in a voice. */
  async speak(voiceID: string, text: string, signal?: AbortSignal): Promise<Blob> {
    if (!csrf) csrf = (await api.bootstrap()).csrf
    const rsp = await fetch("/api/dots/speak", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Atlas-CSRF": csrf },
      body: JSON.stringify({ voice_id: voiceID, text }),
      credentials: "same-origin",
      signal,
    })
    if (!rsp.ok) {
      let message = rsp.statusText
      try {
        message = (await rsp.json()).message ?? message
      } catch {
        // Keep the status text.
      }
      throw new ApiError(rsp.status, message)
    }
    return rsp.blob()
  },
}
