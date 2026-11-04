// Side panel views about the dot itself: what it remembers, what it costs
// and what it did while it had full access.

import { createMemo, createResource, createSignal, For, Show } from "solid-js"
import type { Message } from "../api/types"
import { dotsApi, type AccessWindow, type Dot } from "./api"
import { Icon } from "./icons"
import { toast, type Runtime } from "./store"
import { toolDetail, toolLabel } from "./Thread"

// ---------- Memory ----------

/** Memory shows the dot's long-term memory and lets the owner edit it.
 * Atlas puts this text in front of every conversation the dot has. */
export function Memory(props: { dot: Dot }) {
  const [mem, { mutate, refetch }] = createResource(() => props.dot.id, (id) => dotsApi.memory(id))
  const [draft, setDraft] = createSignal<string | undefined>()
  const [saving, setSaving] = createSignal(false)
  const text = () => draft() ?? mem()?.content ?? ""
  const limit = () => mem()?.limit ?? 3200
  const over = () => text().length > limit()
  const lines = createMemo(() =>
    (mem()?.content ?? "")
      .split("\n")
      .map((l) => l.replace(/^\s*[-*]\s*/, "").trim())
      .filter(Boolean),
  )

  const save = async () => {
    setSaving(true)
    try {
      mutate(await dotsApi.setMemory(props.dot.id, text()))
      setDraft(undefined)
      toast("Hafıza kaydedildi")
    } catch (err) {
      toast((err as Error).message, true)
    } finally {
      setSaving(false)
    }
  }

  const forget = async (line: string) => {
    const kept = (mem()?.content ?? "")
      .split("\n")
      .filter((l) => l.replace(/^\s*[-*]\s*/, "").trim() !== line)
      .join("\n")
    try {
      mutate(await dotsApi.setMemory(props.dot.id, kept))
      toast("Unutuldu")
    } catch (err) {
      toast((err as Error).message, true)
    }
  }

  return (
    <div class="memory">
      <p class="muted small-print">
        {props.dot.name} her sohbetin başında bunu okur. Sen bir şey söylediğinde buraya kendisi not alır; istediğini düzeltebilir ya da sildirebilirsin.
      </p>
      <Show when={!mem.loading} fallback={<div class="empty"><span class="spinner" /></div>}>
        <Show
          when={draft() !== undefined}
          fallback={
            <>
              <Show
                when={lines().length}
                fallback={
                  <div class="empty">
                    <b>Henüz bir şey hatırlamıyor</b>
                    Ona "bunu hatırla" de ya da buraya kendin yaz.
                  </div>
                }
              >
                <ul class="mem-list">
                  <For each={lines()}>
                    {(l) => (
                      <li>
                        <span>{l}</span>
                        <button class="icon-btn sm" title="Unut" onClick={() => void forget(l)}>
                          {Icon.close(12)}
                        </button>
                      </li>
                    )}
                  </For>
                </ul>
              </Show>
              <div class="row-actions">
                <button class="btn sm" onClick={() => setDraft(mem()?.content ?? "")}>
                  {Icon.pen(13)} Düzenle
                </button>
                <button class="btn ghost sm" onClick={() => void refetch()}>
                  Yenile
                </button>
              </div>
            </>
          }
        >
          <textarea
            class="textarea mono"
            rows={12}
            value={text()}
            placeholder={"- Sabah raporlarını kısa ister\n- Şirket adı: …"}
            onInput={(e) => setDraft(e.currentTarget.value)}
          />
          <div class={`meter ${over() ? "over" : ""}`}>
            <i style={{ width: `${Math.min(100, (text().length / limit()) * 100)}%` }} />
          </div>
          <div class="row-actions">
            <small class={over() ? "err-text" : "faint"}>
              {text().length.toLocaleString()} / {limit().toLocaleString()} karakter
            </small>
            <span class="grow" />
            <button class="btn ghost sm" onClick={() => setDraft(undefined)}>
              Vazgeç
            </button>
            <button class="btn primary sm" disabled={saving() || over()} onClick={() => void save()}>
              Kaydet
            </button>
          </div>
        </Show>
      </Show>
    </div>
  )
}

// ---------- Spending ----------

function money(v: number): string {
  if (v === 0) return "$0"
  if (v < 0.01) return "<$0.01"
  return `$${v.toFixed(2)}`
}

/** SpendingChart draws the last two weeks of spending as bars, with the
 * daily budget as a line when one is set. */
