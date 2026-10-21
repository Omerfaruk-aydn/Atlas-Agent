// The dialogs and actions behind the "/" commands that the terminal UI
// answers with a screen of their own: rewind, jobs, usage, files, agents,
// history, search and snippets. Each one reads from the Atlas server through
// the gateway; none of them keeps state the terminal does not.

import { createMemo, createResource, createSignal, For, Show, type Accessor, type JSX } from "solid-js"
import { Button } from "@atlas/ui/button"
import { useDialog } from "@atlas/ui/context/dialog"
import { Dialog, DialogFooter, DialogHeader, DialogTitleGroup } from "@atlas/ui/dialog"
import { TextInput } from "@atlas/ui/text-input"
import { api } from "../api/client"
import type { Catalog, Job, Message, Session } from "../api/types"
import { formatCost, formatTokens, relativeTime, truncate } from "../lib/format"
import { openSession } from "../state/app"
import type { WorkspaceStore } from "../state/workspace"
import type { ContextUsage } from "./context-ring"
import { showToast } from "./toast"

export type PaletteContext = {
  store: Accessor<WorkspaceStore>
  sessionID: Accessor<string | undefined>
  catalog: Accessor<Catalog | undefined>
  usage: Accessor<ContextUsage>
  /** Entries of the prompt history, newest first. */
  history: () => string[]
  setPrompt: (text: string) => void
}

const fail = (err: unknown) => showToast({ variant: "error", title: "Request failed", description: String((err as Error)?.message ?? err) })

export const messageText = (m: Message) =>
  m.parts
    .map((p) => (p.type === "text" ? p.data.text : p.type === "shell_command" ? p.data.command : ""))
    .join("")
    .trim()

type Row = { id: string; title: string; description?: string; meta?: string }

/** A searchable list: type to narrow, Enter or a click to pick. */
function ListDialog(props: {
  title: string
  description?: string
  placeholder?: string
  rows: Accessor<Row[]> | (() => Row[] | undefined)
  loading?: () => boolean
  empty: string
  onPick: (row: Row) => void
  footer?: JSX.Element
}) {
  const dialog = useDialog()
  const [query, setQuery] = createSignal("")
  const [cursor, setCursor] = createSignal(0)
  const visible = createMemo(() => {
    const q = query().trim().toLowerCase()
    const rows = props.rows() ?? []
    return q ? rows.filter((r) => `${r.title} ${r.description ?? ""}`.toLowerCase().includes(q)) : rows
  })
  const pick = (row: Row | undefined) => {
    if (!row) return
    props.onPick(row)
  }
  return (
    <Dialog fit>
      <DialogHeader hideClose>
        <DialogTitleGroup title={props.title} description={props.description} />
      </DialogHeader>
      <div class="flex flex-col gap-2 px-4 pb-2" style={{ "width": "min(560px, 88vw)" }}>
        <TextInput
          appearance="base"
          value={query()}
          placeholder={props.placeholder ?? "Search"}
          autofocus
          spellcheck={false}
          autocomplete="off"
          onInput={(event) => {
            setQuery(event.currentTarget.value)
            setCursor(0)
          }}
          onKeyDown={(event) => {
            if (event.key === "ArrowDown") {
              event.preventDefault()
              setCursor((i) => Math.min(i + 1, visible().length - 1))
            } else if (event.key === "ArrowUp") {
              event.preventDefault()
              setCursor((i) => Math.max(i - 1, 0))
            } else if (event.key === "Enter") {
              event.preventDefault()
              pick(visible()[cursor()])
            }
          }}
        />
        <div class="flex max-h-[min(360px,50vh)] flex-col overflow-y-auto" role="listbox">
          <Show when={!props.loading?.()} fallback={<div class="px-2 py-6 text-center text-[13px] text-v2-text-text-muted">…</div>}>
            <Show when={visible().length > 0} fallback={<div class="px-2 py-6 text-center text-[13px] text-v2-text-text-muted">{props.empty}</div>}>
              <For each={visible()}>
                {(row, index) => (
                  <button
                    type="button"
                    role="option"
                    aria-selected={cursor() === index()}
                    class="flex items-baseline gap-3 rounded-md px-2 py-1.5 text-left text-[13px] text-v2-text-text-base hover:bg-v2-overlay-simple-overlay-hover"
                    classList={{ "bg-v2-overlay-simple-overlay-hover": cursor() === index() }}
                    onMouseEnter={() => setCursor(index())}
                    onClick={() => pick(row)}
                  >
                    <span class="min-w-0 flex-1 truncate">{row.title}</span>
                    <Show when={row.description}>
                      <span class="max-w-[45%] truncate text-v2-text-text-muted">{row.description}</span>
                    </Show>
                    <Show when={row.meta}>
                      <span class="shrink-0 tabular-nums text-v2-text-text-muted">{row.meta}</span>
                    </Show>
                  </button>
                )}
              </For>
            </Show>
          </Show>
        </div>
      </div>
      <DialogFooter>
        {props.footer}
        <Button variant="ghost" onClick={() => dialog.close()}>
          Close
        </Button>
      </DialogFooter>
    </Dialog>
  )
}

