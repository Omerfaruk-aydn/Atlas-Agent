// The dot's 24/7 side: what wakes it up, what it did each time, and what
// it is allowed to spend.

import { createResource, createSignal, For, onCleanup, Show } from "solid-js"
import { dotsApi, type Budget, type Dot, type Run, type RunStatus, type Trigger, type TriggerKind } from "./api"
import { Icon } from "./icons"
import { toast } from "./store"

const presets: { label: string; cron: string }[] = [
  { label: "Her gün 09:00", cron: "0 9 * * *" },
  { label: "Hafta içi 09:00", cron: "0 9 * * 1-5" },
  { label: "Her saat", cron: "0 * * * *" },
  { label: "Her 15 dakika", cron: "*/15 * * * *" },
  { label: "Pazartesi 08:30", cron: "30 8 * * 1" },
]

const statusText: Record<RunStatus, string> = {
  running: "Çalışıyor",
  waiting_approval: "Onay bekliyor",
  complete: "Bitti",
  failed: "Başarısız",
  cancelled: "Durduruldu",
  budget: "Bütçe doldu",
  skipped: "Atlandı",
}

const sourceText = { schedule: "Zamanlı", webhook: "Webhook", manual: "Elle", watch: "Değişiklik" } as const

const kinds: { id: TriggerKind; label: string }[] = [
  { id: "schedule", label: "Zamanlı" },
  { id: "file", label: "Dosya" },
  { id: "page", label: "Sayfa" },
  { id: "webhook", label: "Webhook" },
]

function everyText(n?: number): string {
  if (!n) return ""
  if (n < 60) return `${n} dk`
  return n % 60 ? `${Math.floor(n / 60)} sa ${n % 60} dk` : `${n / 60} sa`
}

function hostOf(u?: string): string {
  try {
    return new URL(u ?? "").host
  } catch {
    return u ?? ""
  }
}

function triggerLine(t: Trigger): string {
  const off = t.enabled ? "" : " · kapalı"
  switch (t.kind) {
    case "webhook":
      return "Webhook" + off
    case "file":
      return `${t.path} · her ${everyText(t.every)} bakar${off}`
    case "page":
      return `${hostOf(t.url)} · her ${everyText(t.every)} bakar${off}`
    default:
      return t.enabled ? `${scheduleText(t.cron)} · sıradaki ${when(t.next_fire_at)}` : `${scheduleText(t.cron)}${off}`
  }
}

function scheduleText(cron?: string): string {
  return presets.find((p) => p.cron === cron)?.label ?? cron ?? ""
}

function when(iso?: string): string {
  if (!iso) return ""
  const t = new Date(iso)
  const d = Date.now() - t.getTime()
  const time = t.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  if (Math.abs(d) < 24 * 3600 * 1000 && t.toDateString() === new Date().toDateString()) return `bugün ${time}`
  return `${t.toLocaleDateString([], { day: "numeric", month: "short" })} ${time}`
}

function span(a: string, b?: string): string {
  const s = Math.max(0, Math.round(((b ? Date.parse(b) : Date.now()) - Date.parse(a)) / 1000))
  if (s < 60) return `${s} sn`
  if (s < 3600) return `${Math.floor(s / 60)} dk ${s % 60} sn`
  return `${Math.floor(s / 3600)} sa ${Math.floor((s % 3600) / 60)} dk`
}

function usd(n: number): string {
  return `$${n.toFixed(n < 1 ? 3 : 2)}`
}