export function SpendingChart(props: { dot: Dot; busy: boolean }) {
  const [data] = createResource(() => [props.dot.id, props.busy] as const, ([id]) => dotsApi.spending(id, 14))
  const [hover, setHover] = createSignal<number | undefined>()
  const W = 300
  const H = 96
  const top = () => {
    const d = data()
    if (!d) return 1
    return Math.max(d.budget.limit_usd, ...d.days.map((x) => x.usd), 0.01) * 1.15
  }
  const total = () => data()?.days.reduce((n, d) => n + d.usd, 0) ?? 0
  return (
    <Show when={data()} fallback={<div class="empty"><span class="spinner" /></div>}>
      {(d) => {
        const bw = W / d().days.length
        return (
          <div class="spend">
            <div class="spend-head">
              <div>
                <small>Son 14 gün</small>
                <b>{money(total())}</b>
              </div>
              <div>
                <small>Bugün</small>
                <b>{money(d().budget.spent_usd)}</b>
              </div>
              <div>
                <small>Uyandırmalar</small>
                <b>
                  {d().runs} · {money(d().run_cost)}
                </b>
              </div>
            </div>
            <svg class="spend-chart" viewBox={`0 0 ${W} ${H + 16}`} role="img" aria-label="Günlük harcama" onPointerLeave={() => setHover(undefined)}>
              <For each={d().days}>
                {(day, i) => {
                  const h = () => Math.max(day.usd > 0 ? 2 : 1, (day.usd / top()) * H)
                  return (
                    <g onPointerEnter={() => setHover(i())}>
                      <rect x={i() * bw} y={0} width={bw} height={H + 16} fill="transparent" />
                      <rect
                        class={`bar ${i() === d().days.length - 1 ? "today" : ""} ${hover() === i() ? "hot" : ""} ${day.usd === 0 ? "zero" : ""}`}
                        x={i() * bw + bw * 0.18}
                        y={H - h()}
                        width={bw * 0.64}
                        height={h()}
                        rx={Math.min(3, bw * 0.2)}
                      />
                      <Show when={i() % 2 === (d().days.length - 1) % 2}>
                        <text x={i() * bw + bw / 2} y={H + 12} text-anchor="middle">
                          {day.day.slice(8)}
                        </text>
                      </Show>
                    </g>
                  )
                }}
              </For>
              <Show when={d().budget.limit_usd > 0}>
                <line class="limit" x1="0" x2={W} y1={H - (d().budget.limit_usd / top()) * H} y2={H - (d().budget.limit_usd / top()) * H} />
              </Show>
            </svg>
            <div class="spend-foot">
              <Show
                when={hover() !== undefined}
                fallback={<span class="faint">{d().budget.limit_usd > 0 ? `Kesik çizgi günlük bütçe: ${money(d().budget.limit_usd)}` : "Günlük bütçe yok; 7/24 sekmesinden koyabilirsin."}</span>}
              >
                <span>
                  {new Date(d().days[hover()!].day + "T12:00:00").toLocaleDateString([], { weekday: "long", day: "numeric", month: "long" })} ·{" "}
                  <b>{money(d().days[hover()!].usd)}</b>
                </span>
              </Show>
            </div>
          </div>
        )
      }}
    </Show>
  )
}

// ---------- Full access audit ----------

interface AuditStep {
  id: string
  at: number
  label: string
  detail: string
  error: boolean
}

function ms(v: number): number {
  return v > 1e12 ? v : v * 1000
}

function windowSteps(w: AccessWindow, messages: Message[]): AuditStep[] {
  const from = Date.parse(w.from)
  const to = w.to ? Date.parse(w.to) : Date.now()
  const results = new Map<string, boolean>()
  for (const m of messages) for (const p of m.parts) if (p.type === "tool_result") results.set(p.data.tool_call_id, p.data.is_error)
  const out: AuditStep[] = []
  for (const m of messages) {
    if (m.role !== "assistant") continue
    const at = ms(m.created_at)
    if (at < from || at > to) continue
    for (const p of m.parts) {
      if (p.type !== "tool_call") continue
      out.push({ id: p.data.id, at, label: toolLabel(p.data.name, true, !!results.get(p.data.id)), detail: toolDetail(p.data.input), error: !!results.get(p.data.id) })
    }
  }
  return out
}

function when(v: string | number): string {
  const d = new Date(v)
  return d.toLocaleString([], { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })
}

const endLabel: Record<string, string> = { revoked: "kapattın", expired: "süresi doldu", replaced: "yenilendi" }

/** Audit lists every full access window, newest first, with each action
 * the dot took in it without asking. */
export function Audit(props: { dot: Dot; rt: Runtime }) {
  const windows = createMemo(() => [...(props.dot.access_log ?? [])].reverse())
  return (
    <div class="audit">
      <Show
        when={windows().length}
        fallback={
          <div class="empty">
            <b>Tam yetki hiç açılmadı</b>
            Açtığın her seferde {props.dot.name}'in onaysız attığı adımlar burada listelenir.
          </div>
        }
      >
        <For each={windows()}>
          {(w) => {
            const steps = createMemo(() => windowSteps(w, props.rt.messages))
            return (
              <div class={`audit-win ${w.to ? "" : "open"}`}>
                <div class="audit-head">
                  {Icon.key(13)}
                  <b>{when(w.from)}</b>
                  <span class="faint">→ {w.to ? `${when(w.to)} (${endLabel[w.end ?? ""] ?? "bitti"})` : w.permanent ? "kalıcı, açık" : "açık"}</span>
                </div>
                <Show when={steps().length} fallback={<small class="faint audit-none">Bu sürede bu sohbette onaysız bir adım atmadı.</small>}>
                  <For each={steps()}>
                    {(s) => (
                      <div class={`audit-step ${s.error ? "err" : ""}`}>
                        <span class="mono faint">{new Date(s.at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>
                        <span>
                          {s.label}
                          <Show when={s.detail}>
                            <small>{s.detail}</small>
                          </Show>
                        </span>
                      </div>
                    )}
                  </For>
                </Show>
              </div>
            )
          }}
        </For>
        <p class="faint small-print">Şu anki sohbetteki adımlar görünür; 7/24 uyandırmaları da aynı sohbette çalıştığı için buradadır. Yeni sohbet başlatınca ya da bir mesajı düzenleyince eski sohbet ayrı saklanır.</p>
      </Show>
    </div>
  )
}
