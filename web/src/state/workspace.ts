// Live state of one workspace: sessions, messages of the sessions that
// are open, run states and pending questions and permission requests.
//
// Sync protocol: the event stream is opened first. Whenever the gateway
// says the client may have missed something (the first connection, a
// resync, a restarted gateway), incoming events are buffered while the
// snapshots load, then applied on top in order. Events carry whole
// objects, so the result is the newest state whether an event was older
// or newer than the snapshot.

import { batch } from "solid-js"
import { createStore, produce, reconcile } from "solid-js/store"
import { api, ApiError } from "../api/client"
import { openEventStream, type StreamControl, type StreamState } from "../api/events"
import type {
  HubEvent,
  Message,
  PermissionRequest,
  QuestionRequest,
  RunComplete,
  Session,
  SessionStatus,
  WorkspaceView,
} from "../api/types"
import {
  addPermission,
  addQuestion,
  lastFinish,
  removeMessage,
  removeSession,
  runState,
  settlePermission,
  settleQuestion,
  sortMessages,
  sortSessions,
  upsertMessage,
  upsertSession,
  type RunState,
} from "./merge"

export interface WorkspaceData {
  id: string
  view: WorkspaceView
  stream: StreamState
  upstream: "online" | "offline"
  syncing: boolean
  sessionsLoaded: boolean
  sessions: Session[]
  status: Record<string, { busy: boolean; queued: number }>
  messages: Record<string, Message[]>
  loading: Record<string, boolean>
  permissions: PermissionRequest[]
  questions: QuestionRequest[]
  lastRun: Record<string, RunComplete>
  starting: Record<string, string>
  errors: Record<string, string>
  notices: Record<string, string>
  /** Counters that panels watch to know when to refetch. */
  versions: { files: number; jobs: number; settings: number; history: Record<string, number> }
}

/** Called when the server re-created a workspace under a new ID. */
let replacedHook: ((oldID: string, newID: string) => void) | undefined
export function onWorkspaceReplaced(fn: (oldID: string, newID: string) => void) {
  replacedHook = fn
}

export class WorkspaceStore {
  readonly state: WorkspaceData
  private set: (...args: any[]) => void
  private stream?: StreamControl
  private buffer: HubEvent[] | undefined
  private syncToken = 0
  private openSessions = new Set<string>()

  constructor(view: WorkspaceView) {
    const [state, set] = createStore<WorkspaceData>({
      id: view.id,
      view,
      stream: "connecting",
      upstream: view.connected ? "online" : "offline",
      syncing: true,
      sessionsLoaded: false,
      sessions: [],
      status: {},
      messages: {},
      loading: {},
      permissions: [],
      questions: [],
      lastRun: {},
      starting: {},
      errors: {},
      notices: {},
      versions: { files: 0, jobs: 0, settings: 0, history: {} },
    })
    this.state = state
    this.set = set
  }

  get id() {
    return this.state.id
  }

  start() {
    this.stream = openEventStream(() => this.state.id, {
      onState: (s) => this.set("stream", s),
      onHello: (_hello, needsSnapshot) => {
        if (needsSnapshot) void this.resync()
      },
      onEvent: (ev) => {
        if (this.buffer) this.buffer.push(ev)
        else this.apply(ev)
      },
      onClosed: () => this.recover(),
    })
  }

  stop() {
    this.stream?.close()
  }

  /** recover finds out why the stream closed. A gateway that restarted no
   * longer knows the workspace (404): it is opened again by path, possibly
   * under a new ID. A lost sign-in (401) stops reconnecting; the app shows
   * the sign-in screen. */
  private async recover(): Promise<boolean> {
    try {
      await api.workspace(this.state.id)
      return true
    } catch (err) {
      if (!(err instanceof ApiError)) return true
      if (err.status === 401) return false
      if (err.status !== 404) return true
      try {
        const view = await api.openWorkspace(this.state.view.path)
        const old = this.state.id
        if (view.id !== old) {
          this.set("id", view.id)
          this.set("view", view)
          replacedHook?.(old, view.id)
        }
      } catch {
        // Try again on the next reconnect.
      }
      return true
    }
  }