export function Automation(props: { dot: Dot }) {
  const id = () => props.dot.id
  const [triggers, { refetch: reloadTriggers }] = createResource(id, (i) => dotsApi.triggers(i))
  const [runs, { refetch: reloadRuns }] = createResource(id, (i) => dotsApi.runs(i))
  const [budget, { refetch: reloadBudget, mutate: setBudget }] = createResource(id, (i) => dotsApi.budget(i))
  const poll = setInterval(() => {
    reloadRuns()
    reloadBudget()
    reloadTriggers()
  }, 4000)
  onCleanup(() => clearInterval(poll))

  const [task, setTask] = createSignal("")
  const [creating, setCreating] = createSignal(false)
  const live = () => (runs() ?? []).some((r) => r.status === "running" || r.status === "waiting_approval")

  const start = async () => {
    const prompt = task().trim()
    if (!prompt) return
    try {
      await dotsApi.startRun(id(), prompt)
      setTask("")
      reloadRuns()
    } catch (e) {
      toast(e instanceof Error ? e.message : "Başlatılamadı")
    }
  }

  return (
    <div class="auto">
      <BudgetCard dotId={id()} budget={budget()} onChange={setBudget} />

      <div class="section-label">Görev ver</div>
      <div class="auto-task">
        <textarea
          class="input"
          rows={2}
          placeholder={`${props.dot.name} sohbetten bağımsız, arka planda çalışsın…`}
          value={task()}
          onInput={(e) => setTask(e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) start()
          }}
        />
        <button class="btn primary sm" disabled={!task().trim() || live()} onClick={start} title={live() ? "Önceki iş sürüyor" : ""}>
          Başlat
        </button>
      </div>

      <div class="section-label auto-row">
        <span class="grow">Tetikleyiciler</span>
        <button class="btn sm" onClick={() => setCreating(!creating())}>
          {creating() ? "Vazgeç" : "Yeni"}
        </button>
      </div>
      <Show when={creating()}>
        <NewTrigger
          dotId={id()}
          onDone={() => {
            setCreating(false)
            reloadTriggers()
          }}
        />
      </Show>
      <Show
        when={(triggers() ?? []).length}
        fallback={
          <Show when={!creating()}>
            <div class="empty slim">
              <b>Tetikleyici yok</b>
              Zamanlı bir iş, dosya ya da sayfa izleme veya webhook ekle; {props.dot.name} sen yokken de uyanır.
            </div>
          </Show>
        }
      >
        <For each={triggers()}>{(t) => <TriggerRow dotId={id()} t={t} onChange={() => { reloadTriggers(); reloadRuns() }} />}</For>
      </Show>

      <div class="section-label">Çalışmalar</div>
      <Show
        when={(runs() ?? []).length}
        fallback={
          <div class="empty slim">
            <b>Henüz çalışma yok</b>
            Her uyanış, ne yaptığı ve maliyetiyle burada kayıtlı kalır.
          </div>
        }
      >
        <For each={runs()}>{(r) => <RunRow dotId={id()} r={r} onChange={reloadRuns} />}</For>
      </Show>
    </div>
  )
}

function BudgetCard(props: { dotId: string; budget?: Budget; onChange: (b: Budget) => void }) {
  const [editing, setEditing] = createSignal(false)
  const [value, setValue] = createSignal("")
  const pct = () => {
    const b = props.budget
    return b && b.limit_usd > 0 ? Math.min(100, (b.spent_usd / b.limit_usd) * 100) : 0
  }
  const save = async () => {
    const n = value().trim() === "" ? 0 : Number(value().replace(",", "."))
    if (!Number.isFinite(n) || n < 0) return toast("Geçerli bir tutar yaz")
    try {
      props.onChange(await dotsApi.setBudget(props.dotId, n))
      setEditing(false)
    } catch (e) {
      toast(e instanceof Error ? e.message : "Kaydedilemedi")
    }
  }
  return (
    <div class="budget" classList={{ over: !!props.budget && props.budget.limit_usd > 0 && props.budget.spent_usd >= props.budget.limit_usd }}>
      <div class="budget-top">
        <small>Günlük bütçe</small>
        <Show
          when={!editing()}
          fallback={
            <span class="budget-edit">
              $
              <input
                class="input sm"
                inputmode="decimal"
                placeholder="Sınırsız"
                value={value()}
                ref={(el) => setTimeout(() => el.focus(), 20)}
                onInput={(e) => setValue(e.currentTarget.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") save()
                  if (e.key === "Escape") setEditing(false)
                }}
              />
              <button class="btn sm primary" onClick={save}>
                Kaydet
              </button>
            </span>
          }
        >
          <button
            class="link"
            onClick={() => {
              setValue(props.budget?.limit_usd ? String(props.budget.limit_usd) : "")
              setEditing(true)
            }}
          >
            {props.budget?.limit_usd ? "Değiştir" : "Sınır koy"}
          </button>
        </Show>
      </div>
      <b>
        {usd(props.budget?.spent_usd ?? 0)}
        <span class="faint"> / {props.budget?.limit_usd ? usd(props.budget.limit_usd) : "sınırsız"}</span>
      </b>
      <Show when={props.budget?.limit_usd}>
        <div class="bar" role="progressbar" aria-valuenow={Math.round(pct())} aria-valuemin={0} aria-valuemax={100}>
          <i style={{ width: `${pct()}%` }} />
        </div>
        <small class="faint">Dolunca dot hemen durur; yarın yeniden başlar.</small>
      </Show>
    </div>
  )
}

