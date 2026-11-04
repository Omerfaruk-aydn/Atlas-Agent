import { createEffect, createMemo, createSignal, For, Match, on, onCleanup, onMount, Show, Switch } from "solid-js"
import { fullAccessActive, type Dot } from "./api"
import { modelPulse, panelOpenPulse, panelTogglePulse, searchPulse, settingsPane, settingsPulse, type SettingsPane } from "./commands"
import { Activity } from "./Activity"
import { inkFor, LiveAvatar, MiniAvatar, type AvatarState } from "./Avatar"
import { Composer } from "./Composer"
import { CreateDot, templates, type Template } from "./CreateDot"
import { DotSettings, FullAccessLeft } from "./DotSettings"
import { Icon } from "./icons"
import { ModelSheet } from "./ModelSheet"
import { Search } from "./Search"
import { currentDot, expireLocal, load, revokeFullAccess, select, setPref, state, type Runtime } from "./store"
import { Thread, toolLabel } from "./Thread"
import { canListen, fold, inputLevel, outputLevel, speaking, startWake, stopSpeaking } from "./voice"

/** accentFor keeps the dot's color as the accent unless it is too pale
 * or too dark to read as one. */
function accentFor(color: string): string {
  const n = parseInt(color.slice(1), 16)
  const l = (0.2126 * ((n >> 16) & 255) + 0.7152 * ((n >> 8) & 255) + 0.0722 * (n & 255)) / 255
  return l > 0.8 || l < 0.12 ? "var(--text)" : color
}

function runningTool(rt: Runtime | undefined): string {
  if (!rt?.busy) return ""
  const done = new Set<string>()
  for (const m of rt.messages) for (const p of m.parts) if (p.type === "tool_result") done.add(p.data.tool_call_id)
  for (let i = rt.messages.length - 1; i >= 0; i--) {
    const m = rt.messages[i]
    if (m.role !== "assistant") continue
    for (let j = m.parts.length - 1; j >= 0; j--) {
      const p = m.parts[j]
      if (p.type === "tool_call" && !done.has(p.data.id)) return p.data.name
    }
    break
  }
  return ""
}

function avatarState(rt: Runtime | undefined): AvatarState {
  if (state.listening) return "listening"
  if (speaking()) return "speaking"
  if (!rt) return "sleeping"
  if (rt.permissions.length) return "waiting"
  if (rt.busy) return runningTool(rt) ? "working" : "thinking"
  return "idle"
}

function statusText(d: Dot, rt: Runtime | undefined): string {
  if (state.opening[d.id]) return "Açılıyor…"
  if (rt?.permissions.length) return "Onayını bekliyor"
  if (rt?.busy) {
    const tool = runningTool(rt)
    return tool ? toolLabel(tool) : "Düşünüyor"
  }
  if (fullAccessActive(d.full_access)) return "Tam yetkili"
  return d.role || "Hazır"
}

export function App() {
  onMount(() => void load())
  const t = setInterval(expireLocal, 30_000)
  onCleanup(() => clearInterval(t))

  return (
    <Switch>
      <Match when={state.phase === "loading"}>
        <div class="center-fill">
          <span class="spinner" />
        </div>
      </Match>
      <Match when={state.phase === "signed-out"}>
        <Gate title="Oturum açılmamış" body="Atlas Dots'u açmak için terminalde şu komutu çalıştır; açılan bağlantı bu tarayıcıda oturum açar.">
          <code class="mono" style={{ background: "var(--panel-2)", padding: "8px 12px", "border-radius": "8px" }}>atlas-agent dots</code>
        </Gate>
      </Match>
      <Match when={state.phase === "offline"}>
        <Gate title="Atlas'a ulaşılamıyor" body="Atlas web sunucusu çalışmıyor ya da yeniden başladı.">
          <button class="btn primary" onClick={() => location.reload()}>
            Tekrar dene
          </button>
        </Gate>
      </Match>
      <Match when={state.phase === "ready"}>
        <Shell />
      </Match>
    </Switch>
  )
}

function Gate(props: { title: string; body: string; children: any }) {
  return (
    <div class="center-fill" style={{ color: "var(--text)" }}>
      <div class="card" style={{ "max-width": "420px", display: "grid", gap: "12px", padding: "24px" }}>
        <MiniAvatar shape="orb" color="#1c1c1f" size={36} sleeping />
        <b style={{ "font-size": "16px" }}>{props.title}</b>
        <p class="muted">{props.body}</p>
        {props.children}
      </div>
    </div>
  )
}

