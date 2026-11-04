// Live state of Atlas Dots.
//
// Each dot is opened as an Atlas workspace on first use and keeps one
// session. Messages, busy state and permission requests come from that
// workspace's event stream, exactly as in the main web client, so what a
// dot shows is what the Atlas server is really doing.

import { batch } from "solid-js"
import { createStore, produce, reconcile } from "solid-js/store"
import { api, ApiError, newRunID } from "../api/client"
import { openEventStream, type StreamControl } from "../api/events"
import type { Attachment, HubEvent, Message, PermissionMode, PermissionRequest, RunComplete, SessionStatus } from "../api/types"
import { addPermission, removeMessage, settlePermission, upsertMessage } from "../state/merge"
import { dotsApi, fullAccessActive, type Dot, type DotInput, type Shape } from "./api"
import { speak, stopSpeaking } from "./voice"

export interface Runtime {
  wsId: string
  sessionId: string
  messages: Message[]
  busy: boolean
  permissions: PermissionRequest[]
  error: string
  connected: boolean
  sending: boolean
}

interface State {
  phase: "loading" | "ready" | "signed-out" | "offline"
  dots: Dot[]
  shapes: Shape[]
  colors: string[]
  voiceConfigured: boolean
  current: string
  rt: Record<string, Runtime>
  opening: Record<string, boolean>
  listening: boolean
  typing: boolean
  toasts: { id: number; text: string; error?: boolean }[]
  prefs: { speak: boolean; lang: string; notify: boolean; sound: boolean; wake: boolean }
  /** mood is a short-lived feeling a dot shows after something happened. */
  mood: Record<string, { kind: Mood; until: number }>
}

export type Mood = "happy" | "worried"

const savedPrefs = (() => {
  try {
    return JSON.parse(localStorage.getItem("atlas-dots-prefs") ?? "{}")
  } catch {
    return {}
  }
})()

const [state, setState] = createStore<State>({
  phase: "loading",
  dots: [],
  shapes: [],
  colors: [],
  voiceConfigured: false,
  current: "",
  rt: {},
  opening: {},
  listening: false,
  typing: false,
  toasts: [],
  prefs: {
    speak: savedPrefs.speak ?? true,
    lang: savedPrefs.lang ?? navigator.language ?? "tr-TR",
    notify: savedPrefs.notify ?? false,
    sound: savedPrefs.sound ?? true,
    wake: savedPrefs.wake ?? false,
  },
  mood: {},
})
export { state }

const streams = new Map<string, StreamControl>()
const wsToDot = new Map<string, string>()
let toastSeq = 0

export function toast(text: string, error = false) {
  const id = ++toastSeq
  setState("toasts", (l) => [...l, { id, text, error }])
  setTimeout(() => setState("toasts", (l) => l.filter((t) => t.id !== id)), error ? 5200 : 2600)
}

export function setPref<K extends keyof State["prefs"]>(key: K, value: State["prefs"][K]) {
  setState("prefs", key, value)
  try {
    localStorage.setItem("atlas-dots-prefs", JSON.stringify(state.prefs))
  } catch {
    // Preferences are a convenience.
  }
}

export const currentDot = () => state.dots.find((d) => d.id === state.current)
export const runtimeOf = (id: string): Runtime | undefined => state.rt[id]

export function message(err: unknown): string {
  if (err instanceof ApiError) return err.message
  return (err as Error)?.message ?? String(err)
}

// ---------- Loading ----------

export async function load() {
  try {
    const boot = await api.bootstrap()
    dotsApi.setCsrf(boot.csrf)
    const list = await dotsApi.list()
    batch(() => {
      setState({ dots: list.dots, shapes: list.shapes, colors: list.colors, voiceConfigured: list.voice_configured, phase: "ready" })
      const last = localStorage.getItem("atlas-dots-current")
      const first = list.dots.find((d) => d.id === last) ?? list.dots[0]
      if (first) void select(first.id)
    })
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) setState("phase", "signed-out")
    else setState("phase", "offline")
  }
}