function needSession(ctx: PaletteContext): string | undefined {
  const sid = ctx.sessionID()
  if (!sid) showToast({ title: "Open a session first", description: "This command works on the session you are in." })
  return sid
}

// --- Rewind -----------------------------------------------------------

function RewindConfirm(props: { ctx: PaletteContext; sid: string; message: Message }) {
  const dialog = useDialog()
  const store = props.ctx.store()
  const [preview] = createResource(() => api.rewindPreview(store.id, props.sid, props.message.id))
  const [busy, setBusy] = createSignal(false)
  const run = async () => {
    setBusy(true)
    try {
      const result = await api.rewind(store.id, props.sid, props.message.id)
      dialog.close()
      openSession(store.id, result.session.id)
      showToast({ title: "Rewound", description: `${result.files_written} files written, ${result.files_deleted} deleted. Continuing in a new session.` })
    } catch (err) {
      setBusy(false)
      fail(err)
    }
  }
  return (
    <Dialog fit>
      <DialogHeader hideClose>
        <DialogTitleGroup title="Rewind to this message?" description={truncate(messageText(props.message), 140)} />
      </DialogHeader>
      <div class="px-4 pb-2 text-[13px] text-v2-text-text-base" style={{ "width": "min(460px, 88vw)" }}>
        <Show when={!preview.loading} fallback={<span class="text-v2-text-text-muted">Checking what would change…</span>}>
          <Show when={preview()} fallback={<span class="text-v2-text-text-muted">The files can't be previewed.</span>}>
            {(p) => (
              <span>
                The session is forked here and the project goes back to that point: {p().files_to_write} files rewritten,{" "}
                {p().files_to_delete} deleted. This session stays as it is.
              </span>
            )}
          </Show>
        </Show>
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={() => dialog.close()}>
          Cancel
        </Button>
        <Button variant="submit" disabled={busy() || preview.loading} onClick={() => void run()}>
          Rewind
        </Button>
      </DialogFooter>
    </Dialog>
  )
}

function RewindDialog(props: { ctx: PaletteContext; sid: string }) {
  const dialog = useDialog()
  const [messages] = createResource(() => api.userMessages(props.ctx.store().id, props.sid).catch(() => [] as Message[]))
  const rows = () => [...(messages() ?? [])].reverse().map((m) => ({ id: m.id, title: truncate(messageText(m) || "(attachment)", 90), meta: relativeTime(m.created_at) }))
  return (
    <ListDialog
      title="Rewind to checkpoint"
      description="Pick the message to go back to"
      rows={rows}
      loading={() => messages.loading}
      empty="No messages to rewind to yet."
      onPick={(row) => {
        const message = messages()?.find((m) => m.id === row.id)
        if (message) dialog.show(() => <RewindConfirm ctx={props.ctx} sid={props.sid} message={message} />)
      }}
    />
  )
}

// --- Jobs -------------------------------------------------------------

function JobOutput(props: { workspace: string; job: Job }) {
  const dialog = useDialog()
  const [output, { refetch }] = createResource(() => api.jobOutput(props.workspace, props.job.id).catch(() => undefined))
  const kill = async () => {
    try {
      await api.killJob(props.workspace, props.job.id)
      showToast({ title: "Job stopped" })
      dialog.close()
    } catch (err) {
      fail(err)
    }
  }
  return (
    <Dialog fit>
      <DialogHeader hideClose>
        <DialogTitleGroup title={props.job.description || props.job.command} description={props.job.status} />
      </DialogHeader>
      <pre
        class="mx-4 mb-2 max-h-[50vh] overflow-auto rounded-md p-3 text-[12px] leading-5 text-v2-text-text-base"
        style={{ background: "var(--v2-background-bg-deep)", "width": "min(620px, 88vw)", "white-space": "pre-wrap" }}
      >
        {(output()?.stdout ?? "") + (output()?.stderr ? `\n${output()!.stderr}` : "") || "No output yet."}
      </pre>
      <DialogFooter>
        <Button variant="ghost" onClick={() => void refetch()}>
          Refresh
        </Button>
        <Show when={!props.job.done}>
          <Button variant="ghost" onClick={() => void kill()}>
            Stop job
          </Button>
        </Show>
        <Button variant="ghost" onClick={() => dialog.close()}>
          Close
        </Button>
      </DialogFooter>
    </Dialog>
  )
}

function JobsDialog(props: { ctx: PaletteContext }) {
  const dialog = useDialog()
  const [jobs] = createResource(() => api.jobs(props.ctx.store().id).catch(() => [] as Job[]))
  const rows = () => (jobs() ?? []).map((j) => ({ id: j.id, title: j.description || j.command, description: j.command, meta: j.done ? j.status : "running" }))
  return (
    <ListDialog
      title="Background jobs"
      description="Commands the agent left running"
      rows={rows}
      loading={() => jobs.loading}
      empty="No background jobs."
      onPick={(row) => {
        const job = jobs()?.find((j) => j.id === row.id)
        if (job) dialog.show(() => <JobOutput workspace={props.ctx.store().id} job={job} />)
      }}
    />
  )
}

// --- Usage ------------------------------------------------------------

function UsageDialog(props: { ctx: PaletteContext }) {
  const dialog = useDialog()
  const usage = props.ctx.usage
  const session = () => props.ctx.store().state.sessions.find((s) => s.id === props.ctx.sessionID())
  const percent = () => (usage().window ? Math.min(100, (usage().tokens / usage().window!) * 100) : undefined)
  const line = (label: string, value: string) => (
    <div class="flex items-baseline justify-between gap-8 text-[13px]">
      <span class="text-v2-text-text-muted">{label}</span>
      <span class="tabular-nums text-v2-text-text-base">{value}</span>
    </div>
  )
  return (
    <Dialog fit>
      <DialogHeader hideClose>
        <DialogTitleGroup title="Usage & cost" description={session()?.title || undefined} />
      </DialogHeader>
      <div class="flex flex-col gap-2 px-4 pb-3" style={{ "width": "min(420px, 88vw)" }}>
        {line("Context window", usage().window ? `${formatTokens(usage().tokens)} / ${formatTokens(usage().window!)}` : formatTokens(usage().tokens))}
        <Show when={percent() !== undefined}>{line("Used", `${percent()!.toFixed(percent()! < 1 ? 1 : 0)}%`)}</Show>
        {line("Prompt tokens", formatTokens(session()?.prompt_tokens ?? 0))}
        {line("Completion tokens", formatTokens(session()?.completion_tokens ?? 0))}
        {line("Messages", String(session()?.message_count ?? 0))}
        {line("Cost", formatCost(usage().cost))}
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={() => dialog.close()}>
          Close
        </Button>
      </DialogFooter>
    </Dialog>
  )
}

// --- Files, agents, history, search, sessions ---------------------------

function FilesDialog(props: { ctx: PaletteContext; sid: string }) {
  const dialog = useDialog()
  const [files] = createResource(() => api.sessionFiles(props.ctx.store().id, props.sid).catch(() => []))
  const rows = () => (files() ?? []).map((f) => ({ id: f.path, title: f.path }))
  return (
    <ListDialog
      title="Modified files"
      description="Files this session has read or changed"
      rows={rows}
      loading={() => files.loading}
      empty="No files yet."
      onPick={(row) => {
        props.ctx.setPrompt(`@${row.id} `)
        dialog.close()
      }}
    />
  )
}

function AgentsDialog(props: { ctx: PaletteContext; sid: string }) {
  const dialog = useDialog()
  const [agents] = createResource(() => api.agents(props.ctx.store().id, props.sid).catch(() => []))
  const rows = () =>
    (agents() ?? []).map((a) => ({ id: a.session_id, title: a.title || a.session_id, description: a.busy ? "running" : "idle", meta: formatCost(a.cost) }))
  return (
    <ListDialog
      title="Agents"
      description="Sub-agents this session started"
      rows={rows}
      loading={() => agents.loading}
      empty="No sub-agents running."
      onPick={(row) => {
        openSession(props.ctx.store().id, row.id)
        dialog.close()
      }}
    />
  )
}

function HistoryDialog(props: { ctx: PaletteContext }) {
  const dialog = useDialog()
  const rows = () => props.ctx.history().map((text, i) => ({ id: String(i), title: truncate(text.replace(/\s+/g, " "), 120) }))
  return (
    <ListDialog
      title="Prompt history"
      description="Prompts you have sent before"
      rows={rows}
      empty="Nothing sent yet."
      onPick={(row) => {
        props.ctx.setPrompt(props.ctx.history()[Number(row.id)])
        dialog.close()
      }}
    />
  )
}

function SearchChatDialog(props: { ctx: PaletteContext; sid: string }) {
  const dialog = useDialog()
  const rows = () =>
    (props.ctx.store().state.messages[props.sid] ?? [])
      .filter((m) => m.role === "user" || m.role === "assistant")
      .map((m) => ({ id: m.id, title: truncate(messageText(m).replace(/\s+/g, " "), 120), description: m.role, meta: relativeTime(m.created_at) }))
      .filter((r) => r.title)
      .reverse()
  return (
    <ListDialog
      title="Search chat"
      description="Find something said in this conversation"
      rows={rows}
      empty="Nothing matches."
      onPick={(row) => {
        dialog.close()
        requestAnimationFrame(() => {
          const el = document.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(row.id)}"]`)
          if (el) el.scrollIntoView({ block: "center" })
          else showToast({ title: "Not on screen", description: "That message is not loaded in the view." })
        })
      }}
    />
  )
}

