// Text and voice input. Holding Space (outside a text field) or pressing
// the mic button listens; letting go sends what was heard.
//
// Typing "/" opens the command menu: the dot's own commands, then the
// custom commands, workflow recipes and skills Atlas offers in the
// terminal. Typing "@" lists files in the dot's workspace. Files can be
// attached with the clip button, dropped anywhere on the conversation or
// pasted.

import { createEffect, createMemo, createSignal, For, on, onCleanup, onMount, Show } from "solid-js"
import { api } from "../api/client"
import type { Attachment, DirectoryEntry, SlashCommand } from "../api/types"
import type { Dot } from "./api"
import { attachPulse, findCommand, localCommands, type LocalCommand } from "./commands"
import { Icon } from "./icons"
import {
  cancel,
  editAndResend,
  send,
  setListening,
  setTyping,
  state,
  toast,
  type Runtime,
} from "./store"
import { canListen, inputLevel, listen, stopSpeaking } from "./voice"

// Attachment limits, matching the gateway's.
const MAX_FILES = 10
const MAX_FILE_BYTES = 10 << 20
const MAX_TOTAL_BYTES = 25 << 20
const TEXT_EXT = /\.(md|markdown|txt|csv|tsv|log|json|ya?ml|toml|ini|xml|html?|css|js|jsx|ts|tsx|go|py|rb|rs|java|kt|c|h|cpp|hpp|cs|php|sh|ps1|sql|env|conf)$/i

interface Pending {
  id: number
  file: File
  mime: string
  url?: string
}

/** editing is the message the owner is rewriting, if any. */
const [editing, setEditing] = createSignal<{ id: string; text: string } | undefined>()
export { setEditing }

/** mimeOf returns the type a file is sent as, or "" when Atlas cannot
 * read it. */
function mimeOf(f: File): string {
  const t = f.type.toLowerCase()
  if (t.startsWith("image/")) return t === "image/svg+xml" ? "" : t
  if (t.startsWith("text/") || t === "application/json") return t
  if (TEXT_EXT.test(f.name)) return "text/plain"
  return ""
}

async function toAttachment(p: Pending): Promise<Attachment> {
  const buf = await p.file.arrayBuffer()
  let binary = ""
  const bytes = new Uint8Array(buf)
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return { file_name: p.file.name || "yapistirilan", mime_type: p.mime, content: btoa(binary) }
}

function size(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1 << 20) return `${(n / 1024).toFixed(0)} KB`
  return `${(n / (1 << 20)).toFixed(1)} MB`
}

type MenuItem =
  | { kind: "head"; name: string }
  | { kind: "local"; name: string; description: string; command: LocalCommand }
  | { kind: "command"; name: string; description: string; command: SlashCommand }
  | { kind: "file"; name: string; description: string; entry: DirectoryEntry }

const commandCache = new Map<string, SlashCommand[]>()