  reconnect() {
    this.stream?.reconnect()
  }

  /** Reloads every snapshot while buffering live events. */
  async resync() {
    const token = ++this.syncToken
    this.buffer = []
    this.set("syncing", true)
    try {
      const [view, sessions, status, pending] = await Promise.all([
        api.workspace(this.state.id),
        api.sessions(this.state.id),
        api.status(this.state.id),
        api.pending(this.state.id),
      ])
      const messages: Record<string, Message[]> = {}
      await Promise.all(
        [...this.openSessions].map(async (sid) => {
          try {
            messages[sid] = sortMessages(await api.messages(this.state.id, sid))
          } catch (err) {
            if (!(err instanceof ApiError && err.status === 404)) throw err
          }
        }),
      )
      if (token !== this.syncToken) return
      batch(() => {
        this.set("view", view)
        this.set("upstream", view.connected ? "online" : "offline")
        this.set("sessions", reconcile(sortSessions(sessions ?? []), { key: "id" }))
        const st: WorkspaceData["status"] = {}
        for (const s of sessions ?? []) st[s.id] = { busy: s.is_busy, queued: 0 }
        for (const [sid, v] of Object.entries(status.sessions ?? {})) st[sid] = v
        this.set("status", reconcile(st))
        for (const [sid, list] of Object.entries(messages)) this.set("messages", sid, reconcile(list, { key: "id" }))
        this.set("permissions", reconcile(pending.permissions ?? [], { key: "id" }))
        this.set("questions", reconcile(pending.questions ?? [], { key: "id" }))
        this.set("sessionsLoaded", true)
        const buffered = this.buffer ?? []
        this.buffer = undefined
        for (const ev of buffered) this.apply(ev)
        this.set("syncing", false)
        this.bumpAll()
      })
    } catch {
      if (token !== this.syncToken) return
      // Keep buffering; the stream's next hello or a later retry reloads.
      const buffered = this.buffer ?? []
      this.buffer = undefined
      batch(() => {
        for (const ev of buffered) this.apply(ev)
        this.set("syncing", false)
      })
      setTimeout(() => {
        if (token === this.syncToken) void this.resync()
      }, 2000)
    }
  }

  private bumpAll() {
    this.set(
      "versions",
      produce((v: WorkspaceData["versions"]) => {
        v.files++
        v.jobs++
        v.settings++
        for (const k of Object.keys(v.history)) v.history[k]++
      }),
    )
  }

  /** Loads a session's messages and keeps them live from now on. */
  async openSession(sid: string) {
    this.openSessions.add(sid)
    if (this.state.messages[sid] || this.state.loading[sid]) return
    this.set("loading", sid, true)
    try {
      const list = sortMessages(await api.messages(this.state.id, sid))
      // Events that arrived while loading already sit in the store; merge
      // them over the snapshot so neither side loses anything.
      const live = (this.state.messages as Record<string, Message[] | undefined>)[sid] ?? []
      let merged = list
      for (const m of live) merged = upsertMessage(merged, m)
      this.set("messages", sid, merged)
    } finally {
      this.set("loading", sid, false)
    }
  }

  /** Reloads a session's messages from the server, the way the terminal's
   * "refresh session" resyncs a chat that looks stuck. */
  /** Tells everything that shows server settings to read them again. */
  settingsChanged() {
    this.set("versions", "settings", (n: number) => n + 1)
  }

  async reloadMessages(sid: string) {
    const list = sortMessages(await api.messages(this.state.id, sid))
    this.set("messages", sid, reconcile(list, { key: "id" }))
  }