function FindSessionsDialog(props: { ctx: PaletteContext }) {
  const dialog = useDialog()
  const rows = () =>
    [...props.ctx.store().state.sessions]
      .sort((a, b) => b.updated_at - a.updated_at)
      .map((s: Session) => ({ id: s.id, title: s.title || "Untitled", description: `${s.message_count} messages`, meta: relativeTime(s.updated_at) }))
  return (
    <ListDialog
      title="Find session"
      description="Search all sessions in this project"
      rows={rows}
      empty="No sessions match."
      onPick={(row) => {
        openSession(props.ctx.store().id, row.id)
        dialog.close()
      }}
    />
  )
}

// --- Snippets (kept in this browser) -----------------------------------

const SNIPPETS_KEY = "atlas.snippets"
const readSnippets = (): string[] => {
  try {
    const raw = JSON.parse(localStorage.getItem(SNIPPETS_KEY) ?? "[]")
    return Array.isArray(raw) ? raw.filter((s) => typeof s === "string") : []
  } catch {
    return []
  }
}
const writeSnippets = (list: string[]) => {
  try {
    localStorage.setItem(SNIPPETS_KEY, JSON.stringify(list))
  } catch {
    // Private windows may refuse storage; the snippet then lasts until reload.
  }
}

function SnippetsDialog(props: { ctx: PaletteContext }) {
  const dialog = useDialog()
  const [list, setList] = createSignal(readSnippets())
  const [draft, setDraft] = createSignal("")
  const add = () => {
    const text = draft().trim()
    if (!text) return
    const next = [text, ...list().filter((s) => s !== text)]
    writeSnippets(next)
    setList(next)
    setDraft("")
  }
  const remove = (text: string) => {
    const next = list().filter((s) => s !== text)
    writeSnippets(next)
    setList(next)
  }
  return (
    <Dialog fit>
      <DialogHeader hideClose>
        <DialogTitleGroup title="Snippets" description="Saved prompts, kept in this browser" />
      </DialogHeader>
      <div class="flex flex-col gap-2 px-4 pb-2" style={{ "width": "min(560px, 88vw)" }}>
        <form
          class="flex gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            add()
          }}
        >
          <TextInput appearance="base" value={draft()} placeholder="Save a new snippet" spellcheck={false} autocomplete="off" onInput={(e) => setDraft(e.currentTarget.value)} />
          <Button variant="ghost" type="submit">
            Save
          </Button>
        </form>
        <div class="flex max-h-[min(320px,45vh)] flex-col overflow-y-auto">
          <Show when={list().length > 0} fallback={<div class="px-2 py-6 text-center text-[13px] text-v2-text-text-muted">No snippets yet.</div>}>
            <For each={list()}>
              {(text) => (
                <div class="flex items-center gap-2 rounded-md px-2 py-1.5 text-[13px] hover:bg-v2-overlay-simple-overlay-hover">
                  <button
                    type="button"
                    class="min-w-0 flex-1 truncate text-left text-v2-text-text-base"
                    onClick={() => {
                      props.ctx.setPrompt(text)
                      dialog.close()
                    }}
                  >
                    {text}
                  </button>
                  <button type="button" class="text-v2-text-text-muted hover:text-v2-text-text-base" aria-label="Delete snippet" onClick={() => remove(text)}>
                    ×
                  </button>
                </div>
              )}
            </For>
          </Show>
        </div>
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={() => dialog.close()}>
          Close
        </Button>
      </DialogFooter>
    </Dialog>
  )
}

