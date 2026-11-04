// The dot's private browser, watched live. It is not the owner's desktop.

import { createEffect, createSignal, onCleanup, Show } from "solid-js"
import { dotsApi, type Dot, type ScreenState } from "./api"
import { message } from "./store"

export function ScreenPane(props: { dot: Dot; lastStep: string; lastDetail: string }) {
  const [info, setInfo] = createSignal<ScreenState>()
  const [err, setErr] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const [stamp, setStamp] = createSignal(0)
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches
  let box: HTMLImageElement | undefined
  let typed = ""

  async function refresh() {
    try {
      setInfo(await dotsApi.screen(props.dot.id))
      setErr("")
    } catch (e) {
      setErr(message(e))
    }
  }

  createEffect(() => {
    props.dot.id
    void refresh()
    const t = setInterval(() => void refresh(), 2000)
    onCleanup(() => clearInterval(t))
  })

  createEffect(() => {
    if (!reduce || !info()?.running) return
    const t = setInterval(() => setStamp(Date.now()), 1500)
    onCleanup(() => clearInterval(t))
  })

  async function run(fn: () => Promise<ScreenState>) {
    setBusy(true)
    try {
      setInfo(await fn())
      setErr("")
      setStamp(Date.now())
    } catch (e) {
      setErr(message(e))
    } finally {
      setBusy(false)
    }
  }

  function place(ev: MouseEvent) {
    const img = box
    const state = info()
    if (!img || !state?.taken_over || !img.naturalWidth) return
    const r = img.getBoundingClientRect()
    const x = Math.round((ev.clientX - r.left) * (img.naturalWidth / r.width))
    const y = Math.round((ev.clientY - r.top) * (img.naturalHeight / r.height))
    void dotsApi.screenInput(props.dot.id, { kind: "click", x, y }).catch((e) => setErr(message(e)))
  }

  const src = () => {
    if (!info()?.running) return ""
    const id = encodeURIComponent(props.dot.id)
    if (reduce) return `/api/dots/${id}/screen/frame?t=${stamp()}`
    return `/api/dots/${id}/screen/stream?t=${stamp()}`
  }

  return (
    <div class="screen-pane">
      <Show
        when={info()?.running}
        fallback={
          <div class="empty">
            <b>{info()?.starting ? "Windows açılıyor" : "Ekran kapalı"}</b>
            {info()?.note || `${props.dot.name}'in ekranı henüz açık değil.`}
            <button class="btn primary sm" style={{ "margin-top": "12px" }} disabled={busy() || info()?.starting} onClick={() => void run(() => dotsApi.startScreen(props.dot.id))}>
              {info()?.starting ? "Açılıyor…" : "Ekranı aç"}
            </button>
          </div>
        }
      >
        <div class={`screen-frame ${info()?.taken_over ? "held" : ""}`}>
          <img
            ref={box}
            src={src()}
            alt={`${props.dot.name} ekranı`}
            onClick={place}
          />
        </div>
        <p class="screen-url" title={info()?.note || info()?.url}>
          {info()?.title || "Boş sayfa"}
          <Show when={info()?.url && info()!.url !== "about:blank"}>
            <small>{info()!.url}</small>
          </Show>
          <Show when={info()?.note}>
            <small>{info()!.note}</small>
          </Show>
        </p>
      </Show>

      <Show when={props.lastStep}>
        <div class="screen-step">
          <span>Son adım</span>
          <b>{props.lastStep}</b>
          <Show when={props.lastDetail}>
            <small>{props.lastDetail}</small>
          </Show>
        </div>
      </Show>

      <div class="screen-actions">
        <Show when={info()?.running && !info()?.taken_over}>
          <button class="btn sm" disabled={busy()} onClick={() => void run(() => dotsApi.takeScreen(props.dot.id, true))}>
            Devral
          </button>
        </Show>
        <Show when={info()?.taken_over}>
          <button class="btn primary sm" disabled={busy()} onClick={() => void run(() => dotsApi.takeScreen(props.dot.id, false))}>
            Bırak
          </button>
        </Show>
        <Show when={info()?.running}>
          <button class="btn ghost sm" disabled={busy()} onClick={() => void run(() => dotsApi.stopScreen(props.dot.id))}>
            Durdur
          </button>
        </Show>
      </div>

      <Show when={info()?.taken_over}>
        <form
          class="screen-type"
          onSubmit={(e) => {
            e.preventDefault()
            const text = typed.trim()
            if (!text) return
            typed = ""
            ;(e.currentTarget.elements.namedItem("keys") as HTMLInputElement).value = ""
            void dotsApi.screenInput(props.dot.id, { kind: "type", text }).catch((err) => setErr(message(err)))
          }}
        >
          <input
            name="keys"
            placeholder="Yaz, Enter ile gönder"
            onInput={(e) => (typed = e.currentTarget.value)}
          />
        </form>
      </Show>

      <Show when={err()}>
        <p class="screen-err">{err()}</p>
      </Show>
      <p class="screen-note">{info()?.note}</p>
    </div>
  )
}
