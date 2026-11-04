// Choosing an ElevenLabs voice, with a preview on click. The API key is
// what spends the subscription credit. Until it is saved, the dot uses
// the browser's own voice.

import { createMemo, createResource, createSignal, For, Show } from "solid-js"
import { api } from "../api/client"
import { dotsApi, type Voice } from "./api"
import { Icon } from "./icons"
import { refreshDots, state, toast } from "./store"
import { playURL, stopSpeaking } from "./voice"

export function VoicePicker(props: { workspaceId?: string; value: string; onChange: (v: { id: string; name: string } | undefined) => void; compact?: boolean }) {
  const [voices] = createResource(
    () => state.voiceConfigured,
    async (on) => (on ? await dotsApi.voices() : ([] as Voice[])),
  )
  const [q, setQ] = createSignal("")
  const [playing, setPlaying] = createSignal("")
  const list = createMemo(() => {
    const needle = q().trim().toLocaleLowerCase()
    const all = voices() ?? []
    return needle ? all.filter((v) => `${v.name} ${v.description ?? ""} ${v.accent ?? ""} ${v.gender ?? ""}`.toLocaleLowerCase().includes(needle)) : all
  })

  const preview = async (v: Voice) => {
    if (!v.preview_url) return
    if (playing() === v.id) {
      stopSpeaking()
      setPlaying("")
      return
    }
    setPlaying(v.id)
    await playURL(v.preview_url)
    setPlaying((p) => (p === v.id ? "" : p))
  }

  const [busy, setBusy] = createSignal(false)
  const [note, setNote] = createSignal("")
  const [apiKey, setApiKey] = createSignal("")
  const [, { refetch: refetchAccount }] = createResource(
    () => props.workspaceId,
    async (id) => {
      const views = await api.connections(id)
      const on = views.some((view) => view.id === "elevenlabs" && view.secrets?.api_key)
      if (on) await refreshDots()
      return on
    },
  )
  const ready = () => state.voiceConfigured
  const saveKey = async () => {
    const workspaceId = props.workspaceId
    const key = apiKey().trim()
    if (!workspaceId || !key || busy()) return
    setBusy(true)
    setNote("")
    try {
      await api.saveConnection(workspaceId, "elevenlabs", { api_key: key })
      setApiKey("")
      await refreshDots()
      void refetchAccount()
      if (!state.voiceConfigured) {
        setNote("Anahtar kaydedildi ama ses listesi açılmadı.")
        return
      }
      toast("Anahtar kaydedildi")
      setNote("Anahtar kayıtlı. Bir ses seç.")
    } catch (error) {
      setNote(error instanceof Error ? error.message : "Anahtar kaydedilemedi.")
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Show when={props.workspaceId !== undefined}>
      <div class="group" style={{ "margin-bottom": "14px" }}>
        <div class="group-row">
          <div class="text">
            <b>ElevenLabs API anahtarı</b>
            <small>{ready() ? "Anahtar kayıtlı. Sesler abonelik kredinle çalışır." : "Anahtarı yapıştır. Sesler bu anahtarın kredisiyle gelir."}</small>
          </div>
        </div>
        <div class="rule-add">
          <input
            class="input"
            type="password"
            autocomplete="off"
            spellcheck={false}
            placeholder={ready() ? "Yeni anahtar" : "sk_..."}
            value={apiKey()}
            onInput={(e) => setApiKey(e.currentTarget.value)}
            onKeyDown={(e) => e.key === "Enter" && void saveKey()}
          />
          <button class="btn sm" disabled={!apiKey().trim() || busy()} onClick={() => void saveKey()}>
            {busy() ? "Kaydediliyor…" : "Kaydet"}
          </button>
        </div>
      </div>
      <Show when={note()}>
        <p class="muted" role="status" style={{ "margin-bottom": "12px" }}>{note()}</p>
      </Show>
      </Show>
    <Show
      when={state.voiceConfigured}
      fallback={
        <div class="notice">
          {Icon.voice(16)}
          <span>Anahtar kaydedilince sesler burada listelenir. O zamana kadar dot tarayıcının sesini kullanır.</span>
        </div>
      }
    >
      <Show when={!voices.loading} fallback={<div class="empty"><span class="spinner" /></div>}>
        <Show when={!voices.error} fallback={<div class="notice">{String(voices.error?.message ?? voices.error)}</div>}>
          <input class="input" placeholder="Ses ara" value={q()} onInput={(e) => setQ(e.currentTarget.value)} style={{ "margin-bottom": "8px" }} />
          <div class="voice-list" style={{ "max-height": props.compact ? "200px" : "320px" }}>
            <For each={list()} fallback={<div class="empty">Eşleşen ses yok</div>}>
              {(v) => (
                <div class="voice-row" role="button" tabIndex={0} aria-pressed={props.value === v.id} onClick={() => props.onChange(props.value === v.id ? undefined : { id: v.id, name: v.name })}>
                  <button
                    class="icon-btn sm"
                    title="Dinle"
                    onClick={(e) => {
                      e.stopPropagation()
                      void preview(v)
                    }}
                  >
                    {playing() === v.id ? Icon.stop(12) : Icon.play(12)}
                  </button>
                  <span>
                    {v.name}
                    <small>{[v.gender, v.accent, v.description].filter(Boolean).join(" · ") || v.category}</small>
                  </span>
                  <Show when={props.value === v.id}>{Icon.check(14)}</Show>
                </div>
              )}
            </For>
          </div>
        </Show>
      </Show>
    </Show>
    </>
  )
}