// --- Workflow views -----------------------------------------------------

const isScalar = (v: unknown) => typeof v === "string" || typeof v === "number" || typeof v === "boolean"
const isEmpty = (v: unknown) => v == null || (Array.isArray(v) && v.length === 0) || (typeof v === "object" && !Array.isArray(v) && Object.keys(v as object).length === 0)

/** One row per item of a list, or per field of an object. A row whose value
 * has more inside can be opened. */
function workflowRows(value: unknown): (Row & { child?: unknown })[] {
  if (value == null) return []
  if (Array.isArray(value)) {
    return value.map((item, i) => {
      if (!item || typeof item !== "object") return { id: String(i), title: truncate(String(item), 120) }
      const o = item as Record<string, unknown>
      const title = String(o.title ?? o.name ?? o.summary ?? o.kind ?? o.id ?? `#${i + 1}`)
      const rest = Object.entries(o)
        .filter(([k, v]) => isScalar(v) && k !== "id" && String(v) !== title)
        .map(([k, v]) => `${k}: ${truncate(String(v), 60)}`)
      return { id: String(o.id ?? i), title: truncate(title, 90), description: rest.slice(0, 4).join(" · "), child: o }
    })
  }
  if (typeof value === "object") {
    return Object.entries(value as Record<string, unknown>)
      .filter(([, v]) => !isEmpty(v))
      .map(([key, v]) => ({
        id: key,
        title: key.replace(/_/g, " "),
        description: isScalar(v) ? truncate(String(v), 120) : Array.isArray(v) ? `${v.length} items` : "open to see more",
        child: isScalar(v) ? undefined : v,
      }))
  }
  return [{ id: "value", title: truncate(String(value), 200) }]
}

