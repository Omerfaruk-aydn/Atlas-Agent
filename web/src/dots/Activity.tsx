// The dot's side panel: what it did, step by step; the files in its own
// workspace; and what its session has cost so far.

import { createEffect, createMemo, createResource, createSignal, For, Match, on, Show, Switch } from "solid-js"
import { api } from "../api/client"
import type { Message } from "../api/types"
import type { Dot } from "./api"
import { panelTab, setPanelTab, type SideTab } from "./commands"
import { Automation } from "./Automation"
import { Icon } from "./icons"
import { Audit, Memory, SpendingChart } from "./Insights"
import { ScreenPane } from "./Screen"
import type { Runtime } from "./store"
import { toolDetail, toolLabel } from "./Thread"

type Tab = SideTab

interface Step {
  id: string
  at: number
  label: string
  detail: string
  state: "ok" | "run" | "err"
}

function stepsOf(messages: Message[], busy: boolean): Step[] {
  const results = new Map<string, boolean>()
  for (const m of messages) for (const p of m.parts) if (p.type === "tool_result") results.set(p.data.tool_call_id, p.data.is_error)
  const out: Step[] = []
  for (const m of messages) {
    if (m.role === "user") {
      const text = m.parts.find((p) => p.type === "text")
      if (text && text.type === "text") out.push({ id: m.id, at: m.created_at, label: "Görev aldı", detail: text.data.text.slice(0, 120), state: "ok" })
    }
    if (m.role !== "assistant") continue
    for (const p of m.parts) {
      if (p.type !== "tool_call") continue
      const err = results.get(p.data.id)
      out.push({
        id: p.data.id,
        at: m.created_at,
        label: toolLabel(p.data.name, err !== undefined || !busy, !!err),
        detail: toolDetail(p.data.input),
        state: err === undefined ? (busy ? "run" : "ok") : err ? "err" : "ok",
      })
    }
  }
  return out
}