export async function refreshDots() {
  const list = await dotsApi.list()
  setState({ dots: list.dots, voiceConfigured: list.voice_configured })
}

function putDot(d: Dot) {
  setState("dots", (l) => {
    const i = l.findIndex((x) => x.id === d.id)
    if (i < 0) return [...l, d]
    const next = l.slice()
    next[i] = d
    return next
  })
}

// ---------- Selecting and opening ----------

export async function select(id: string) {
  stopSpeaking()
  setState("current", id)
  try {
    localStorage.setItem("atlas-dots-current", id)
  } catch {
    // Ignore.
  }
  await ensureOpen(id)
}

async function ensureOpen(id: string) {
  if (state.rt[id] || state.opening[id]) return
  const dot = state.dots.find((d) => d.id === id)
  if (!dot) return
  setState("opening", id, true)
  try {
    const ws = await api.openWorkspace(dot.workspace)
    let sessionId = dot.session_id
    if (sessionId) {
      try {
        await api.session(ws.id, sessionId)
      } catch {
        sessionId = ""
      }
    }
    if (!sessionId) {
      const s = await api.createSession(ws.id, dot.name)
      sessionId = s.id
      putDot(await dotsApi.update(dot.id, { session_id: sessionId }))
    }
    await applyMode(ws.id, dot)
    const [messages, pending, status] = await Promise.all([
      api.messages(ws.id, sessionId),
      api.pending(ws.id).catch(() => ({ permissions: [], questions: [] })),
      api.status(ws.id).catch(() => undefined),
    ])
    wsToDot.set(ws.id, id)
    setState("rt", id, {
      wsId: ws.id,
      sessionId,
      messages: messages ?? [],
      busy: !!status?.sessions?.[sessionId]?.busy,
      permissions: (pending.permissions ?? []).filter((p) => p.session_id === sessionId),
      error: "",
      connected: true,
      sending: false,
    })
    connect(id)
  } catch (err) {
    toast(`${dot.name} açılamadı: ${message(err)}`, true)
  } finally {
    setState("opening", id, false)
  }
}

/** applyMode maps the dot's full access to the workspace permission mode:
 * full access approves everything, otherwise file edits inside the dot's
 * own workspace go through and everything else asks. */
async function applyMode(wsId: string, dot: Dot) {
  const mode: PermissionMode = fullAccessActive(dot.full_access) ? "bypass" : "auto_accept_edits"
  try {
    await api.setPermissionMode(wsId, mode)
  } catch (err) {
    toast(`İzin modu ayarlanamadı: ${message(err)}`, true)
  }
}

function connect(id: string) {
  streams.get(id)?.close()
  const ctl = openEventStream(() => state.rt[id]?.wsId ?? "", {
    onHello: (_h, needsSnapshot) => {
      setState("rt", id, "connected", true)
      if (needsSnapshot) void resync(id)
    },
    onEvent: (ev) => apply(id, ev),
    onState: (s) => setState("rt", id, "connected", s === "open"),
  })
  streams.set(id, ctl)
}

async function resync(id: string) {
  const rt = state.rt[id]
  if (!rt) return
  try {
    const [messages, pending] = await Promise.all([api.messages(rt.wsId, rt.sessionId), api.pending(rt.wsId)])
    setState("rt", id, "messages", reconcile(messages ?? [], { key: "id" }))
    setState("rt", id, "permissions", (pending.permissions ?? []).filter((p) => p.session_id === rt.sessionId))
  } catch {
    // The next event will try again.
  }
}

