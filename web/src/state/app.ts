// Application state: who we are signed in as, which projects are open in
// this web UI, which screen the tab shows, and the open session tabs. The
// screen and selection live in the URL so a reload, a bookmark or a second
// tab land on the same place.

import { batch, createRoot, createSignal } from "solid-js"
import { createStore } from "solid-js/store"
import { api, ApiError, setUnauthorizedHandler } from "../api/client"
import type { Bootstrap, WorkspaceView } from "../api/types"
import { prefs, setPref, type SessionTab } from "./prefs"
import { onWorkspaceReplaced, WorkspaceStore } from "./workspace"

export type AuthState = "loading" | "signed-in" | "signed-out" | "unreachable"
export type Screen = "home" | "session" | "draft" | "settings"

interface AppData {
  auth: AuthState
  boot?: Bootstrap
  workspaceIDs: string[]
  screen: Screen
  current?: string
  session?: string
  /** The draft tab shown when screen is "draft". */
  draft?: string
  settingsSection: string
}

const stores = new Map<string, WorkspaceStore>()

const [app, setApp] = createRoot(() =>
  createStore<AppData>({ auth: "loading", workspaceIDs: [], screen: "home", settingsSection: "preferences" }),
)
export { app }

export const [projectDialog, setProjectDialog] = createRoot(() => createSignal(false))
export const [helpDialog, setHelpDialog] = createRoot(() => createSignal(false))

export function workspace(id?: string): WorkspaceStore | undefined {
  return id ? stores.get(id) : undefined
}

export function currentWorkspace(): WorkspaceStore | undefined {
  return workspace(app.current)
}

export function workspaces(): WorkspaceStore[] {
  return app.workspaceIDs.map((id) => stores.get(id)).filter((s): s is WorkspaceStore => !!s)
}

export function workspaceByPath(path: string): WorkspaceStore | undefined {
  return workspaces().find((s) => pathKey(s.state.view.path) === pathKey(path))
}

export function pathKey(p: string) {
  return p.replaceAll("\\", "/").replace(/\/+$/, "").toLowerCase()
}

function readURL(): { ws?: string; session?: string; draft?: string; settings: boolean } {
  const q = new URLSearchParams(location.search)
  return { ws: q.get("w") ?? undefined, session: q.get("s") ?? undefined, draft: q.get("draft") ?? undefined, settings: q.get("view") === "settings" }
}

function writeURL() {
  const q = new URLSearchParams()
  if (app.screen === "settings") q.set("view", "settings")
  if (app.screen === "session" && app.current) {
    q.set("w", app.current)
    if (app.session) q.set("s", app.session)
  }
  if (app.screen === "draft" && app.draft) q.set("draft", app.draft)
  const next = q.toString() ? `${location.pathname}?${q}` : location.pathname
  if (next !== location.pathname + location.search) history.replaceState(null, "", next)
}

function adopt(view: WorkspaceView): WorkspaceStore {
  let store = stores.get(view.id)
  if (!store) {
    store = new WorkspaceStore(view)
    stores.set(view.id, store)
    store.start()
  }
  if (!app.workspaceIDs.includes(view.id)) setApp("workspaceIDs", (l) => [...l, view.id])
  return store
}

export async function bootstrap() {
  setUnauthorizedHandler(() => setApp("auth", "signed-out"))
  try {
    const boot = await api.bootstrap()
    const wanted = readURL()
    batch(() => {
      setApp("boot", boot)
      setApp("auth", "signed-in")
      for (const w of boot.workspaces) adopt(w)
    })
    // Tabs remember projects by path; reopen the ones the gateway lost
    // (it restarted) so their sessions are reachable again.
    for (const tab of prefs().tabs) {
      if (!workspaceByPath(tab.path)) {
        try {
          adopt(await api.openWorkspace(tab.path))
        } catch {
          // A folder that is gone; its tab stays until closed.
        }
      }
    }
    if (wanted.settings) {
      openSettings()
    } else if (wanted.ws && wanted.session && workspace(wanted.ws)) {
      openSession(wanted.ws, wanted.session)
    } else if (wanted.draft && prefs().tabs.some((t) => t.draft === wanted.draft)) {
      openDraft(wanted.draft)
    } else {
      const first = workspaces()[0]
      if (first) setApp("current", first.id)
      goHome()
    }
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) setApp("auth", "signed-out")
    else setApp("auth", "unreachable")
  }
}

export async function openProject(path: string): Promise<WorkspaceStore> {
  const view = await api.openWorkspace(path)
  const store = adopt(view)
  setApp("current", view.id)
  return store
}

export async function closeProject(id: string) {
  const store = stores.get(id)
  await api.closeWorkspace(id)
  store?.stop()
  stores.delete(id)
  batch(() => {
    setApp("workspaceIDs", (l) => l.filter((x) => x !== id))
    if (store) setPref("tabs", prefs().tabs.filter((t) => pathKey(t.path) !== pathKey(store.state.view.path)))
    if (app.current === id) {
      setApp("current", app.workspaceIDs[0])
      if (app.screen === "session") goHome()
    }
  })
  writeURL()
}

export function goHome() {
  batch(() => {
    setApp("screen", "home")
    setApp("session", undefined)
    setApp("draft", undefined)
  })
  writeURL()
}