function WorkflowSection(props: { title: string; description?: string; value: unknown }) {
  const dialog = useDialog()
  const rows = () => workflowRows(props.value)
  return (
    <ListDialog
      title={props.title}
      description={props.description}
      rows={rows}
      empty="Nothing here yet."
      onPick={(row) => {
        const child = rows().find((r) => r.id === row.id)?.child
        if (child !== undefined) dialog.push(() => <WorkflowSection title={row.title} value={child} />)
      }}
    />
  )
}

function WorkflowDialog(props: { ctx: PaletteContext; sid: string; title: string; section?: string }) {
  const [snapshot] = createResource(async () => {
    try {
      return { value: await api.workflow(props.ctx.store().id, props.sid) }
    } catch (err) {
      return { error: String((err as Error)?.message ?? err) }
    }
  })
  const value = () => {
    const data = snapshot.latest?.value
    return props.section ? data?.[props.section] : data
  }
  return (
    <Show
      when={snapshot.latest}
      fallback={<ListDialog title={props.title} rows={() => []} loading={() => true} empty="" onPick={() => {}} />}
    >
      <Show
        when={!snapshot.latest?.error}
        fallback={<ListDialog title={props.title} rows={() => []} empty={snapshot.latest!.error!} onPick={() => {}} />}
      >
        <WorkflowSection title={props.title} description="Read-only view of what the agent is coordinating in this session" value={value()} />
      </Show>
    </Show>
  )
}