function NewTrigger(props: { dotId: string; onDone: () => void }) {
  const [name, setName] = createSignal("")
  const [kind, setKind] = createSignal<TriggerKind>("schedule")
  const [cron, setCron] = createSignal(presets[0].cron)
  const [custom, setCustom] = createSignal(false)
  const [path, setPath] = createSignal("")
  const [pageURL, setPageURL] = createSignal("")
  const [every, setEvery] = createSignal(0)
  const [prompt, setPrompt] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const everyOptions = () => (kind() === "file" ? [1, 5, 15, 60] : [5, 15, 60, 360, 1440])
  const everyValue = () => every() || (kind() === "file" ? 1 : 15)
  const ok = () => {
    if (!name().trim() || !prompt().trim()) return false
    switch (kind()) {
      case "schedule":
        return !!cron().trim()
      case "file":
        return !!path().trim()
      case "page":
        return /^https?:\/\/\S+/.test(pageURL().trim())
      default:
        return true
    }
  }
  const save = async () => {
    if (!ok() || busy()) return
    setBusy(true)
    try {
      const k = kind()
      await dotsApi.createTrigger(props.dotId, {
        name: name().trim(),
        kind: k,
        prompt: prompt().trim(),
        ...(k === "schedule" ? { cron: cron().trim(), timezone: Intl.DateTimeFormat().resolvedOptions().timeZone } : {}),
        ...(k === "file" ? { path: path().trim(), every: everyValue() } : {}),
        ...(k === "page" ? { url: pageURL().trim(), every: everyValue() } : {}),
      })
      props.onDone()
    } catch (e) {
      toast(e instanceof Error ? e.message : "Eklenemedi", true)
    } finally {
      setBusy(false)
    }
  }
  const placeholder = () =>
    ({
      schedule: "Uyandığında ne yapsın?",
      webhook: "Çağrı geldiğinde ne yapsın? Gelen veri görevin sonuna eklenir.",
      file: "Dosyalar değişince ne yapsın? Eşleşen dosyaların listesi göreve eklenir.",
      page: "Sayfa değişince ne yapsın? Sayfanın yeni metninden bir bölüm göreve eklenir.",
    })[kind()]
  return (
    <div class="trig-new">
      <div class="seg four">
        <For each={kinds}>
          {(k) => (
            <button
              aria-pressed={kind() === k.id}
              onClick={() => {
                setKind(k.id)
                setEvery(0)
              }}
            >
              {k.label}
            </button>
          )}
        </For>
      </div>
      <input class="input" placeholder="Adı (ör. Sabah özeti)" maxLength={60} value={name()} onInput={(e) => setName(e.currentTarget.value)} />
      <Show when={kind() === "schedule"}>
        <div class="chips">
          <For each={presets}>
            {(p) => (
              <button
                class="chip"
                aria-pressed={!custom() && cron() === p.cron}
                onClick={() => {
                  setCustom(false)
                  setCron(p.cron)
                }}
              >
                {p.label}
              </button>
            )}
          </For>
          <button class="chip" aria-pressed={custom()} onClick={() => setCustom(true)}>
            Özel
          </button>
        </div>
        <Show when={custom()}>
          <input class="input mono" placeholder="dakika saat gün ay haftanın-günü" value={cron()} onInput={(e) => setCron(e.currentTarget.value)} />
        </Show>
      </Show>
      <Show when={kind() === "file"}>
        <input class="input mono" placeholder="gelen/*.csv  ya da bir klasör: gelen" value={path()} onInput={(e) => setPath(e.currentTarget.value)} />
        <small class="faint">Yol dot'un çalışma alanına göredir. İlk bakış yalnızca şu anki hali kaydeder; sonraki her değişiklikte uyanır.</small>
      </Show>
      <Show when={kind() === "page"}>
        <input class="input mono" inputmode="url" placeholder="https://ornek.com/urun" value={pageURL()} onInput={(e) => setPageURL(e.currentTarget.value)} />
        <small class="faint">Sayfanın görünen metni karşılaştırılır; betik ve stil değişiklikleri sayılmaz.</small>
      </Show>
      <Show when={kind() === "file" || kind() === "page"}>
        <div class="chips">
          <For each={everyOptions()}>
            {(n) => (
              <button class="chip" aria-pressed={everyValue() === n} onClick={() => setEvery(n)}>
                Her {everyText(n)}
              </button>
            )}
          </For>
        </div>
      </Show>
      <textarea class="input" rows={3} placeholder={placeholder()} value={prompt()} onInput={(e) => setPrompt(e.currentTarget.value)} />
      <button class="btn primary" disabled={!ok() || busy()} onClick={save}>
        {busy() ? "Ekleniyor…" : "Ekle"}
      </button>
    </div>
  )
}