  apply(ev: HubEvent) {
    const p = ev.payload
    switch (ev.type) {
      case "message": {
        const m = p.payload as Message
        if (!m?.session_id) return
        if (!this.state.messages[m.session_id] && !this.openSessions.has(m.session_id)) {
          this.touch(m.session_id, m.updated_at)
          return
        }
        if (p.type === "deleted") this.set("messages", m.session_id, (l: Message[] = []) => removeMessage(l, m.id))
        else this.set("messages", m.session_id, (l: Message[] = []) => upsertMessage(l, m))
        if (m.role === "user" && p.type === "created") this.set("errors", m.session_id, undefined!)
        return
      }
      case "session": {
        const s = p.payload as Session
        if (p.type === "deleted") this.set("sessions", (l: Session[]) => removeSession(l, s.id))
        else this.set("sessions", (l: Session[]) => upsertSession(l, s))
        return
      }
      case "session_status": {
        const st = p as SessionStatus
        this.set("status", st.session_id, { busy: st.busy, queued: st.queued })
        if (st.busy) {
          this.set("starting", st.session_id, undefined!)
          this.set("lastRun", st.session_id, undefined!)
        }
        return
      }
      case "run_complete": {
        const rc = p.payload as RunComplete
        this.set("lastRun", rc.session_id, rc)
        if (!rc.run_id || this.state.starting[rc.session_id] === rc.run_id) this.set("starting", rc.session_id, undefined!)
        if (rc.error) this.set("errors", rc.session_id, rc.error)
        // A run that ended may have withdrawn a prompt nobody answered.
        void this.refreshPending()
        this.bumpHistory(rc.session_id)
        return
      }
      case "agent_event": {
        const a = p.payload as { type: string; session_id?: string; error?: string }
        if (!a.session_id) return
        if (a.type === "error" && a.error) this.set("errors", a.session_id, a.error)
        else if (a.error) this.set("notices", a.session_id, a.error)
        return
      }
      case "permission_request":
        this.set("permissions", (l: PermissionRequest[]) => addPermission(l, p.payload))
        return
      case "permission_notification":
        this.set("permissions", (l: PermissionRequest[]) => settlePermission(l, p.payload))
        return
      case "question_batch_request":
        this.set("questions", (l: QuestionRequest[]) => addQuestion(l, p.payload))
        return
      case "question_batch_notification":
        this.set("questions", (l: QuestionRequest[]) => settleQuestion(l, p.payload.batch_id))
        return
      case "connection":
        this.set("upstream", p.state === "online" ? "online" : "offline")
        return
      case "workspace_replaced":
        this.set("id", p.new_id)
        this.set("view", "id", p.new_id)
        replacedHook?.(p.old_id, p.new_id)
        return
      case "file":
        if (p.payload?.session_id) this.bumpHistory(p.payload.session_id)
        this.set("versions", "files", (n: number) => n + 1)
        return
      case "job_event":
        this.set("versions", "jobs", (n: number) => n + 1)
        return
      case "config_changed":
      case "mcp_event":
      case "skills_event":
      case "lsp_event":
        this.set("versions", "settings", (n: number) => n + 1)
        return
    }
  }

  private touch(sid: string, at: number) {
    const s = this.state.sessions.find((x) => x.id === sid)
    if (s && at > s.updated_at) this.set("sessions", (l: Session[]) => upsertSession(l, { ...s, updated_at: at }))
  }

  private bumpHistory(sid: string) {
    this.set("versions", "history", sid, (n: number | undefined) => (n ?? 0) + 1)
  }

  async refreshPending() {
    try {
      const pending = await api.pending(this.state.id)
      batch(() => {
        this.set("permissions", reconcile(pending.permissions ?? [], { key: "id" }))
        this.set("questions", reconcile(pending.questions ?? [], { key: "id" }))
      })
    } catch {
      // The next resync will catch up.
    }
  }

  markStarting(sid: string, runID: string) {
    batch(() => {
      this.set("starting", sid, runID)
      this.set("lastRun", sid, undefined!)
      this.set("errors", sid, undefined!)
    })
  }

  clearStarting(sid: string) {
    this.set("starting", sid, undefined!)
  }

  runState(sid: string): RunState {
    const s = this.state
    return runState({
      busy: !!s.status[sid]?.busy,
      starting: !!s.starting[sid],
      hasQuestion: s.questions.some((q) => q.session_id === sid),
      hasPermission: s.permissions.some((p) => p.session_id === sid),
      lastRun: s.lastRun[sid],
      lastFinish: lastFinish(s.messages[sid] ?? []),
      error: s.errors[sid],
    })
  }
}
