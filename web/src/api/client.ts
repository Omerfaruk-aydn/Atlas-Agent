// The one place the web client talks to Atlas. Components never call
// fetch themselves: every endpoint, the CSRF token and retry policy live
// here, so the wire contract with internal/webui is in a single file.

import type {
  AgentHubEntry,
  Attachment,
  Bootstrap,
  Catalog,
  DirectoryListing,
  FileContent,
  HistoryFile,
  Job,
  JobOutput,
  Message,
  Pending,
  PermissionMode,
  Project,
  QuestionResponse,
  Session,
  Settings,
  SlashCommand,
  WorkspaceView,
} from "./types"

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message)
  }

  /** The request never reached the gateway, or the gateway could not
   * reach the Atlas server. */
  get unreachable() {
    return this.status === 0 || this.status === 502 || this.status === 503
  }
}

let csrf = ""

/** Signed-out state is reported once per page load to whoever listens. */
let onUnauthorized: (() => void) | undefined
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" }
  if (body !== undefined) headers["Content-Type"] = "application/json"
  if (method !== "GET") headers["X-Atlas-CSRF"] = csrf
  let rsp: Response
  try {
    rsp = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: "same-origin",
      cache: "no-store",
      signal,
    })
  } catch (err) {
    if ((err as Error)?.name === "AbortError") throw err
    throw new ApiError(0, "The Atlas web server cannot be reached.")
  }
  if (rsp.status === 401) onUnauthorized?.()
  if (!rsp.ok) {
    let message = rsp.statusText
    try {
      const data = await rsp.json()
      if (data && typeof data.message === "string") message = data.message
    } catch {
      // Keep the status text.
    }
    throw new ApiError(rsp.status, message)
  }
  if (rsp.status === 204) return undefined as T
  const text = await rsp.text()
  return (text ? JSON.parse(text) : undefined) as T
}

const ws = (id: string, rest = "") => `/api/workspaces/${encodeURIComponent(id)}${rest}`
const sess = (id: string, sid: string, rest = "") => ws(id, `/sessions/${encodeURIComponent(sid)}${rest}`)

