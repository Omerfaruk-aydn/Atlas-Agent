// Search across every dot: names and roles, and everything said in their
// conversations. Opening it opens all dots, so their messages are loaded.

import { createMemo, createSignal, For, onMount, Show } from "solid-js"
import type { Message } from "../api/types"
import { MiniAvatar } from "./Avatar"
import { Icon } from "./icons"
import { openAll, select, state, userText } from "./store"

/** fold lowercases and drops accents one character for one character, so
 * an index into the folded text is an index into the original. */
function fold(s: string): string {
  return s
    .toLocaleLowerCase("tr")
    .replace(/ı/g, "i")
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
}

interface Hit {
  key: string
  dotId: string
  messageId?: string
  title: string
  before: string
  match: string
  after: string
  at?: number
}

function textOf(m: Message): string {
  if (m.role === "user") return userText(m)
  return m.parts
    .filter((p) => p.type === "text")
    .map((p) => (p.type === "text" ? p.data.text : ""))
    .join("\n")
}

/** excerpt cuts the text around the first match of q. */
function excerpt(text: string, q: string): [string, string, string] | undefined {
  const flat = text.replace(/\s+/g, " ")
  const i = fold(flat).indexOf(q)
  if (i < 0) return undefined
  const start = Math.max(0, i - 48)
  return [(start > 0 ? "…" : "") + flat.slice(start, i), flat.slice(i, i + q.length), flat.slice(i + q.length, i + q.length + 90)]
}

export function Search(props: { onClose: () => void }) {
  let input!: HTMLInputElement
  const [q, setQ] = createSignal("")
  const [active, setActive] = createSignal(0)
  const [loading, setLoading] = createSignal(true)
  onMount(() => {
    input.focus()
    void openAll().finally(() => setLoading(false))
  })

  const hits = createMemo<Hit[]>(() => {
    const query = fold(q().trim())
    if (!query) return state.dots.map((d) => ({ key: d.id, dotId: d.id, title: d.name, before: d.role, match: "", after: "" }))
    const out: Hit[] = []
    for (const d of state.dots) {
      if (fold(`${d.name} ${d.role}`).includes(query)) out.push({ key: d.id, dotId: d.id, title: d.name, before: d.role, match: "", after: "" })
    }
    for (const d of state.dots) {
      const msgs = state.rt[d.id]?.messages ?? []
      for (let i = msgs.length - 1; i >= 0 && out.length < 60; i--) {
        const m = msgs[i]
        if (m.is_summary_message || (m.role !== "user" && m.role !== "assistant")) continue
        const ex = excerpt(textOf(m), query)
        if (!ex) continue
        out.push({ key: m.id, dotId: d.id, messageId: m.id, title: m.role === "user" ? `Sen → ${d.name}` : d.name, before: ex[0], match: ex[1], after: ex[2], at: m.created_at })
      }
    }
    return out
  })

  const open = async (h: Hit | undefined) => {
    if (!h) return
    props.onClose()
    await select(h.dotId)
    if (!h.messageId) return
    // The thread renders on the next frames; wait for the message.
    for (let i = 0; i < 20; i++) {
      const el = document.querySelector<HTMLElement>(`[data-mid="${CSS.escape(h.messageId)}"]`)
      if (el) {
        el.scrollIntoView({ block: "center", behavior: "smooth" })
        el.classList.add("flash")
        setTimeout(() => el.classList.remove("flash"), 1600)
        return
      }
      await new Promise((r) => setTimeout(r, 50))
    }
  }

  const dotOf = (id: string) => state.dots.find((d) => d.id === id)

  return (
    <div class="scrim top" onClick={(e) => e.target === e.currentTarget && props.onClose()}>
      <div class="palette" role="dialog" aria-modal="true" aria-label="Ara">
        <div class="palette-input">
          {Icon.search(16)}
          <input
            ref={input}
            placeholder="Dot'larda ve konuşmalarda ara"
            value={q()}
            onInput={(e) => {
              setQ(e.currentTarget.value)
              setActive(0)
            }}
            onKeyDown={(e) => {
              const n = hits().length
              if (e.key === "Escape") props.onClose()
              else if (e.key === "ArrowDown" && n) {
                e.preventDefault()
                setActive((a) => (a + 1) % n)
              } else if (e.key === "ArrowUp" && n) {
                e.preventDefault()
                setActive((a) => (a - 1 + n) % n)
              } else if (e.key === "Enter") {
                e.preventDefault()
                void open(hits()[active()])
              }
            }}
          />
          <Show when={loading()}>
            <span class="spinner sm" />
          </Show>
          <kbd>Esc</kbd>
        </div>
        <div class="palette-list" role="listbox">
          <Show when={hits().length} fallback={<div class="empty slim">“{q()}” hiçbir konuşmada geçmiyor.</div>}>
            <For each={hits()}>
              {(h, i) => {
                const d = () => dotOf(h.dotId)
                return (
                  <button class="palette-item" role="option" aria-selected={i() === active()} onMouseEnter={() => setActive(i())} onClick={() => void open(h)}>
                    <Show when={d()}>{(x) => <MiniAvatar shape={x().shape} color={x().color} size={22} />}</Show>
                    <span class="palette-text">
                      <b>{h.title}</b>
                      <small>
                        {h.before}
                        <Show when={h.match}>
                          <mark>{h.match}</mark>
                        </Show>
                        {h.after}
                      </small>
                    </span>
                    <Show when={h.at}>
                      <span class="faint mono palette-time">
                        {new Date(h.at! > 1e12 ? h.at! : h.at! * 1000).toLocaleDateString([], { day: "numeric", month: "short" })}
                      </span>
                    </Show>
                  </button>
                )
              }}
            </For>
          </Show>
        </div>
      </div>
    </div>
  )
}
