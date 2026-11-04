// Picks the model this dot's workspace runs on. The choice is saved for
// the workspace, the same way /model does in the terminal.

import { createMemo, createResource, createSignal, For, Show } from "solid-js"
import { api } from "../api/client"
import type { Catalog, ModelInfo } from "../api/types"
import { useAccountSignIn } from "../lib/account-signin"
import { Icon } from "./icons"
import { toast } from "./store"

export function ModelSheet(props: { wsId: string; onClose: () => void }) {
  const [catalog, { mutate }] = createResource(() => props.wsId, (id) => api.models(id))
  const [query, setQuery] = createSignal("")
  const [busy, setBusy] = createSignal("")

  const signIn = useAccountSignIn(
    () => props.wsId,
    "grok-web",
    () => {
      void api.models(props.wsId).then(mutate)
      toast("Grok bağlandı")
    },
  )
  const grokConnected = () => catalog()?.providers?.some((p) => p.id === "grok-web") ?? false

  const current = () => catalog()?.active ?? catalog()?.selected?.large
  const rows = createMemo(() => {
    const q = query().trim().toLocaleLowerCase("tr")
    const out: { provider: string; providerName: string; model: ModelInfo }[] = []
    for (const p of catalog()?.providers ?? []) {
      for (const m of p.models) {
        if (!q || `${p.name} ${m.name} ${m.id}`.toLocaleLowerCase("tr").includes(q)) out.push({ provider: p.id, providerName: p.name, model: m })
      }
    }
    return out
  })

  const choose = async (provider: string, model: ModelInfo) => {
    if (busy()) return
    setBusy(model.id)
    const cur = current()
    try {
      const next: Catalog = await api.selectModel(props.wsId, {
        type: "large",
        provider,
        model: model.id,
        reasoning_effort: cur?.provider === provider && cur?.model === model.id ? cur.reasoning_effort : model.default_reasoning_effort,
        think: model.can_reason ? true : undefined,
        scope: "workspace",
      })
      mutate(next)
      toast(`${model.name} seçildi`)
      props.onClose()
    } catch (err) {
      toast((err as Error).message, true)
    } finally {
      setBusy("")
    }
  }

  return (
    <div class="scrim" onClick={(e) => e.target === e.currentTarget && props.onClose()}>
      <div class="dialog" role="dialog" aria-modal="true" aria-label="Model" style={{ width: "min(440px, 100%)", "max-height": "min(560px, calc(100dvh - 32px))" }}>
        <div class="dialog-head">
          <h2>Model</h2>
          <button class="icon-btn sm" aria-label="Kapat" onClick={props.onClose}>
            {Icon.close(14)}
          </button>
        </div>
        <div class="dialog-body" style={{ display: "flex", "flex-direction": "column", gap: "10px", "min-height": "0" }}>
          <Show when={!catalog.loading && !grokConnected()}>
            <div style={{ display: "flex", "align-items": "center", gap: "10px" }}>
              <span class="muted" style={{ flex: "1", "min-width": "0" }}>
                {signIn.login()?.state === "waiting"
                  ? `Sayfada ${signIn.login()?.user_code} kodunu onayla.`
                  : signIn.login()?.state === "failed"
                    ? signIn.login()?.error
                    : "Grok aboneliğinle bağlan."}
              </span>
              <Show
                when={signIn.login()?.state === "waiting"}
                fallback={
                  <button class="btn" disabled={signIn.starting()} onClick={() => void signIn.start()}>
                    Giriş yap
                  </button>
                }
              >
                <button class="btn" onClick={() => window.open(signIn.login()?.url, "_blank", "noopener,noreferrer")}>
                  Sayfayı aç
                </button>
                <button class="btn" onClick={signIn.cancel}>
                  Vazgeç
                </button>
              </Show>
            </div>
          </Show>
          <input class="input" placeholder="Model ara" value={query()} onInput={(e) => setQuery(e.currentTarget.value)} />
          <Show when={!catalog.loading} fallback={<span class="spinner" />}>
            <div class="model-list">
              <For each={rows()} fallback={<p class="muted">Bu ada uyan model yok.</p>}>
                {(row) => {
                  const on = () => current()?.provider === row.provider && current()?.model === row.model.id
                  return (
                    <button class="model-row" aria-pressed={on()} disabled={!!busy()} onClick={() => void choose(row.provider, row.model)}>
                      <span>
                        <b>{row.model.name}</b>
                        <small>{row.providerName}</small>
                      </span>
                      <Show when={on()}>{Icon.check(14)}</Show>
                    </button>
                  )
                }}
              </For>
            </div>
          </Show>
        </div>
      </div>
    </div>
  )
}
