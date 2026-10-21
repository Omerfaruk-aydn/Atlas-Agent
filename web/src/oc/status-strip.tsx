// The two strips the terminal shows around its editor: the agent's to-do
// list and queued prompts above it, and a status line below it with the
// model, working mode, permission mode and what the session has used.

import { createMemo, createResource, createSignal, For, Show, type Accessor } from "solid-js"
import { api } from "../api/client"
import type { Catalog, PermissionMode } from "../api/types"
import { formatCost, formatTokens } from "../lib/format"
import { prefs } from "../state/prefs"
import type { WorkspaceStore } from "../state/workspace"
import { findModel } from "../components/ModelPicker"
import { showToast } from "./toast"

const permissionName: Record<PermissionMode, string> = {
  manual: "Ask first",
  auto_accept_edits: "Accept edits",
  plan: "Plan",
  bypass: "Yolo",
}

const dot = (status: string) => (status === "completed" ? "✓" : status === "in_progress" ? "▸" : "○")

export function TodoStrip(props: { store: WorkspaceStore; sessionID: string }) {
  const session = createMemo(() => props.store.state.sessions.find((s) => s.id === props.sessionID))
  const todos = () => session()?.todos ?? []
  const queued = () => props.store.state.status[props.sessionID]?.queued ?? 0
  const [open, setOpen] = createSignal(false)
  const done = () => todos().filter((t) => t.status === "completed").length
  const current = () => todos().find((t) => t.status === "in_progress") ?? todos().find((t) => t.status !== "completed")

  const clear = async () => {
    try {
      await api.clearQueue(props.store.id, props.sessionID)
    } catch (err) {
      showToast({ variant: "error", title: "Request failed", description: String((err as Error)?.message ?? err) })
    }
  }

  return (
    <Show when={prefs().pills && (todos().length > 0 || queued() > 0)}>
      <div data-component="todo-strip" class="mb-2 rounded-lg border border-v2-border-border-muted px-3 py-1.5 text-[12px] text-v2-text-text-base">
        <div class="flex items-center gap-3">
          <Show when={todos().length > 0}>
            <button type="button" class="flex min-w-0 flex-1 items-center gap-2 text-left" aria-expanded={open()} onClick={() => setOpen((v) => !v)}>
              <span class="tabular-nums text-v2-text-text-muted">
                {done()}/{todos().length}
              </span>
              <span class="min-w-0 flex-1 truncate">{current()?.active_form || current()?.content || "All done"}</span>
              <span class="text-v2-text-text-muted">{open() ? "▾" : "▸"}</span>
            </button>
          </Show>
          <Show when={queued() > 0}>
            <span class="flex shrink-0 items-center gap-2 text-v2-text-text-muted">
              {queued()} queued
              <button type="button" class="underline-offset-2 hover:underline" onClick={() => void clear()}>
                Clear
              </button>
            </span>
          </Show>
        </div>
        <Show when={open() && todos().length > 0}>
          <ul class="mt-1.5 flex max-h-40 flex-col gap-0.5 overflow-y-auto">
            <For each={todos()}>
              {(todo) => (
                <li class="flex gap-2" classList={{ "text-v2-text-text-muted": todo.status === "completed" }}>
                  <span class="w-3 shrink-0 text-center" aria-hidden="true">
                    {dot(todo.status)}
                  </span>
                  <span classList={{ "line-through": todo.status === "completed" }}>{todo.content}</span>
                </li>
              )}
            </For>
          </ul>
        </Show>
      </div>
    </Show>
  )
}

export function StatusLine(props: { store: WorkspaceStore; sessionID: string; catalog: Accessor<Catalog | undefined> }) {
  const [settings] = createResource(
    () => ({ id: props.store.id, v: props.store.state.versions.settings }),
    ({ id }) => api.settings(id).catch(() => undefined),
  )
  const session = createMemo(() => props.store.state.sessions.find((s) => s.id === props.sessionID))
  const active = () => props.catalog()?.active ?? props.catalog()?.selected?.large
  const model = () => findModel(props.catalog(), active()?.provider, active()?.model)
  const tokens = () => (session() ? session()!.prompt_tokens + session()!.completion_tokens : 0)

  const part = (text: string | undefined) => (
    <Show when={text}>
      <span class="truncate">{text}</span>
    </Show>
  )
  return (
    <div data-component="status-line" class="mt-1.5 flex min-w-0 items-center gap-3 px-1 text-[11px] text-v2-text-text-muted">
      {part(model()?.name ?? active()?.model)}
      {part(settings.latest?.session_mode)}
      <Show when={settings.latest}>
        <span classList={{ "text-[#ef4444]": settings.latest!.permission_mode === "bypass" }}>{permissionName[settings.latest!.permission_mode]}</span>
      </Show>
      <span class="ms-auto shrink-0 tabular-nums">
        {formatTokens(tokens())}
        <Show when={(session()?.cost ?? 0) > 0}> · {formatCost(session()!.cost)}</Show>
      </span>
    </div>
  )
}
