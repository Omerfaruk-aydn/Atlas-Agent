// Per-browser display preferences. These are conveniences only -- no
// credential, token or server state is ever stored in the browser.

import { createEffect, createRoot, createSignal } from "solid-js"

export type ThemePref = "system" | "light" | "dark"
export type LanguagePref = "system" | "en" | "tr"

const key = "atlas.web.prefs.v1"

/** An open session tab. Projects are remembered by path, because a
 * restarted server gives a project a new workspace ID. */
export interface SessionTab {
  path: string
  /** The session shown, or empty for a draft tab (a new session not yet sent). */
  sid: string
  /** Set on a draft tab: its local ID. */
  draft?: string
}

interface Prefs {
  theme: ThemePref
  language: LanguagePref
  sidebarWidth: number
  panelWidth: number
  panelOpen: boolean
  panelTab: "changes" | "files" | "jobs" | "agents"
  lastWorkspace?: string
  lastSession: Record<string, string>
  showReasoning: boolean
  /** Whether the to-do and queue strip shows above the composer. */
  pills: boolean
  tabs: SessionTab[]
  diffStyle: "unified" | "split"
}

const defaults: Prefs = {
  theme: "system",
  language: "system",
  sidebarWidth: 272,
  panelWidth: 440,
  panelOpen: false,
  panelTab: "changes",
  lastSession: {},
  showReasoning: true,
  pills: true,
  tabs: [],
  diffStyle: "unified",
}

function load(): Prefs {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return { ...defaults }
    return { ...defaults, ...JSON.parse(raw) }
  } catch {
    return { ...defaults }
  }
}

const [prefs, setPrefsSignal] = createSignal<Prefs>(load())

export { prefs }

export function setPref<K extends keyof Prefs>(k: K, v: Prefs[K]) {
  setPrefsSignal((p) => ({ ...p, [k]: v }))
}

createRoot(() => {
  createEffect(() => {
    const p = prefs()
    try {
      localStorage.setItem(key, JSON.stringify(p))
    } catch {
      // Storage may be unavailable (private mode); preferences then last
      // for this page only.
    }
  })
})

const media = typeof window !== "undefined" ? window.matchMedia("(prefers-color-scheme: dark)") : undefined
const [systemDark, setSystemDark] = createSignal(media?.matches ?? false)
media?.addEventListener("change", (e) => setSystemDark(e.matches))

export function resolvedTheme(): "light" | "dark" {
  const t = prefs().theme
  if (t === "system") return systemDark() ? "dark" : "light"
  return t
}