export const api = {
  async bootstrap(): Promise<Bootstrap> {
    const b = await request<Bootstrap>("GET", "/api/bootstrap")
    csrf = b.csrf
    return b
  },
  signOut: () => request<void>("POST", "/api/auth/signout"),

  projects: () => request<Project[]>("GET", "/api/projects"),
  workspaces: () => request<WorkspaceView[]>("GET", "/api/workspaces"),
  openWorkspace: (path: string) => request<WorkspaceView>("POST", "/api/workspaces", { path }),
  workspace: (id: string) => request<WorkspaceView>("GET", ws(id)),
  closeWorkspace: (id: string) => request<void>("DELETE", ws(id)),
  status: (id: string) =>
    request<{ connected: boolean; seq: number; instance: string; sessions: Record<string, { busy: boolean; queued: number }> }>(
      "GET",
      ws(id, "/status"),
    ),

  sessions: (id: string, query?: string) =>
    request<Session[]>("GET", ws(id, query ? `/sessions?q=${encodeURIComponent(query)}` : "/sessions")),
  createSession: (id: string, title = "") => request<Session>("POST", ws(id, "/sessions"), { title }),
  session: (id: string, sid: string) => request<Session>("GET", sess(id, sid)),
  renameSession: (id: string, sid: string, title: string) => request<Session>("PATCH", sess(id, sid), { title }),
  deleteSession: (id: string, sid: string) => request<void>("DELETE", sess(id, sid)),
  messages: (id: string, sid: string, signal?: AbortSignal) =>
    request<Message[]>("GET", sess(id, sid, "/messages"), undefined, signal),
  history: (id: string, sid: string) => request<HistoryFile[] | null>("GET", sess(id, sid, "/history")),
  agents: (id: string, sid: string) => request<AgentHubEntry[] | null>("GET", sess(id, sid, "/agents")),
  queue: (id: string, sid: string) => request<string[] | null>("GET", sess(id, sid, "/queue")),
  clearQueue: (id: string, sid: string) => request<void>("DELETE", sess(id, sid, "/queue")),
  cancel: (id: string, sid: string) => request<void>("POST", sess(id, sid, "/cancel")),
  summarize: (id: string, sid: string) => request<void>("POST", sess(id, sid, "/summarize")),
  workflow: (id: string, sid: string) => request<Record<string, unknown>>("GET", sess(id, sid, "/workflow")),
  compaction: (id: string, body: { enabled?: boolean; percent?: number }) => request<void>("POST", ws(id, "/compaction"), body),
  goal: (id: string, sid: string) => request<{ active: boolean; goal?: string; used?: number; budget?: number }>("GET", sess(id, sid, "/goal")),
  setGoal: (id: string, sid: string, goal: string) => request<void>("POST", sess(id, sid, "/goal"), { goal }),
  userMessages: (id: string, sid: string) => request<Message[] | null>("GET", sess(id, sid, "/user-messages")),
  sessionFiles: (id: string, sid: string) => request<{ path: string }[] | null>("GET", sess(id, sid, "/files")),
  subagents: (id: string, sid: string) => request<Session[] | null>("GET", sess(id, sid, "/subagents")),
  rewindPreview: (id: string, sid: string, upTo: string) =>
    request<{ files_to_write: number; files_to_delete: number }>("POST", sess(id, sid, "/rewind/preview"), { up_to_message_id: upTo }),
  rewind: (id: string, sid: string, upTo: string) =>
    request<{ session: Session; files_written: number; files_deleted: number }>("POST", sess(id, sid, "/rewind"), { up_to_message_id: upTo }),
  shell: (id: string, sid: string, command: string) =>
    request<{ output: string; exit_code: number }>("POST", sess(id, sid, "/shell"), { command }),

  /** Sends a prompt. The run ID makes it idempotent, so a request whose
   * answer was lost is retried with the same ID and the server starts
   * the run once. */
  async prompt(id: string, sid: string, prompt: string, runID: string, attachments: Attachment[] = []) {
    const body = { prompt, run_id: runID, attachments }
    let lastError: unknown
    for (let attempt = 0; attempt < 4; attempt++) {
      try {
        return await request<{ run_id: string; duplicate: boolean }>("POST", sess(id, sid, "/prompt"), body)
      } catch (err) {
        lastError = err
        if (!(err instanceof ApiError) || !err.unreachable) throw err
        await new Promise((r) => setTimeout(r, 400 * 2 ** attempt))
      }
    }
    throw lastError
  },

  pending: (id: string) => request<Pending>("GET", ws(id, "/pending")),
  decidePermission: (id: string, permissionID: string, action: "allow" | "allow_session" | "deny") =>
    request<{ resolved: boolean }>("POST", ws(id, `/permissions/${encodeURIComponent(permissionID)}`), { action }),
  answerQuestion: (id: string, batchID: string, responses: QuestionResponse[]) =>
    request<{ resolved: boolean }>("POST", ws(id, `/questions/${encodeURIComponent(batchID)}/answer`), { responses }),
  cancelQuestion: (id: string, batchID: string) =>
    request<{ resolved: boolean }>("POST", ws(id, `/questions/${encodeURIComponent(batchID)}/cancel`)),

  commands: (id: string) => request<SlashCommand[]>("GET", ws(id, "/commands")),
  expandCommand: (id: string, command: string, args?: Record<string, string>) =>
    request<{ prompt: string }>("POST", ws(id, "/commands/expand"), { id: command, args }),

  models: (id: string) => request<Catalog>("GET", ws(id, "/models")),
  selectModel: (
    id: string,
    sel: { type: "large" | "small"; provider: string; model: string; reasoning_effort?: string; think?: boolean; scope: "global" | "workspace" },
  ) => request<Catalog>("POST", ws(id, "/models"), sel),
  settings: (id: string) => request<Settings>("GET", ws(id, "/settings")),
  setPermissionMode: (id: string, mode: PermissionMode) =>
    request<{ mode: PermissionMode }>("POST", ws(id, "/permission-mode"), { mode }),
  setSessionMode: (id: string, mode: string) =>
    request<{ session_mode: string; scope: string }>("POST", ws(id, "/session-mode"), { mode }),

  files: (id: string, path = "") => request<DirectoryListing>("GET", ws(id, `/files?path=${encodeURIComponent(path)}`)),
  file: (id: string, path: string, maxBytes?: number) =>
    request<FileContent>(
      "GET",
      ws(id, `/files/content?path=${encodeURIComponent(path)}${maxBytes ? `&max_bytes=${maxBytes}` : ""}`),
    ),

  jobs: (id: string) => request<Job[] | null>("GET", ws(id, "/jobs")),
  jobOutput: (id: string, jid: string) => request<JobOutput>("GET", ws(id, `/jobs/${encodeURIComponent(jid)}/output`)),
  killJob: (id: string, jid: string) => request<void>("DELETE", ws(id, `/jobs/${encodeURIComponent(jid)}`)),

  eventsURL: (id: string, after: number, instance: string) =>
    ws(id, `/events?after=${after}&instance=${encodeURIComponent(instance)}`),
}

/** newRunID returns a random UUID for a prompt. */
export function newRunID(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID()
  // randomUUID needs a secure context; loopback is one, but a dev proxy on
  // another host name may not be.
  const b = globalThis.crypto.getRandomValues(new Uint8Array(16))
  b[6] = (b[6] & 0x0f) | 0x40
  b[8] = (b[8] & 0x3f) | 0x80
  const h = [...b].map((x) => x.toString(16).padStart(2, "0")).join("")
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}