export function Composer(props: { dot: Dot; rt: Runtime }) {
  let ta!: HTMLTextAreaElement
  let fileInput!: HTMLInputElement
  const [text, setText] = createSignal("")
  const [files, setFiles] = createSignal<Pending[]>([])
  const [bars, setBars] = createSignal<number[]>(Array(28).fill(2))
  const [caret, setCaret] = createSignal(0)
  const [active, setActive] = createSignal(0)
  const [menuClosed, setMenuClosed] = createSignal(false)
  const [commands, setCommands] = createSignal<SlashCommand[]>([])
  const [entries, setEntries] = createSignal<{ dir: string; list: DirectoryEntry[] }>({ dir: "\0", list: [] })
  const [dragging, setDragging] = createSignal(false)
  let stopListen: (() => Promise<string>) | undefined
  let raf = 0
  let seq = 0

  const grow = () => {
    if (!ta) return
    ta.style.height = "20px"
    ta.style.height = Math.min(180, ta.scrollHeight) + "px"
  }

  // ----- Commands -----

  createEffect(
    on(
      () => props.rt.wsId,
      (ws) => {
        const cached = commandCache.get(ws)
        if (cached) return setCommands(cached)
        api
          .commands(ws)
          .then((l) => {
            commandCache.set(ws, l ?? [])
            setCommands(l ?? [])
          })
          .catch(() => setCommands([]))
      },
    ),
  )

  /** token is the "/" or "@" word the caret is in, if any. */
  const token = createMemo(() => {
    const t = text()
    const c = caret()
    const before = t.slice(0, c)
    const slash = /^\/([^\s]*)$/.exec(before)
    if (slash) return { kind: "/" as const, query: slash[1], start: 0 }
    const at = /(^|\s)@([^\s]*)$/.exec(before)
    if (at) return { kind: "@" as const, query: at[2], start: c - at[2].length - 1 }
    return undefined
  })

  createEffect(
    on(
      () => (token()?.kind === "@" ? token()!.query : undefined),
      (q) => {
        if (q === undefined) return
        const dir = q.includes("/") ? q.slice(0, q.lastIndexOf("/")) : ""
        if (entries().dir === dir) return
        setEntries({ dir, list: [] })
        api
          .files(props.rt.wsId, dir)
          .then((l) => setEntries({ dir, list: (l.entries ?? []).filter((e) => !e.name.startsWith(".") && e.name !== "AGENTS.md") }))
          .catch(() => setEntries({ dir, list: [] }))
      },
    ),
  )

  const items = createMemo<MenuItem[]>(() => {
    const tk = token()
    if (!tk || menuClosed()) return []
    const q = tk.query.toLocaleLowerCase("tr")
    if (tk.kind === "/") {
      const match = (name: string, aliases?: string[]) => [name, ...(aliases ?? [])].some((n) => n.toLocaleLowerCase("tr").includes(q))
      const rows: MenuItem[] = []
      const groups = ["Sohbet", "Panel", "Ayarlar", "Terminal"] as const
      for (const g of groups) {
        const hit = localCommands.filter((c) => c.group === g && match(c.name, c.aliases))
        if (!hit.length) continue
        rows.push({ kind: "head", name: g })
        for (const c of hit) rows.push({ kind: "local", name: c.name, description: c.description, command: c })
      }
      const server = commands().filter((c) => match(c.name))
      if (server.length) {
        rows.push({ kind: "head", name: "Bu klasör" })
        for (const c of server) {
          rows.push({
            kind: "command",
            name: c.name,
            description: c.description || (c.kind === "skill" ? "Beceri" : c.kind === "workflow" ? "İş akışı" : "Özel komut"),
            command: c,
          })
        }
      }
      return rows
    }
    const base = q.includes("/") ? q.slice(q.lastIndexOf("/") + 1) : q
    return entries()
      .list.filter((e) => e.name.toLocaleLowerCase("tr").includes(base))
      .sort((a, b) => Number(b.is_dir) - Number(a.is_dir) || a.name.localeCompare(b.name))
      .slice(0, 30)
      .map((e) => ({ kind: "file", name: e.is_dir ? e.path + "/" : e.path, description: e.is_dir ? "Klasör" : e.size != null ? size(e.size) : "Dosya", entry: e }))
  })

  createEffect(
    on(items, (list) => {
      const i = list.findIndex((it) => it.kind !== "head")
      setActive(i < 0 ? 0 : i)
    }),
  )
  createEffect(on(text, () => setMenuClosed(false), { defer: true }))

  const replaceToken = (insert: string) => {
    const tk = token()
    if (!tk) return
    const t = text()
    const next = t.slice(0, tk.start) + insert + t.slice(caret())
    setText(next)
    const pos = tk.start + insert.length
    queueMicrotask(() => {
      ta.focus()
      ta.setSelectionRange(pos, pos)
      setCaret(pos)
      grow()
    })
  }

  const choose = (item: MenuItem | undefined) => {
    if (!item) return
    switch (item.kind) {
      case "head":
        break
      case "local":
        if (item.command.needsArgs) {
          replaceToken(`/${item.name} `)
          setMenuClosed(true)
        } else {
          setText("")
          setCaret(0)
          void Promise.resolve(item.command.run("")).then((prompt) => {
            if (typeof prompt === "string" && prompt.trim()) void send(prompt)
          })
        }
        break
      case "command":
        replaceToken(`/${item.name} `)
        setMenuClosed(true)
        break
      case "file":
        replaceToken(item.entry.is_dir ? `@${item.entry.path}/` : `@${item.entry.path} `)
        if (!item.entry.is_dir) setMenuClosed(true)
        break
    }
  }

  /** expand turns "/name args" into the prompt the command stands for. */
  const expand = async (t: string): Promise<string | undefined> => {
    const m = /^\/(\S+)\s*([\s\S]*)$/.exec(t)
    if (!m) return t
    const cmd = commands().find((c) => c.name === m[1])
    if (!cmd) return t
    const rest = m[2].trim()
    const args: Record<string, string> = {}
    const defs = cmd.arguments ?? []
    if (defs.length === 1) args[defs[0].id] = rest
    else if (defs.length > 1) {
      const named = [...rest.matchAll(/(\w+)=("([^"]*)"|\S+)/g)]
      if (named.length) for (const n of named) args[n[1]] = n[3] ?? n[2]
      else {
        const words = rest.split(/\s+/).filter(Boolean)
        defs.forEach((d, i) => (args[d.id] = i === defs.length - 1 ? words.slice(i).join(" ") : (words[i] ?? "")))
      }
    }
    const missing = defs.filter((d) => d.required && !args[d.id]?.trim())
    if (missing.length) {
      toast(`/${cmd.name} için gerekli: ${missing.map((d) => d.title || d.id).join(", ")}`, true)
      return undefined
    }
    try {
      const out = await api.expandCommand(props.rt.wsId, cmd.id, args)
      return defs.length === 0 && rest ? `${out.prompt}\n\n${rest}` : out.prompt
    } catch (err) {
      toast((err as Error).message, true)
      return undefined
    }
  }

  // ----- Attachments -----

  const addFiles = (list: FileList | File[]) => {
    const cur = files()
    const next = [...cur]
    let total = cur.reduce((n, p) => n + p.file.size, 0)
    for (const f of Array.from(list)) {
      const mime = mimeOf(f)
      if (!mime) {
        toast(`${f.name}: yalnızca görsel ve metin dosyaları eklenebilir`, true)
        continue
      }
      if (f.size > MAX_FILE_BYTES) {
        toast(`${f.name} 10 MB'tan büyük`, true)
        continue
      }
      if (next.length >= MAX_FILES) {
        toast(`En fazla ${MAX_FILES} dosya eklenebilir`, true)
        break
      }
      if (total + f.size > MAX_TOTAL_BYTES) {
        toast("Ekler toplamda 25 MB'ı geçemez", true)
        break
      }
      total += f.size
      next.push({ id: ++seq, file: f, mime, url: mime.startsWith("image/") ? URL.createObjectURL(f) : undefined })
    }
    setFiles(next)
  }

  const removeFile = (id: number) => {
    const p = files().find((x) => x.id === id)
    if (p?.url) URL.revokeObjectURL(p.url)
    setFiles((l) => l.filter((x) => x.id !== id))
  }

  const clearFiles = () => {
    for (const p of files()) if (p.url) URL.revokeObjectURL(p.url)
    setFiles([])
  }

  // ----- Sending -----

  /** mentionNote tells the dot that @words are files in its workspace. */
  const mentionNote = (t: string) => {
    const paths = [...t.matchAll(/(?:^|\s)@([^\s@]+)/g)].map((m) => m[1].replace(/[.,;:!?)]+$/, ""))
    if (!paths.length) return t
    return `${t}\n\n(Bahsedilen dosyalar çalışma alanında: ${[...new Set(paths)].join(", ")})`
  }

  const submit = async () => {
    let t = text().trim()
    const pending = files()
    if (!t && !pending.length) return
    if (props.rt.sending) return
    if (t.startsWith("/")) {
      const typed = /^\/(\S+)\s*([\s\S]*)$/.exec(t)
      const local = typed && findCommand(typed[1])
      if (local) {
        const rest = typed[2].trim()
        if (local.needsArgs && !rest) {
          toast(`/${local.name} için bir metin yaz`)
          return
        }
        setText("")
        setCaret(0)
        const prompt = await local.run(rest)
        if (typeof prompt !== "string" || !prompt.trim()) return
        t = prompt
      } else {
        const expanded = await expand(t)
        if (expanded === undefined) return
        t = expanded
      }
    }
    let attachments: Attachment[] = []
    try {
      attachments = await Promise.all(pending.map(toAttachment))
    } catch {
      toast("Dosyalar okunamadı", true)
      return
    }
    const body = mentionNote(t)
    const ed = editing()
    setText("")
    setCaret(0)
    queueMicrotask(grow)
    const ok = ed ? await editAndResend(ed.id, body) : await send(body, attachments)
    if (ok) {
      clearFiles()
      setEditing(undefined)
    } else if (!text()) setText(t)
  }

  createEffect(
    on(editing, (ed) => {
      if (!ed) return
      setText(ed.text)
      queueMicrotask(() => {
        ta?.focus()
        ta?.setSelectionRange(ed.text.length, ed.text.length)
        grow()
      })
    }),
  )

  createEffect(
    on(attachPulse, (n) => {
      if (n) fileInput?.click()
    }, { defer: true }),
  )

  createEffect(on(() => props.dot.id, () => {
    setEditing(undefined)
    clearFiles()
  }, { defer: true }))

  // ----- Voice -----

  const meter = () => {
    const l = inputLevel()
    setBars((b) => [...b.slice(1), 2 + l * 16])
    raf = requestAnimationFrame(meter)
  }

  const startListening = async () => {
    if (state.listening) return
    if (!canListen()) {
      toast("Bu tarayıcı sesli girişi desteklemiyor. Chrome veya Edge deneyin.", true)
      return
    }
    stopSpeaking()
    setListening(true)
    try {
      stopListen = await listen(state.prefs.lang, (t) => setText(t))
      raf = requestAnimationFrame(meter)
    } catch {
      setListening(false)
      toast("Mikrofon açılamadı.", true)
    }
  }

  const stopListening = async (submitAfter: boolean) => {
    if (!state.listening) return
    cancelAnimationFrame(raf)
    const heard = (await stopListen?.()) ?? ""
    stopListen = undefined
    setListening(false)
    setBars(Array(28).fill(2))
    if (heard) setText(heard)
    if (submitAfter && (heard || text().trim())) void submit()
  }

  const typing = () => {
    const el = document.activeElement
    return el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || (el as HTMLElement)?.isContentEditable
  }
  const down = (e: KeyboardEvent) => {
    if (e.code === "Space" && !e.repeat && !typing() && !document.querySelector(".scrim")) {
      e.preventDefault()
      void startListening()
    }
    if (e.key === "Escape" && props.rt.busy && !document.querySelector(".scrim") && !items().length) void cancel()
  }
  const up = (e: KeyboardEvent) => {
    if (e.code === "Space" && state.listening && !typing()) {
      e.preventDefault()
      void stopListening(true)
    }
  }
  const wake = () => void startListening()

  // Files dropped anywhere on the conversation are attached.
  const hasFiles = (e: DragEvent) => Array.from(e.dataTransfer?.types ?? []).includes("Files")
  const inMain = (e: Event) => !!(e.target as HTMLElement)?.closest?.(".main")
  const dragOver = (e: DragEvent) => {
    if (!hasFiles(e) || !inMain(e)) return
    e.preventDefault()
    setDragging(true)
  }
  const dragLeave = (e: DragEvent) => {
    if (!e.relatedTarget || !(e.relatedTarget as HTMLElement).closest?.(".main")) setDragging(false)
  }
  const drop = (e: DragEvent) => {
    setDragging(false)
    if (!hasFiles(e) || !inMain(e)) return
    e.preventDefault()
    if (e.dataTransfer?.files.length) addFiles(e.dataTransfer.files)
  }

  onMount(() => {
    document.addEventListener("keydown", down)
    document.addEventListener("keyup", up)
    document.addEventListener("dragover", dragOver)
    document.addEventListener("dragleave", dragLeave)
    document.addEventListener("drop", drop)
    window.addEventListener("atlas-dots-wake", wake)
  })
  onCleanup(() => {
    document.removeEventListener("keydown", down)
    document.removeEventListener("keyup", up)
    document.removeEventListener("dragover", dragOver)
    document.removeEventListener("dragleave", dragLeave)
    document.removeEventListener("drop", drop)
    window.removeEventListener("atlas-dots-wake", wake)
    cancelAnimationFrame(raf)
    void stopListen?.()
    clearFiles()
    setTyping(false)
  })

  const canSend = () => (!!text().trim() || files().length > 0) && !props.rt.sending

  return (
    <div class="composer-wrap">
      <Show when={dragging()}>
        <div class="drop-veil">
          {Icon.clip(22)}
          <b>Bırak, {props.dot.name}'e ekleyeyim</b>
          <small>Görseller ve metin dosyaları · en fazla 10 dosya</small>
        </div>
      </Show>
      <Show when={!items().length && !menuClosed() && token()?.kind === "@" && entries().dir === (token()!.query.includes("/") ? token()!.query.slice(0, token()!.query.lastIndexOf("/")) : "")}>
        <div class="menu">
          <div class="menu-head">Çalışma alanı</div>
          <div class="empty slim" style={{ padding: "4px 10px 10px" }}>
            {entries().list.length ? "Bu adla eşleşen dosya yok." : `${props.dot.name}'in çalışma alanında henüz dosya yok.`}
          </div>
        </div>
      </Show>
      <Show when={items().length}>
        <div class="menu" role="listbox" aria-label={token()?.kind === "/" ? "Komutlar" : "Dosyalar"}>
          <div class="menu-head">{token()?.kind === "/" ? "Komutlar" : `Çalışma alanı${entries().dir ? " / " + entries().dir : ""}`}</div>
          <For each={items()}>
            {(it, i) => (
              <Show
                when={it.kind !== "head"}
                fallback={<div class="menu-head">{it.name}</div>}
              >
                <button
                  class="menu-item"
                  role="option"
                  aria-selected={i() === active()}
                  onMouseEnter={() => setActive(i())}
                  onMouseDown={(e) => {
                    e.preventDefault()
                    choose(it)
                  }}
                >
                  <span class="menu-ico">{it.kind === "file" ? (it.entry.is_dir ? Icon.folder(14) : Icon.file(14)) : it.kind === "local" ? Icon.bolt(14) : Icon.sparkle(14)}</span>
                  <b>{it.kind === "file" ? it.name : "/" + it.name}</b>
                  <small>{it.kind === "head" ? "" : it.description}</small>
                </button>
              </Show>
            )}
          </For>
        </div>
      </Show>
      <Show when={editing()}>
        <div class="edit-bar">
          {Icon.pen(13)}
          <span>Mesajı düzenliyorsun. Gönderince sohbet o noktadan yeniden başlar; eski hali saklanır.</span>
          <button class="btn ghost sm" onClick={() => {
            setEditing(undefined)
            setText("")
          }}>
            Vazgeç
          </button>
        </div>
      </Show>
      <div class={`composer ${files().length ? "has-files" : ""}`}>
        <Show when={files().length}>
          <div class="attach-row">
            <For each={files()}>
              {(p) => (
                <div class={`attach ${p.url ? "img" : ""}`} title={`${p.file.name} · ${size(p.file.size)}`}>
                  <Show when={p.url} fallback={<span class="attach-ico">{Icon.file(15)}</span>}>
                    <img src={p.url} alt="" />
                  </Show>
                  <Show when={!p.url}>
                    <span class="attach-name">
                      <b>{p.file.name}</b>
                      <small>{size(p.file.size)}</small>
                    </span>
                  </Show>
                  <button class="attach-x" aria-label={`${p.file.name} dosyasını çıkar`} onClick={() => removeFile(p.id)}>
                    {Icon.close(10)}
                  </button>
                </div>
              )}
            </For>
          </div>
        </Show>
        <div class="composer-row">
          <button class="icon-btn" title="Dosya ekle" disabled={!!editing()} onClick={() => fileInput.click()}>
            {Icon.clip()}
          </button>
          <input
            ref={fileInput}
            type="file"
            multiple
            hidden
            accept="image/png,image/jpeg,image/webp,image/gif,text/*,application/json,.md,.csv,.json,.yaml,.yml,.log,.go,.py,.ts,.tsx,.js"
            onChange={(e) => {
              if (e.currentTarget.files) addFiles(e.currentTarget.files)
              e.currentTarget.value = ""
            }}
          />
          <Show
            when={!state.listening}
            fallback={
              <div class="listening-bar" aria-label="Dinliyor">
                <For each={bars()}>{(h) => <i style={{ height: `${h}px` }} />}</For>
                <span class="muted" style={{ "margin-left": "10px", "font-size": "13px", "white-space": "nowrap", overflow: "hidden", "text-overflow": "ellipsis" }}>
                  {text() || "Dinliyorum…"}
                </span>
              </div>
            }
          >
            <textarea
              ref={ta}
              rows={1}
              value={text()}
              placeholder={`${props.dot.name} için mesaj yaz  ·  / komutlar  ·  @ dosyalar`}
              onInput={(e) => {
                setText(e.currentTarget.value)
                setCaret(e.currentTarget.selectionStart ?? 0)
                setTyping(!!e.currentTarget.value.trim())
                grow()
              }}
              onKeyUp={(e) => setCaret(e.currentTarget.selectionStart ?? 0)}
              onClick={(e) => setCaret(e.currentTarget.selectionStart ?? 0)}
              onBlur={() => setTyping(false)}
              onFocus={(e) => setTyping(!!e.currentTarget.value.trim())}
              onPaste={(e) => {
                const list = Array.from(e.clipboardData?.files ?? [])
                if (list.length) {
                  e.preventDefault()
                  addFiles(list)
                }
              }}
              onKeyDown={(e) => {
                const menu = items()
                if (menu.length) {
                  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                    e.preventDefault()
                    const d = e.key === "ArrowDown" ? 1 : -1
                    setActive((a) => {
                      let i = a
                      for (let n = 0; n < menu.length; n++) {
                        i = (i + d + menu.length) % menu.length
                        if (menu[i].kind !== "head") return i
                      }
                      return a
                    })
                    return
                  }
                  if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey && !e.isComposing)) {
                    const it = menu[active()]
                    // Enter on a fully typed command runs it instead of
                    // completing it again.
                    if (!(e.key === "Enter" && it.kind !== "file" && text().trim() === `/${it.name}` && it.kind === "command")) {
                      e.preventDefault()
                      choose(it)
                      return
                    }
                  }
                  if (e.key === "Escape") {
                    e.preventDefault()
                    setMenuClosed(true)
                    return
                  }
                }
                if (e.key === "Escape" && editing()) {
                  setEditing(undefined)
                  setText("")
                  return
                }
                if (e.key === "ArrowUp" && !text() && !props.rt.busy) {
                  const last = [...props.rt.messages].reverse().find((m) => m.role === "user" && !m.is_summary_message)
                  const t = last?.parts.find((p) => p.type === "text")
                  if (last && t && t.type === "text") {
                    e.preventDefault()
                    setEditing({ id: last.id, text: t.data.text })
                  }
                  return
                }
                if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
                  e.preventDefault()
                  void submit()
                }
              }}
            />
          </Show>
          <button
            class={`icon-btn ${state.listening ? "on" : ""}`}
            title={state.listening ? "Dinlemeyi bitir ve gönder" : "Sesli konuş"}
            onClick={() => (state.listening ? void stopListening(true) : void startListening())}
          >
            {Icon.mic()}
          </button>
          <Show
            when={!props.rt.busy || canSend()}
            fallback={
              <button class="icon-btn send" title="Durdur (Esc)" onClick={() => void cancel()}>
                {Icon.stop()}
              </button>
            }
          >
            <button class="icon-btn send" title={editing() ? "Düzenlenmiş hali gönder" : "Gönder"} disabled={!canSend()} onClick={() => void submit()}>
              {Icon.send()}
            </button>
          </Show>
        </div>
      </div>
      <div class="composer-hint">
        <span>
          <kbd>Enter</kbd> gönder
        </span>
        <span>
          <kbd>/</kbd> komut
        </span>
        <span>
          <kbd>@</kbd> dosya
        </span>
        <span>
          <kbd>↑</kbd> son mesajı düzenle
        </span>
        <span>
          <kbd>Space</kbd> basılı tut, konuş
        </span>
        <Show when={props.rt.busy}>
          <span>
            <kbd>Esc</kbd> durdur
          </span>
        </Show>
      </div>
    </div>
  )
}
