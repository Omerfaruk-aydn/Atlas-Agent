// The conversation with a dot: real Atlas messages, tool activity as
// compact chips, and permission requests as cards the user answers in
// place.

import DOMPurify from "dompurify"
import { marked } from "marked"
import { createEffect, createMemo, createSignal, For, Match, on, onCleanup, Show, Switch } from "solid-js"
import type { Message, Part, PermissionRequest } from "../api/types"
import type { Dot } from "./api"
import { MiniAvatar } from "./Avatar"
import { setEditing } from "./Composer"
import { Icon } from "./icons"
import { decide, handOff, retryLast, state, toast, type Runtime } from "./store"
import { speak } from "./voice"

function copy(text: string) {
  void navigator.clipboard?.writeText(text).then(
    () => toast("Kopyalandı"),
    () => toast("Kopyalanamadı", true),
  )
}

/** HandOff sends a dot's answer to another dot as a new task. */
function HandOff(props: { from: Dot; text: string }) {
  const [open, setOpen] = createSignal(false)
  let root!: HTMLDivElement
  const away = (e: PointerEvent) => {
    if (!root.contains(e.target as Node)) setOpen(false)
  }
  document.addEventListener("pointerdown", away)
  onCleanup(() => document.removeEventListener("pointerdown", away))
  return (
    <div class="handoff" ref={root}>
      <button class="icon-btn sm" title="Başka bir dot'a devret" aria-expanded={open()} onClick={() => setOpen(!open())}>
        {Icon.share(14)}
      </button>
      <Show when={open()}>
        <div class="popover" role="menu">
          <div class="menu-head">Devret</div>
          <For each={state.dots.filter((d) => d.id !== props.from.id)}>
            {(d) => (
              <button
                class="menu-item"
                role="menuitem"
                onClick={() => {
                  setOpen(false)
                  void handOff(props.from.id, d.id, props.text)
                }}
              >
                <span class="menu-ico">
                  <MiniAvatar shape={d.shape} color={d.color} size={18} />
                </span>
                <b>{d.name}</b>
                <small>{d.role}</small>
              </button>
            )}
          </For>
        </div>
      </Show>
    </div>
  )
}

marked.setOptions({ gfm: true, breaks: true })

function md(text: string): string {
  return DOMPurify.sanitize(marked.parse(text, { async: false }) as string)
}

const labels: [test: (n: string) => boolean, running: string, done: string, failed: string][] = [
  [(n) => n === "bash" || n.includes("shell"), "Komut çalıştırıyor", "Komut çalıştırdı", "Komut tamamlanmadı"],
  [(n) => n === "view" || n === "read", "Dosya okuyor", "Dosya okudu", "Dosya okunamadı"],
  [(n) => n === "edit" || n === "multiedit" || n === "write", "Dosya yazıyor", "Dosya yazdı", "Dosya yazılamadı"],
  [(n) => n === "glob" || n === "grep" || n === "ls", "Dosyalarda arıyor", "Dosyalarda aradı", "Arama tamamlanmadı"],
  [(n) => n.includes("search"), "Web'de arıyor", "Web'de aradı", "Web araması tamamlanmadı"],
  [(n) => n.includes("fetch"), "Web sayfası okuyor", "Web sayfası okudu", "Web sayfası açılmadı"],
  [(n) => n.includes("browser"), "Tarayıcıda çalışıyor", "Tarayıcıda çalıştı", "Tarayıcı adımı tamamlanmadı"],
  [(n) => n.includes("computer") || n.includes("desktop"), "Bilgisayarda çalışıyor", "Bilgisayarda çalıştı", "Bilgisayar adımı tamamlanmadı"],
  [(n) => n === "agent" || n.includes("subagent"), "Yardımcı ajan çalıştırıyor", "Yardımcı ajan çalıştırdı", "Yardımcı ajan tamamlanmadı"],
  [(n) => n.includes("todo"), "Planı güncelliyor", "Planı güncelledi", "Plan güncellenemedi"],
  [(n) => n.includes("mail"), "E-postaya bakıyor", "E-postaya baktı", "E-posta açılamadı"],
  [(n) => n.includes("calendar"), "Takvime bakıyor", "Takvime baktı", "Takvim açılamadı"],
]

/** toolLabel names what a tool call is doing, or did, in plain words. */
export function toolLabel(name: string, done = false, failed = false): string {
  const n = name.toLowerCase()
  const hit = labels.find(([test]) => test(n))
  if (hit) return !done ? hit[1] : failed ? hit[3] : hit[2]
  const plain = name.replace(/_/g, " ")
  return failed ? `${plain} tamamlanmadı` : plain
}

/** toolDetail pulls a short hint out of a tool call's JSON input. */
export function toolDetail(input: string): string {
  try {
    const v = JSON.parse(input)
    const pick = v.command ?? v.url ?? v.query ?? v.file_path ?? v.path ?? v.pattern ?? v.description ?? v.prompt
    if (typeof pick === "string") return pick.length > 90 ? pick.slice(0, 89) + "…" : pick
  } catch {
    // Partial input while streaming.
  }
  return ""
}