function TriggerRow(props: { dotId: string; t: Trigger; onChange: () => void }) {
  const [open, setOpen] = createSignal(false)
  const hook = () => `${location.origin}/hooks/dots/${props.t.token}`
  const act = async (fn: () => Promise<unknown>, fail: string) => {
    try {
      await fn()
      props.onChange()
    } catch (e) {
      toast(e instanceof Error ? e.message : fail)
    }
  }
  return (
    <div class="trig" classList={{ off: !props.t.enabled }}>
      <div class="trig-head">
        <button class="trig-name" onClick={() => setOpen(!open())} aria-expanded={open()}>
          <b>{props.t.name}</b>
          <small>{triggerLine(props.t)}</small>
        </button>
        <button
          class="switch"
          role="switch"
          aria-checked={props.t.enabled}
          title={props.t.enabled ? "Kapat" : "Aç"}
          onClick={() => act(() => dotsApi.updateTrigger(props.dotId, props.t.id, { enabled: !props.t.enabled }), "Değiştirilemedi")}
        />
      </div>
      <Show when={open()}>
        <div class="trig-body">
          <p>{props.t.prompt}</p>
          <Show when={props.t.kind === "webhook"}>
            <div class="hook">
              <code>{hook()}</code>
              <button
                class="btn sm"
                onClick={() => navigator.clipboard?.writeText(hook()).then(() => toast("Adres kopyalandı"))}
              >
                Kopyala
              </button>
            </div>
            <small class="faint">POST ile çağır; gövde (en çok 64 KB) göreve eklenir. Adres yalnızca bu bilgisayardan erişilebilir; dışarıdan çağırmak için kendi tünelini kur.</small>
          </Show>
          <Show when={props.t.kind === "page"}>
            <a class="faint mono small-link" href={props.t.url} target="_blank" rel="noreferrer noopener">
              {props.t.url}
            </a>
          </Show>
          <Show when={props.t.checked_at}>
            <small class="faint">Son bakış: {when(props.t.checked_at)}</small>
          </Show>
          <Show when={props.t.last_fired_at}>
            <small class="faint">
              {props.t.kind === "file" || props.t.kind === "page" ? "Son değişiklik" : "Son çalışma"}: {when(props.t.last_fired_at)}
            </small>
          </Show>
          <div class="trig-actions">
            <button class="btn sm" onClick={() => act(() => dotsApi.runTrigger(props.dotId, props.t.id), "Başlatılamadı")}>
              Şimdi çalıştır
            </button>
            <button class="btn sm danger" onClick={() => act(() => dotsApi.removeTrigger(props.dotId, props.t.id), "Silinemedi")}>
              {Icon.close(12)} Sil
            </button>
          </div>
        </div>
      </Show>
    </div>
  )
}

function RunRow(props: { dotId: string; r: Run; onChange: () => void }) {
  const [open, setOpen] = createSignal(false)
  const liveRun = () => props.r.status === "running" || props.r.status === "waiting_approval"
  return (
    <div class={`run ${props.r.status}`}>
      <button class="run-head" onClick={() => setOpen(!open())} aria-expanded={open()}>
        <span class="run-state" />
        <span class="run-title">
          <b>{props.r.trigger_name || "Görev"}</b>
          <small>
            {statusText[props.r.status]} · {sourceText[props.r.source]} · {when(props.r.started_at)}
            {props.r.ended_at || liveRun() ? ` · ${span(props.r.started_at, props.r.ended_at)}` : ""}
            {props.r.cost > 0 ? ` · ${usd(props.r.cost)}` : ""}
          </small>
        </span>
      </button>
      <Show when={open() || liveRun()}>
        <div class="run-body">
          <Show when={props.r.error}>
            <p class="err">{props.r.error}</p>
          </Show>
          <Show when={props.r.summary}>
            <p>{props.r.summary}</p>
          </Show>
          <Show when={open()}>
            <small class="faint">Görev: {props.r.goal}</small>
          </Show>
          <Show when={liveRun()}>
            <button
              class="btn sm danger"
              onClick={async () => {
                try {
                  await dotsApi.cancelRun(props.dotId, props.r.id)
                  props.onChange()
                } catch (e) {
                  toast(e instanceof Error ? e.message : "Durdurulamadı")
                }
              }}
            >
              Durdur
            </button>
          </Show>
        </div>
      </Show>
    </div>
  )
}