// --- Compaction ---------------------------------------------------------

function CompactAtDialog(props: { ctx: PaletteContext }) {
  const dialog = useDialog()
  const [settings] = createResource(() => api.settings(props.ctx.store().id).catch(() => undefined))
  const [draft, setDraft] = createSignal<string | undefined>(undefined)
  const shown = () => draft() ?? (settings.latest?.auto_compact_at ? String(settings.latest.auto_compact_at) : "")
  const save = async () => {
    const raw = shown().trim()
    const percent = raw === "" ? 0 : Number(raw)
    if (!Number.isFinite(percent) || (percent !== 0 && (percent < 1 || percent > 99))) {
      showToast({ variant: "error", title: "Threshold must be a number between 1 and 99" })
      return
    }
    try {
      await api.compaction(props.ctx.store().id, { percent })
      props.ctx.store().settingsChanged()
      showToast({ title: percent === 0 ? "Auto-compact threshold reset to the built-in defaults" : `Auto-compact threshold set to ${percent}% of the context window` })
      dialog.close()
    } catch (err) {
      fail(err)
    }
  }
  return (
    <Dialog fit>
      <DialogHeader hideClose>
        <DialogTitleGroup title="Auto-compact threshold" description="Percent of the model's context window that may be used before the session summarizes itself. Leave empty for the built-in thresholds." />
      </DialogHeader>
      <form
        class="flex gap-2 px-4 pb-2"
        style={{ "width": "min(480px, 88vw)" }}
        onSubmit={(event) => {
          event.preventDefault()
          void save()
        }}
      >
        <TextInput appearance="base" value={shown()} placeholder="e.g. 80" inputmode="numeric" autofocus spellcheck={false} autocomplete="off" onInput={(e) => setDraft(e.currentTarget.value)} />
        <Button variant="ghost" type="submit">
          Save
        </Button>
      </form>
      <DialogFooter>
        <Button variant="ghost" onClick={() => dialog.close()}>
          Close
        </Button>
      </DialogFooter>
    </Dialog>
  )
}

// --- Goal ---------------------------------------------------------------

function GoalDialog(props: { ctx: PaletteContext; sid: string }) {
  const dialog = useDialog()
  const id = () => props.ctx.store().id
  const [status, { refetch }] = createResource(() => api.goal(id(), props.sid).catch(() => undefined))
  const [draft, setDraft] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const submit = async (goal: string) => {
    setBusy(true)
    try {
      await api.setGoal(id(), props.sid, goal)
      showToast({ title: goal ? "Goal set" : "Goal cleared" })
      if (goal) dialog.close()
      else {
        setDraft("")
        await refetch()
      }
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog fit>
      <DialogHeader hideClose>
        <DialogTitleGroup title="Goal" description="Atlas keeps taking turns on its own until the goal is reached or its turn budget runs out" />
      </DialogHeader>
      <div class="flex flex-col gap-3 px-4 pb-2" style={{ "width": "min(560px, 88vw)" }}>
        <Show when={status.latest?.active}>
          <div class="rounded-md border border-v2-border-border-muted px-3 py-2 text-[13px] text-v2-text-text-base">
            <div class="text-v2-text-text-muted">
              Working towards · turn {status.latest!.used} of {status.latest!.budget}
            </div>
            <div class="mt-1">{status.latest!.goal}</div>
          </div>
        </Show>
        <form
          class="flex gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            const goal = draft().trim()
            if (goal) void submit(goal)
          }}
        >
          <TextInput appearance="base" value={draft()} placeholder="What should Atlas work towards?" autofocus spellcheck={false} autocomplete="off" onInput={(e) => setDraft(e.currentTarget.value)} />
          <Button variant="ghost" type="submit" disabled={busy() || !draft().trim()}>
            Start
          </Button>
        </form>
      </div>
      <DialogFooter>
        <Show when={status.latest?.active}>
          <Button variant="ghost" disabled={busy()} onClick={() => void submit("")}>
            Clear goal
          </Button>
        </Show>
        <Button variant="ghost" onClick={() => dialog.close()}>
          Close
        </Button>
      </DialogFooter>
    </Dialog>
  )
}

// --- Entry points -------------------------------------------------------