function apply(id: string, ev: HubEvent) {
  const rt = state.rt[id]
  if (!rt) return
  const p = ev.payload
  switch (ev.type) {
    case "message": {
      const m = p.payload as Message
      if (m?.session_id !== rt.sessionId) return
      if (p.type === "deleted") setState("rt", id, "messages", (l) => removeMessage(l, m.id))
      else setState("rt", id, "messages", (l) => upsertMessage(l, m))
      return
    }
    case "session_status": {
      const st = p as SessionStatus
      if (st.session_id !== rt.sessionId) return
      setState("rt", id, "busy", st.busy)
      if (!st.busy) void resync(id)
      return
    }
    case "run_complete": {
      const rc = p.payload as RunComplete
      if (rc.session_id !== rt.sessionId) return
      const failed = !!rc.error && !rc.cancelled
      batch(() => {
        setState("rt", id, "busy", false)
        setState("rt", id, "error", failed ? rc.error! : "")
        if (!rc.cancelled) setMood(id, failed ? "worried" : "happy")
      })
      void resync(id)
      const dot = state.dots.find((d) => d.id === id)
      if (dot && !rc.cancelled) {
        alertOwner(dot.id, failed ? `${dot.name} takıldı` : `${dot.name} işi bitirdi`, failed ? rc.error! : (rc.text ?? ""), failed ? "error" : "done")
      }
      if (rc.text && dot && state.prefs.speak && state.current === id && !state.listening) {
        void speak(rc.text, dot.voice_id, state.voiceConfigured && !!dot.voice_id, state.prefs.lang)
      }
      return
    }
    case "agent_event": {
      const a = p.payload as { type: string; session_id?: string; error?: string }
      if (a.session_id === rt.sessionId && a.type === "error" && a.error) setState("rt", id, "error", a.error)
      return
    }
    case "permission_request": {
      const req = p.payload as PermissionRequest
      if (req.session_id !== rt.sessionId) return
      const known = rt.permissions.some((x) => x.id === req.id)
      setState("rt", id, "permissions", (l) => addPermission(l, req))
      const dot = state.dots.find((d) => d.id === id)
      if (dot && !known) alertOwner(dot.id, `${dot.name} onayını bekliyor`, req.description || req.tool_name, "ask")
      return
    }
    case "permission_notification":
      setState("rt", id, "permissions", (l) => settlePermission(l, p.payload))
      return
    case "workspace_replaced":
      wsToDot.delete(p.old_id)
      wsToDot.set(p.new_id, id)
      setState("rt", id, "wsId", p.new_id)
      return
  }
}

// ---------- Actions ----------

/** send gives the current dot a message, with any attached files. It
 * reports whether the dot took it. */
export async function send(text: string, attachments: Attachment[] = []): Promise<boolean> {
  const id = state.current
  const rt = state.rt[id]
  const body = text.trim()
  if (!rt || (!body && !attachments.length)) return false
  stopSpeaking()
  setState("rt", id, { sending: true, error: "" })
  try {
    if (rt.busy) await api.followUp(rt.wsId, rt.sessionId, body, attachments)
    else await api.prompt(rt.wsId, rt.sessionId, body, newRunID(), attachments)
    setState("rt", id, "busy", true)
    return true
  } catch (err) {
    setState("rt", id, "error", message(err))
    return false
  } finally {
    setState("rt", id, "sending", false)
  }
}

/** userText is the typed text of a user message. */
export function userText(m: Message): string {
  return m.parts
    .filter((p): p is Extract<Message["parts"][number], { type: "text" }> => p.type === "text")
    .map((p) => p.data.text)
    .join("\n")
}

/** retry sends the dot's last task again. */
export async function retryLast() {
  const rt = state.rt[state.current]
  if (!rt || rt.busy) return
  const last = rt.messages.findLast((m) => m.role === "user" && !m.is_summary_message)
  const text = last ? userText(last) : ""
  if (text) await send(text)
}

/**
 * editAndResend replaces a message the owner sent. The conversation is
 * forked just before that message, so what the dot did in answer to it and
 * afterwards is undone in its workspace, and the new text is sent there.
 * The old conversation is kept, never deleted.
 */