function Shell() {
  const [creating, setCreating] = createSignal<{ template?: Template } | undefined>()
  const [settings, setSettings] = createSignal<SettingsPane | undefined>()
  const [panel, setPanel] = createSignal(window.innerWidth > 1180)
  const [railOpen, setRailOpen] = createSignal(false)
  const [searching, setSearching] = createSignal(false)
  createEffect(on(panelOpenPulse, (n) => n && setPanel(true), { defer: true }))
  createEffect(on(panelTogglePulse, (n) => n && setPanel((open) => !open), { defer: true }))
  createEffect(on(settingsPulse, (n) => n && setSettings(settingsPane()), { defer: true }))
  createEffect(on(searchPulse, (n) => n && setSearching(true), { defer: true }))
  const [modelOpen, setModelOpen] = createSignal(false)
  createEffect(on(modelPulse, (n) => n && setModelOpen(true), { defer: true }))
  const [theme, setTheme] = createSignal<string>(localStorage.getItem("atlas-dots-theme") ?? "")
  const applyTheme = (v: string) => {
    setTheme(v)
    if (v) document.documentElement.dataset.theme = v
    else delete document.documentElement.dataset.theme
    try {
      localStorage.setItem("atlas-dots-theme", v)
    } catch {
      // Ignore.
    }
  }
  applyTheme(theme())

  const dot = () => currentDot()
  const rt = () => (dot() ? state.rt[dot()!.id] : undefined)
  const av = createMemo(() => avatarState(rt()))
  const level = () => (state.listening ? inputLevel() : outputLevel())
  const mood = () => {
    const m = dot() ? state.mood[dot()!.id] : undefined
    return m && m.until > Date.now() ? m.kind : undefined
  }

  // Ctrl+K (or Cmd+K) opens search from anywhere.
  const keys = (e: KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
      e.preventDefault()
      setSearching((v) => !v)
    }
  }
  document.addEventListener("keydown", keys)
  onCleanup(() => document.removeEventListener("keydown", keys))

  // The wake word listens while the page is open and no one is talking.
  createEffect(() => {
    if (!state.prefs.wake || state.listening || !canListen() || !state.dots.length) return
    const stop = startWake(
      state.prefs.lang,
      () => state.dots.map((d) => d.name),
      (name) => {
        const d = state.dots.find((x) => fold(x.name) === fold(name))
        void (async () => {
          if (d && d.id !== state.current) await select(d.id)
          window.dispatchEvent(new Event("atlas-dots-wake"))
        })()
      },
    )
    onCleanup(stop)
  })

  const stageLine = () => {
    switch (av()) {
      case "listening":
        return "dinliyor"
      case "speaking":
        return "konuşuyor"
      case "waiting":
        return "onayını bekliyor"
      case "working":
        return toolLabel(runningTool(rt())).toLocaleLowerCase("tr")
      case "thinking":
        return "düşünüyor"
      case "sleeping":
        return state.opening[dot()?.id ?? ""] ? "uyanıyor" : ""
      default:
        return mood() === "happy" ? "bitirdi" : mood() === "worried" ? "bir sorun çıktı" : ""
    }
  }

  return (
    <div class="shell" style={{ "--accent": dot() ? accentFor(dot()!.color) : "var(--text)", "--accent-ink": dot() ? inkFor(dot()!.color) : "var(--bg)" }}>
      <aside class={`rail ${railOpen() ? "open" : ""}`}>
        <div class="rail-top">
          <span class="wordmark">
            <i />
            atlas dots
          </span>
          <span class="grow" />
          <button class="icon-btn sm" title="Ara (Ctrl+K)" onClick={() => setSearching(true)}>
            {Icon.search(15)}
          </button>
          <button class="icon-btn sm" title="Yeni dot" onClick={() => setCreating({})}>
            {Icon.plus(15)}
          </button>
        </div>
        <nav class="dot-list" aria-label="Dotlar">
          <For each={state.dots}>
            {(d) => {
              const r = () => state.rt[d.id]
              return (
                <button
                  class="dot-row"
                  aria-current={state.current === d.id}
                  onClick={() => {
                    setRailOpen(false)
                    void select(d.id)
                  }}
                >
                  <MiniAvatar shape={d.shape} color={d.color} size={34} sleeping={!r()} />
                  <span style={{ "min-width": 0 }}>
                    <b>{d.name}</b>
                    <small>{statusText(d, r())}</small>
                  </span>
                  <span
                    class={`status-dot ${r()?.permissions.length ? "waiting" : r()?.busy ? "busy" : fullAccessActive(d.full_access) ? "full" : ""}`}
                    style={{ visibility: r() || fullAccessActive(d.full_access) ? "visible" : "hidden" }}
                  />
                </button>
              )
            }}
          </For>
        </nav>
        <div class="rail-foot">
          <button class="btn ghost block" onClick={() => setCreating({})}>
            {Icon.plus(15)} Yeni dot
          </button>
          <button class="btn ghost block" onClick={() => applyTheme(theme() === "dark" ? "light" : theme() === "light" ? "" : "dark")}>
            {Icon.sparkle(15)} Tema: {theme() === "dark" ? "Koyu" : theme() === "light" ? "Açık" : "Sistem"}
          </button>
        </div>
      </aside>

      <main class="main">
        <Show when={dot()} fallback={<Welcome onCreate={(t) => setCreating({ template: t })} />}>
          {(d) => (
            <>
              <header class="topbar">
                <button class="icon-btn only-mobile" onClick={() => setRailOpen(!railOpen())} aria-label="Dotlar">
                  {Icon.menu()}
                </button>
                <h1>{d().name}</h1>
                <Show when={d().role}>
                  <span class="faint">{d().role}</span>
                </Show>
                <span class={`pill ${rt()?.permissions.length ? "waiting" : rt()?.busy ? "busy" : ""}`}>
                  <i />
                  {rt()?.busy || rt()?.permissions.length || state.opening[d().id] ? statusText(d(), rt()) : rt() ? "Hazır" : "Uykuda"}
                </span>
                <span class="grow" />
                <Show when={fullAccessActive(d().full_access)}>
                  <span class="pill full" title="Onaysız çalışıyor">
                    <FullAccessLeft dot={d()} />
                    <button onClick={() => revokeFullAccess(d().id)} aria-label="Tam yetkiyi kapat">
                      {Icon.close(11)}
                    </button>
                  </span>
                </Show>
                <button
                  class="icon-btn"
                  title={state.prefs.speak ? "Sesli cevapları kapat" : "Sesli cevapları aç"}
                  onClick={() => {
                    if (state.prefs.speak) stopSpeaking()
                    setPref("speak", !state.prefs.speak)
                  }}
                >
                  {state.prefs.speak ? Icon.speaker() : Icon.mute()}
                </button>
                <button class="icon-btn" title="İzinler" onClick={() => setSettings("permissions")}>
                  {Icon.shield()}
                </button>
                <button class="icon-btn" title="Ayarlar" onClick={() => setSettings("general")}>
                  {Icon.gear()}
                </button>
                <button class={`icon-btn ${panel() ? "" : ""}`} title="Etkinlik paneli" aria-pressed={panel()} onClick={() => setPanel(!panel())}>
                  {Icon.panel()}
                </button>
              </header>
              <Show
                when={rt()}
                fallback={
                  <div class="center-fill">
                    <span class="spinner" />
                  </div>
                }
              >
                {(r) => (
                  <>
                    <div class="converse">
                      <aside class="presence" aria-live="polite">
                        <LiveAvatar shape={d().shape} color={d().color} state={av()} level={level} mood={mood} typing={() => state.typing} size={76} />
                        <div class="stage-line">{stageLine()}</div>
                      </aside>
                      <Thread dot={d()} rt={r()} />
                    </div>
                    <div class="foot-dock">
                      <Composer dot={d()} rt={r()} />
                    </div>
                  </>
                )}
              </Show>
            </>
          )}
        </Show>
      </main>

      <Show when={panel() && dot() && rt()}>
        <Activity dot={dot()!} rt={rt()!} onClose={() => setPanel(false)} />
      </Show>

      <Show when={modelOpen() && rt()?.wsId}>
        <ModelSheet wsId={rt()!.wsId} onClose={() => setModelOpen(false)} />
      </Show>
      <Show when={searching()}>
        <Search onClose={() => setSearching(false)} />
      </Show>
      <Show when={creating()}>{(c) => <CreateDot template={c().template} onClose={() => setCreating(undefined)} />}</Show>
      <Show when={settings() && dot()}>
        <DotSettings dot={dot()!} initial={settings()} onClose={() => setSettings(undefined)} />
      </Show>
      <div class="toast-region" aria-live="polite">
        <For each={state.toasts}>{(t) => <div class={`toast ${t.error ? "err" : ""}`}>{t.text}</div>}</For>
      </div>
    </div>
  )
}

function Welcome(props: { onCreate: (t?: Template) => void }) {
  return (
    <div class="welcome">
      <div>
        <div style={{ display: "grid", "justify-items": "center" }}>
          <LiveAvatar shape="orb" color="#f2762e" state="idle" size={120} />
        </div>
        <h2>İlk dot'unu oluştur</h2>
        <p>
          Dot, senin için 7/24 çalışan kişisel bir ajan. Kendi adı, sesi, kişiliği ve çalışma alanı olur; işi bitirene kadar uğraşır, riskli adımlarda sana sorar.
        </p>
        <button class="btn primary" onClick={() => props.onCreate()}>
          {Icon.plus(15)} Yeni dot
        </button>
        <div class="templates">
          <For each={templates}>
            {(t) => (
              <button class="template" onClick={() => props.onCreate(t)}>
                <MiniAvatar shape={t.shape} color={t.color} size={30} />
                <span>
                  <b>{t.title}</b>
                  <br />
                  <small>{t.blurb}</small>
                </span>
              </button>
            )}
          </For>
        </div>
      </div>
    </div>
  )
}