export type PaletteDialog = "rewind" | "jobs" | "usage" | "files" | "agents" | "history" | "search" | "find" | "snippets" | "goal" | "compact-at" | "workflow"
export type PaletteRun = "compact" | "fresh" | "correct" | "yolo" | "permissions" | "autocompact"

/** The workflow views: the one dialog over the server's workflow snapshot,
 * narrowed to the part a command is about. */
export function openWorkflowView(ctx: PaletteContext, dialog: ReturnType<typeof useDialog>, title: string, section?: string) {
  const sid = needSession(ctx)
  if (!sid) return
  return dialog.show(() => <WorkflowDialog ctx={ctx} sid={sid} title={title} section={section} />)
}

export function openPaletteDialog(name: PaletteDialog, ctx: PaletteContext, dialog: ReturnType<typeof useDialog>) {
  const needs = name === "rewind" || name === "files" || name === "agents" || name === "search" || name === "goal" || name === "workflow"
  const sid = needs ? needSession(ctx) : ctx.sessionID()
  if (needs && !sid) return
  switch (name) {
    case "rewind":
      return dialog.show(() => <RewindDialog ctx={ctx} sid={sid!} />)
    case "jobs":
      return dialog.show(() => <JobsDialog ctx={ctx} />)
    case "usage":
      return dialog.show(() => <UsageDialog ctx={ctx} />)
    case "files":
      return dialog.show(() => <FilesDialog ctx={ctx} sid={sid!} />)
    case "agents":
      return dialog.show(() => <AgentsDialog ctx={ctx} sid={sid!} />)
    case "history":
      return dialog.show(() => <HistoryDialog ctx={ctx} />)
    case "search":
      return dialog.show(() => <SearchChatDialog ctx={ctx} sid={sid!} />)
    case "find":
      return dialog.show(() => <FindSessionsDialog ctx={ctx} />)
    case "snippets":
      return dialog.show(() => <SnippetsDialog ctx={ctx} />)
    case "goal":
      return dialog.show(() => <GoalDialog ctx={ctx} sid={sid!} />)
    case "workflow":
      return dialog.show(() => <WorkflowDialog ctx={ctx} sid={sid!} title="Workflow" />)
    case "compact-at":
      return dialog.show(() => <CompactAtDialog ctx={ctx} />)
  }
}

const permissionOrder = ["manual", "auto_accept_edits", "plan", "bypass"] as const
const permissionLabel: Record<(typeof permissionOrder)[number], string> = {
  manual: "Ask before every tool",
  auto_accept_edits: "Accept edits automatically",
  plan: "Plan only",
  bypass: "Run everything without asking",
}

export async function runPaletteAction(name: PaletteRun, ctx: PaletteContext) {
  const store = ctx.store()
  if (name === "autocompact") {
    try {
      const enabled = !((await api.settings(store.id)).auto_compact ?? true)
      await api.compaction(store.id, { enabled })
      store.settingsChanged()
      showToast({ title: enabled ? "Auto-compact enabled" : "Auto-compact disabled", description: enabled ? undefined : "Summarize manually with /compact." })
    } catch (err) {
      fail(err)
    }
    return
  }
  if (name === "yolo" || name === "permissions") {
    try {
      const current = (await api.settings(store.id)).permission_mode
      const next = name === "yolo" ? (current === "bypass" ? "manual" : "bypass") : permissionOrder[(permissionOrder.indexOf(current) + 1) % permissionOrder.length]
      await api.setPermissionMode(store.id, next)
      store.settingsChanged()
      showToast({ title: permissionLabel[next] })
    } catch (err) {
      fail(err)
    }
    return
  }
  const sid = needSession(ctx)
  if (!sid) return
  try {
    if (name === "compact") {
      await api.summarize(store.id, sid)
      showToast({ title: "Summarizing", description: "The session is being summarized to free up context." })
    } else if (name === "fresh") {
      await api.cancel(store.id, sid).catch(() => undefined)
      await store.reloadMessages(sid)
      showToast({ title: "Session refreshed" })
    } else {
      await api.cancel(store.id, sid)
      requestAnimationFrame(() => document.querySelector<HTMLElement>('[data-component="composer"] [contenteditable]')?.focus())
    }
  } catch (err) {
    fail(err)
  }
}