export async function editAndResend(messageID: string, text: string): Promise<boolean> {
  const id = state.current
  const rt = state.rt[id]
  const dot = currentDot()
  if (!rt || !dot || rt.busy || !text.trim()) return false
  const i = rt.messages.findIndex((m) => m.id === messageID)
  if (i < 0) return false
  try {
    let sid: string
    const prev = rt.messages[i - 1]
    if (prev) sid = (await api.rewind(rt.wsId, rt.sessionId, prev.id)).session.id
    else sid = (await api.createSession(rt.wsId, dot.name)).id
    putDot(await dotsApi.update(dot.id, { session_id: sid }))
    const messages = await api.messages(rt.wsId, sid)
    batch(() => {
      setState("rt", id, { sessionId: sid, messages: messages ?? [], permissions: [], error: "" })
    })
    return await send(text)
  } catch (err) {
    toast(`Mesaj düzenlenemedi: ${message(err)}`, true)
    return false
  }
}

/** newConversation starts the current dot on a fresh session. Its memory
 * and files stay; only the chat starts over. */
export async function newConversation() {
  const id = state.current
  const rt = state.rt[id]
  const dot = currentDot()
  if (!rt || !dot) return
  try {
    const s = await api.createSession(rt.wsId, dot.name)
    putDot(await dotsApi.update(dot.id, { session_id: s.id }))
    setState("rt", id, { sessionId: s.id, messages: [], permissions: [], busy: false, error: "" })
    toast("Yeni sohbet başladı")
  } catch (err) {
    toast(message(err), true)
  }
}

/** summarize compacts the current dot's conversation. */
export async function summarize() {
  const rt = state.rt[state.current]
  if (!rt) return
  try {
    await api.summarize(rt.wsId, rt.sessionId)
    toast("Sohbet özetleniyor")
  } catch (err) {
    toast(message(err), true)
  }
}

/** handOff gives another dot a task, with where it came from. */
export async function handOff(fromID: string, toID: string, text: string) {
  const from = state.dots.find((d) => d.id === fromID)
  const to = state.dots.find((d) => d.id === toID)
  if (!from || !to) return
  try {
    await dotsApi.startRun(to.id, `${from.name} bu işi sana devretti. Gerekeni yap, bitince kısaca ne yaptığını söyle.\n\n---\n${text.trim()}`.slice(0, 4000))
    toast(`${to.name} işi devraldı`)
  } catch (err) {
    toast(`${to.name} işi alamadı: ${message(err)}`, true)
  }
}

/** openAll opens every dot, so search can see all conversations. */
export async function openAll() {
  await Promise.all(state.dots.map((d) => ensureOpen(d.id)))
}

// ---------- Feelings and alerts ----------

export function setMood(id: string, kind: Mood, ms = 2600) {
  setState("mood", id, { kind, until: Date.now() + ms })
  setTimeout(() => {
    if ((state.mood[id]?.until ?? 0) <= Date.now()) setState("mood", produce((m) => void delete m[id]))
  }, ms + 50)
}

export function setTyping(on: boolean) {
  if (state.typing !== on) setState("typing", on)
}

let chimeCtx: AudioContext | undefined

/** chime plays a short, soft two-note sound for what happened. */
export function chime(kind: "done" | "ask" | "error") {
  try {
    chimeCtx ??= new AudioContext()
    const ctx = chimeCtx
    const notes = kind === "done" ? [659.25, 987.77] : kind === "ask" ? [783.99, 783.99] : [392, 311.13]
    notes.forEach((f, i) => {
      const t = ctx.currentTime + i * 0.13
      const o = ctx.createOscillator()
      const g = ctx.createGain()
      o.type = "sine"
      o.frequency.value = f
      g.gain.setValueAtTime(0, t)
      g.gain.linearRampToValueAtTime(0.08, t + 0.012)
      g.gain.exponentialRampToValueAtTime(0.0001, t + 0.42)
      o.connect(g).connect(ctx.destination)
      o.start(t)
      o.stop(t + 0.45)
    })
  } catch {
    // Sound is a convenience.
  }
}

/** alertOwner tells the owner about a dot when they are not looking at
 * it: a sound, and a system notification when the page is hidden. */