interface TraceCall {
  id: string
  name: string
  detail: string
  input: string
  running: boolean
  error: boolean
}

interface Attached {
  name: string
  mime: string
  src?: string
}

type Item =
  | { kind: "user"; id: string; text: string; files: Attached[]; last: boolean }
  | { kind: "text"; id: string; text: string; streaming: boolean }
  | { kind: "trace"; id: string; calls: TraceCall[] }
  | { kind: "error"; id: string; text: string }

function itemsOf(messages: Message[], busy: boolean): Item[] {
  const results = new Map<string, { is_error: boolean }>()
  for (const m of messages) {
    if (m.role !== "tool") continue
    for (const p of m.parts) if (p.type === "tool_result") results.set(p.data.tool_call_id, p.data)
  }
  const out: Item[] = []
  const last = messages.findLast((m) => m.role === "assistant")
  const lastUser = messages.findLast((m) => m.role === "user" && !m.is_summary_message)
  for (const m of messages) {
    if (m.is_summary_message) continue
    if (m.role === "user") {
      const text = m.parts
        .filter((p): p is Extract<Part, { type: "text" }> => p.type === "text")
        .map((p) => p.data.text)
        .join("\n")
        .replace(/\n\n\(Bahsedilen dosyalar çalışma alanında: [^)]*\)$/, "")
      const files: Attached[] = []
      for (const p of m.parts) {
        if (p.type === "binary") {
          const name = p.data.Path.split(/[\\/]/).pop() || "dosya"
          const img = p.data.MIMEType.startsWith("image/") && p.data.Data
          files.push({ name, mime: p.data.MIMEType, src: img ? `data:${p.data.MIMEType};base64,${p.data.Data}` : undefined })
        } else if (p.type === "image_url") files.push({ name: "görsel", mime: "image/*", src: p.data.url })
      }
      if (text || files.length) out.push({ kind: "user", id: m.id, text, files, last: m === lastUser })
      continue
    }
    if (m.role !== "assistant") continue
    const finish = m.parts.find((p): p is Extract<Part, { type: "finish" }> => p.type === "finish")
    m.parts.forEach((p, i) => {
      if (p.type === "text" && p.data.text.trim()) {
        out.push({ kind: "text", id: `${m.id}:${i}`, text: p.data.text, streaming: busy && m === last && !finish })
      } else if (p.type === "tool_call") {
        const r = results.get(p.data.id)
        const call: TraceCall = { id: p.data.id, name: p.data.name, detail: toolDetail(p.data.input), input: p.data.input, running: !r && busy, error: !!r?.is_error }
        const prev = out.at(-1)
        if (prev?.kind === "trace") prev.calls.push(call)
        else out.push({ kind: "trace", id: p.data.id, calls: [call] })
      }
    })
    if (finish?.data.reason === "error" && finish.data.message) out.push({ kind: "error", id: `${m.id}:err`, text: finish.data.message })
  }
  return out
}

export function Thread(props: { dot: Dot; rt: Runtime }) {
  let scroller!: HTMLDivElement
  let stick = true
  const items = createMemo(() => itemsOf(props.rt.messages, props.rt.busy))

  const onScroll = () => {
    stick = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80
  }
  createEffect(
    on(
      () => [items().length, items().at(-1), props.rt.permissions.length],
      () => {
        if (stick) queueMicrotask(() => (scroller.scrollTop = scroller.scrollHeight))
      },
    ),
  )
  createEffect(on(() => props.dot.id, () => {
    stick = true
    queueMicrotask(() => (scroller.scrollTop = scroller.scrollHeight))
  }))

  return (
    <div class="thread" ref={scroller} onScroll={onScroll}>
      <div class="thread-inner">
        <Show when={items().length === 0 && !props.rt.busy}>
          <div class="event">{props.dot.name} hazır</div>
        </Show>
        <For each={items()}>
          {(it) => (
            <Switch>
              <Match when={it.kind === "user" && it}>
                {(u) => (
                  <div class="msg-user-wrap" data-mid={u().id}>
                    <div class="msg user">
                      <Show when={u().files.length}>
                        <div class="msg-files">
                          <For each={u().files}>
                            {(f) => (
                              <Show
                                when={f.src}
                                fallback={
                                  <span class="msg-file">
                                    {Icon.file(13)}
                                    {f.name}
                                  </span>
                                }
                              >
                                <img class="msg-img" src={f.src} alt={f.name} loading="lazy" />
                              </Show>
                            )}
                          </For>
                        </div>
                      </Show>
                      {u().text}
                    </div>
                    <Show when={!props.rt.busy && u().text}>
                      <div class="msg-meta user-meta">
                        <Show when={u().last}>
                          <button class="icon-btn sm" title="Yeniden dene" onClick={() => void retryLast()}>
                            {Icon.retry(13)}
                          </button>
                        </Show>
                        <button class="icon-btn sm" title="Düzenle ve buradan yeniden başlat" onClick={() => setEditing({ id: u().id, text: u().text })}>
                          {Icon.pen(13)}
                        </button>
                        <button class="icon-btn sm" title="Kopyala" onClick={() => copy(u().text)}>
                          {Icon.copy(13)}
                        </button>
                      </div>
                    </Show>
                  </div>
                )}
              </Match>
              <Match when={it.kind === "text" && it}>
                {(t) => (
                  <div class="msg dot" data-mid={t().id.split(":")[0]}>
                    <div class="md" innerHTML={md(t().text)} />
                    <Show when={!t().streaming}>
                      <div class="msg-meta">
                        <button
                          class="icon-btn sm"
                          title="Sesli oku"
                          onClick={() => speak(t().text, props.dot.voice_id, state.voiceConfigured && !!props.dot.voice_id, state.prefs.lang)}
                        >
                          {Icon.speaker(14)}
                        </button>
                        <button class="icon-btn sm" title="Kopyala" onClick={() => copy(t().text)}>
                          {Icon.copy(14)}
                        </button>
                        <Show when={state.dots.length > 1}>
                          <HandOff from={props.dot} text={t().text} />
                        </Show>
                      </div>
                    </Show>
                  </div>
                )}
              </Match>
              <Match when={it.kind === "trace" && it}>{(c) => <Trace calls={c().calls} />}</Match>
              <Match when={it.kind === "error" && it}>{(e) => <div class="err-line">{e().text}</div>}</Match>
            </Switch>
          )}
        </For>
        <Show when={props.rt.busy && !items().some((i) => (i.kind === "text" && i.streaming) || (i.kind === "trace" && i.calls.some((c) => c.running)))}>
          <div class="msg dot faint">
            <span class="caret" />
          </div>
        </Show>
        <For each={props.rt.permissions}>{(p) => <ApprovalCard dot={props.dot} perm={p} />}</For>
        <Show when={props.rt.error}>
          <div class="err-line">{props.rt.error}</div>
        </Show>
      </div>
    </div>
  )
}