/** newDraft opens a new-session tab for a project, like Atlas Agent's "+". */
export function newDraft(path?: string) {
  const store = (path && workspaceByPath(path)) || workspace(app.current) || workspaces()[0]
  if (!store) {
    setProjectDialog(true)
    return
  }
  const id = crypto.randomUUID()
  setPref("tabs", [...prefs().tabs, { path: store.state.view.path, sid: "", draft: id }])
  openDraft(id)
}

/** openDraft shows an existing draft tab. */
export function openDraft(id: string) {
  const tab = prefs().tabs.find((t) => t.draft === id)
  if (!tab) return
  const store = workspaceByPath(tab.path)
  batch(() => {
    if (store) setApp("current", store.id)
    setApp("session", undefined)
    setApp("draft", id)
    setApp("screen", "draft")
  })
  writeURL()
}

/** setDraftProject points a draft tab at another project. */
export function setDraftProject(id: string, path: string) {
  setPref("tabs", prefs().tabs.map((t) => (t.draft === id ? { ...t, path } : t)))
  const store = workspaceByPath(path)
  if (store && app.draft === id) setApp("current", store.id)
}

/** promoteDraft turns a draft tab into the session it created, in place. */
export function promoteDraft(id: string, workspaceID: string, session: string) {
  const store = stores.get(workspaceID)
  if (!store) return
  setPref("tabs", prefs().tabs.map((t) => (t.draft === id ? { path: store.state.view.path, sid: session } : t)))
  openSession(workspaceID, session)
}

/** tabKey identifies a tab: its session, or its draft ID. */
export function tabKey(tab: SessionTab) {
  return tab.draft ? `draft:${tab.draft}` : tab.sid
}

export function openSettings(section?: string) {
  batch(() => {
    setApp("screen", "settings")
    if (section) setApp("settingsSection", section)
  })
  writeURL()
}

/** openSession shows a session and keeps a tab for it. */
// The session the reader stepped away from, for "/back". Cleared when used,
// so a second "/back" does nothing instead of bouncing between two sessions.
let previous: { workspaceID: string; session: string } | undefined

/** goBack returns to the session the reader stepped away from. It reports
 * whether there was one. */
export function goBack(): boolean {
  const target = previous
  previous = undefined
  if (!target || !stores.has(target.workspaceID)) return false
  openSession(target.workspaceID, target.session)
  return true
}

export function openSession(workspaceID: string, session: string) {
  const store = stores.get(workspaceID)
  if (!store) return
  if (app.screen === "session" && app.current && app.session && app.session !== session) previous = { workspaceID: app.current, session: app.session }
  batch(() => {
    setApp("current", workspaceID)
    setApp("session", session)
    setApp("draft", undefined)
    setApp("screen", "session")
  })
  const tab: SessionTab = { path: store.state.view.path, sid: session }
  if (!prefs().tabs.some((t) => !t.draft && t.sid === session)) setPref("tabs", [...prefs().tabs, tab])
  setPref("lastWorkspace", store.state.view.path)
  void store.openSession(session).catch(() => {
    if (app.session === session) closeTab(session)
  })
  writeURL()
}

/** closeTab removes a tab (by session ID or tab key) and moves to its
 * neighbour when it was the one shown. */
export function closeTab(key: string) {
  const tabs = prefs().tabs
  const i = tabs.findIndex((t) => tabKey(t) === key || (!t.draft && t.sid === key))
  if (i < 0) return
  const closing = tabs[i]
  const next = tabs.filter((_, j) => j !== i)
  setPref("tabs", next)
  const shown = closing.draft ? app.screen === "draft" && app.draft === closing.draft : app.screen === "session" && app.session === closing.sid
  if (!shown) return
  const neighbour = next[Math.min(i, next.length - 1)]
  if (!neighbour) return goHome()
  selectTab(neighbour)
}

/** selectTab shows a tab, whichever kind it is. */
export function selectTab(tab: SessionTab) {
  if (tab.draft) return openDraft(tab.draft)
  const store = workspaceByPath(tab.path)
  if (store) openSession(store.id, tab.sid)
  else goHome()
}

/** newSession creates a session in a project and opens it in a tab. */
export async function newSession(workspaceID?: string): Promise<string | undefined> {
  const store = workspace(workspaceID ?? app.current) ?? workspaces()[0]
  if (!store) {
    setProjectDialog(true)
    return undefined
  }
  const s = await api.createSession(store.id)
  openSession(store.id, s.id)
  return s.id
}

export function selectSession(session: string | undefined) {
  if (!app.current) return
  if (session) openSession(app.current, session)
  else goHome()
}

/** Keeps the selection pointing at a workspace whose ID the server changed. */
export function followReplacement(oldID: string, newID: string) {
  const store = stores.get(oldID)
  if (!store) return
  stores.delete(oldID)
  stores.set(newID, store)
  batch(() => {
    setApp("workspaceIDs", (l) => l.map((x) => (x === oldID ? newID : x)))
    if (app.current === oldID) setApp("current", newID)
  })
  writeURL()
}

onWorkspaceReplaced(followReplacement)

addEventListener("visibilitychange", () => {
  // A tab coming back from the background resumes at once instead of
  // waiting out the reconnect backoff.
  if (document.visibilityState === "visible") for (const s of stores.values()) if (s.state.stream !== "open") s.reconnect()
})
addEventListener("online", () => {
  for (const s of stores.values()) s.reconnect()
})