function alertOwner(id: string, title: string, body: string, kind: "done" | "ask" | "error") {
  const looking = state.current === id && document.visibilityState === "visible"
  if (looking && kind === "done") return
  if (state.prefs.sound) chime(kind)
  if (!state.prefs.notify || document.visibilityState === "visible" || !("Notification" in window) || Notification.permission !== "granted") return
  try {
    const n = new Notification(title, { body: body.slice(0, 180), tag: `dot-${id}-${kind}` })
    n.onclick = () => {
      window.focus()
      void select(id)
      n.close()
    }
  } catch {
    // Some browsers only allow notifications from a service worker.
  }
}

/** enableNotifications asks the browser for permission and turns
 * notifications on when it is given. */
export async function enableNotifications(on: boolean) {
  if (!on) return setPref("notify", false)
  if (!("Notification" in window)) {
    toast("Bu tarayıcı bildirim göstermiyor.", true)
    return
  }
  const p = Notification.permission === "granted" ? "granted" : await Notification.requestPermission()
  if (p !== "granted") {
    toast("Bildirim izni verilmedi. Tarayıcı ayarlarından açabilirsin.", true)
    return
  }
  setPref("notify", true)
}

export async function cancel() {
  const rt = state.rt[state.current]
  if (!rt) return
  stopSpeaking()
  try {
    await api.cancel(rt.wsId, rt.sessionId)
  } catch (err) {
    toast(message(err), true)
  }
}

export async function decide(perm: PermissionRequest, action: "allow" | "allow_session" | "deny") {
  const id = state.current
  const rt = state.rt[id]
  if (!rt) return
  setState("rt", id, "permissions", (l) => l.filter((p) => p.id !== perm.id))
  try {
    await api.decidePermission(rt.wsId, perm.id, action)
  } catch (err) {
    toast(message(err), true)
    void resync(id)
  }
}

export async function createDot(input: DotInput): Promise<Dot | undefined> {
  try {
    const d = await dotsApi.create(input)
    putDot(d)
    await select(d.id)
    return d
  } catch (err) {
    toast(message(err), true)
  }
}

export async function updateDot(id: string, input: DotInput): Promise<boolean> {
  try {
    putDot(await dotsApi.update(id, input))
    return true
  } catch (err) {
    toast(message(err), true)
    return false
  }
}

export async function deleteDot(id: string) {
  try {
    await dotsApi.remove(id)
    streams.get(id)?.close()
    streams.delete(id)
    const rt = state.rt[id]
    if (rt) {
      wsToDot.delete(rt.wsId)
      void api.closeWorkspace(rt.wsId).catch(() => {})
    }
    batch(() => {
      setState("dots", (l) => l.filter((d) => d.id !== id))
      setState(
        "rt",
        produce((r) => {
          delete r[id]
        }),
      )
      if (state.current === id) setState("current", state.dots[0]?.id ?? "")
    })
    if (state.current) await ensureOpen(state.current)
  } catch (err) {
    toast(message(err), true)
  }
}

export async function grantFullAccess(id: string, minutes: number, permanent: boolean, confirmName: string) {
  const d = await dotsApi.grantFullAccess(id, { minutes, permanent, confirm_name: confirmName, accepted: true })
  putDot(d)
  const rt = state.rt[id]
  if (rt) await applyMode(rt.wsId, d)
  return d
}

export async function revokeFullAccess(id: string) {
  try {
    const d = await dotsApi.revokeFullAccess(id)
    putDot(d)
    const rt = state.rt[id]
    if (rt) await applyMode(rt.wsId, d)
    toast("Tam yetki kapatıldı")
  } catch (err) {
    toast(message(err), true)
  }
}

/** expireLocal drops full access that ran out while the page was open. */
export function expireLocal() {
  for (const d of state.dots) {
    if (d.full_access?.granted_at && !fullAccessActive(d.full_access)) {
      void refreshDots().then(() => {
        const fresh = state.dots.find((x) => x.id === d.id)
        const rt = state.rt[d.id]
        if (fresh && rt) void applyMode(rt.wsId, fresh)
      })
      return
    }
  }
}

export function setListening(on: boolean) {
  setState("listening", on)
}