function toolMark(name: string) {
  const n = name.toLowerCase()
  if (n.includes("search")) return Icon.globe(14)
  if (n.includes("fetch") || n.includes("browser") || n.includes("computer") || n.includes("desktop")) return Icon.eye(14)
  if (n === "grep" || n === "glob" || n === "ls") return Icon.search(14)
  if (n === "view" || n === "read") return Icon.file(14)
  if (n === "edit" || n === "write" || n.includes("multiedit")) return Icon.pen(14)
  if (n === "bash" || n.includes("shell")) return Icon.terminal(14)
  if (n.includes("agent")) return Icon.sparkle(14)
  if (n.includes("todo")) return Icon.activity(14)
  if (n.includes("mail") || n.includes("calendar")) return Icon.bell(14)
  return Icon.bolt(14)
}

function prettyInput(input: string): string {
  const raw = input.trim()
  if (!raw) return ""
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function Trace(props: { calls: TraceCall[] }) {
  const [open, setOpen] = createSignal<string | undefined>()
  return (
    <div class="trace">
      <For each={props.calls}>
        {(c) => (
          <>
            <button
              class={`trace-row ${c.running ? "run" : ""} ${c.error ? "err" : ""}`}
              aria-expanded={open() === c.id}
              onClick={() => setOpen(open() === c.id ? undefined : c.id)}
            >
              <span class="trace-mark">
                <Show when={c.running}>
                  <i class="trace-ring" />
                </Show>
                {toolMark(c.name)}
              </span>
              <span class="trace-copy">
                <b>{toolLabel(c.name, !c.running, c.error)}</b>
                <Show when={c.detail}>
                  <small>{c.detail}</small>
                </Show>
              </span>
            </button>
            <Show when={open() === c.id && prettyInput(c.input)}>
              <pre class="trace-open">{prettyInput(c.input)}</pre>
            </Show>
          </>
        )}
      </For>
    </div>
  )
}

function ApprovalCard(props: { dot: Dot; perm: PermissionRequest }) {
  const params = () => {
    const p = props.perm.params
    if (p == null) return ""
    const s = typeof p === "string" ? p : JSON.stringify(p, null, 2)
    return s.length > 1200 ? s.slice(0, 1200) + "\n…" : s
  }
  return (
    <div class="card approve-card msg">
      <div class="head">
        {Icon.clock(14)}
        {props.dot.name} onayını bekliyor
      </div>
      <h4>{toolLabel(props.perm.tool_name)}</h4>
      <p>{props.perm.description || props.perm.action}</p>
      <Show when={props.perm.path}>
        <p class="mono faint" style={{ "font-size": "12px", "margin-top": "4px" }}>
          {props.perm.path}
        </p>
      </Show>
      <Show when={params()}>
        <pre>{params()}</pre>
      </Show>
      <div class="actions">
        <button class="btn primary sm" onClick={() => decide(props.perm, "allow")}>
          İzin ver
        </button>
        <button class="btn sm" onClick={() => decide(props.perm, "allow_session")}>
          Bu oturumda hep izin ver
        </button>
        <button class="btn ghost sm" onClick={() => decide(props.perm, "deny")}>
          Reddet
        </button>
      </div>
    </div>
  )
}