function hm(sec: number): string {
  const d = new Date(sec > 1e12 ? sec : sec * 1000)
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

export function Activity(props: { dot: Dot; rt: Runtime; onClose?: () => void }) {
  const [tab, setTab] = createSignal<Tab>(panelTab())
  const pick = (t: Tab) => {
    setTab(t)
    setPanelTab(t)
  }
  createEffect(on(panelTab, (t) => setTab(t)))
  const steps = createMemo(() => stepsOf(props.rt.messages, props.rt.busy).reverse())
  const [files, { refetch }] = createResource(
    () => (tab() === "files" ? [props.rt.wsId, props.rt.messages.length] : undefined),
    async ([ws]) => (await api.files(ws as string, "")).entries ?? [],
  )
  const [session] = createResource(
    () => (tab() === "summary" ? [props.rt.wsId, props.rt.sessionId, props.rt.busy] : undefined),
    ([ws, sid]) => api.session(ws as string, sid as string),
  )

  const latest = createMemo(() => steps()[0])

  return (
    <aside class="side" classList={{ wide: tab() === "screen" }}>
      <div class="side-head">
        <div class="tabs" role="tablist">
          <button class="tab" role="tab" aria-selected={tab() === "screen"} onClick={() => pick("screen")}>
            Ekran
          </button>
          <button class="tab" role="tab" aria-selected={tab() === "activity"} onClick={() => pick("activity")}>
            Etkinlik
          </button>
          <button class="tab" role="tab" aria-selected={tab() === "auto"} onClick={() => pick("auto")}>
            7/24
          </button>
          <button class="tab" role="tab" aria-selected={tab() === "memory"} onClick={() => pick("memory")}>
            Hafıza
          </button>
          <button class="tab" role="tab" aria-selected={tab() === "files"} onClick={() => pick("files")}>
            Dosyalar
          </button>
          <button class="tab" role="tab" aria-selected={tab() === "summary"} onClick={() => pick("summary")}>
            Özet
          </button>
        </div>
        <span class="grow" />
        <Show when={props.onClose}>
          <button class="icon-btn sm" title="Paneli kapat" onClick={props.onClose}>
            {Icon.close(14)}
          </button>
        </Show>
      </div>
      <div class="side-body">
        <Switch>
          <Match when={tab() === "screen"}>
            <ScreenPane dot={props.dot} lastStep={latest()?.label ?? ""} lastDetail={latest()?.detail ?? ""} />
          </Match>
          <Match when={tab() === "activity"}>
            <Show
              when={steps().length}
              fallback={
                <div class="empty">
                  <b>Henüz bir şey yapmadı</b>
                  {props.dot.name}'e bir görev ver; attığı her adım burada görünür.
                </div>
              }
            >
              <div class="now">
                <i class={latest()?.state ?? "ok"} />
                <div>
                  <b>{props.rt.busy ? "Şu an" : "Son adım"}</b>
                  <p>{latest()?.label}</p>
                </div>
                <time>{latest() ? hm(latest()!.at) : ""}</time>
              </div>
              <ol class="log">
                <For each={steps()}>
                  {(s) => (
                    <li class={`step ${s.state}`}>
                      <span class="rail-dot">
                        <i />
                      </span>
                      <div>
                        <p>
                          <span>{s.label}</span>
                          <time>{hm(s.at)}</time>
                        </p>
                        <Show when={s.detail}>
                          <small title={s.detail}>{s.detail}</small>
                        </Show>
                      </div>
                    </li>
                  )}
                </For>
              </ol>
              <p class="log-end">{steps().length} adım</p>
            </Show>
          </Match>
          <Match when={tab() === "auto"}>
            <Automation dot={props.dot} />
          </Match>
          <Match when={tab() === "memory"}>
            <Memory dot={props.dot} />
          </Match>
          <Match when={tab() === "files"}>
            <div class="section-label" style={{ display: "flex", "align-items": "center" }}>
              <span class="grow">Çalışma alanı</span>
              <button class="icon-btn sm" title="Yenile" onClick={() => refetch()}>
                {Icon.activity(13)}
              </button>
            </div>
            <Show when={!files.loading} fallback={<div class="empty"><span class="spinner" /></div>}>
              <Show
                when={(files() ?? []).filter((f) => !f.name.startsWith(".")).length}
                fallback={
                  <div class="empty">
                    <b>Klasör boş</b>
                    {props.dot.name}'in ürettiği dosyalar burada durur.
                  </div>
                }
              >
                <For each={(files() ?? []).filter((f) => !f.name.startsWith("."))}>
                  {(f) => (
                    <div class="file-row" title={f.path}>
                      {f.is_dir ? Icon.folder(14) : Icon.file(14)}
                      <span>{f.name}</span>
                    </div>
                  )}
                </For>
              </Show>
            </Show>
            <p class="faint mono" style={{ "font-size": "11.5px", "margin-top": "14px", "overflow-wrap": "anywhere" }}>
              {props.dot.workspace}
            </p>
          </Match>
          <Match when={tab() === "summary"}>
            <Show when={session()} fallback={<div class="empty"><span class="spinner" /></div>}>
              {(s) => (
                <>
                  <div class="stat-grid">
                    <div class="stat">
                      <small>Mesaj</small>
                      <b>{s().message_count}</b>
                    </div>
                    <div class="stat">
                      <small>Maliyet</small>
                      <b>${s().cost.toFixed(2)}</b>
                    </div>
                    <div class="stat">
                      <small>Girdi token</small>
                      <b>{s().prompt_tokens.toLocaleString()}</b>
                    </div>
                    <div class="stat">
                      <small>Çıktı token</small>
                      <b>{s().completion_tokens.toLocaleString()}</b>
                    </div>
                  </div>
                  <div class="section-label">Harcama</div>
                  <SpendingChart dot={props.dot} busy={props.rt.busy} />
                  <div class="section-label">Tam yetki denetimi</div>
                  <Audit dot={props.dot} rt={props.rt} />
                  <Show when={s().todos?.length}>
                    <div class="section-label">Plan</div>
                    <For each={s().todos}>
                      {(t) => (
                        <div class={`step ${t.status === "completed" ? "ok" : t.status === "in_progress" ? "run" : ""}`}>
                          <span class="rail-dot">
                            <i />
                          </span>
                          <p style={{ "text-decoration": t.status === "completed" ? "line-through" : "none", color: t.status === "completed" ? "var(--text-3)" : "inherit" }}>
                            {t.content}
                          </p>
                        </div>
                      )}
                    </For>
                  </Show>
                </>
              )}
            </Show>
          </Match>
        </Switch>
      </div>
    </aside>
  )
}
