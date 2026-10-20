import { Popover } from "@kobalte/core/popover"
import { createMemo, createResource, createSignal, For, Show } from "solid-js"
import { api } from "../api/client"
import type { Catalog, ModelInfo } from "../api/types"
import { t } from "../i18n"
import { formatTokens } from "../lib/format"
import type { WorkspaceStore } from "../state/workspace"
import { Icon, Segmented, Switch, toast } from "./ui"

export function useCatalog(store: () => WorkspaceStore | undefined) {
  const [catalog, { mutate, refetch }] = createResource(
    () => {
      const s = store()
      return s ? { id: s.id, v: s.state.versions.settings } : undefined
    },
    ({ id }) => api.models(id),
  )
  // Read the last loaded catalog while a refresh runs. Reading the resource
  // itself would suspend the whole screen on every refresh.
  return { catalog: () => catalog.latest, mutate, refetch }
}

export function findModel(c: Catalog | undefined, provider?: string, model?: string): ModelInfo | undefined {
  return c?.providers?.find((p) => p.id === provider)?.models.find((m) => m.id === model)
}

/** ModelPicker selects the main model and its effort. The change goes
 * through the server's preferred-model path and the agent is reloaded;
 * what the agent then actually runs is read back and any difference is
 * shown. */
export function ModelPicker(props: { store: WorkspaceStore; catalog: Catalog | undefined; onChange: (c: Catalog) => void; disabled?: boolean }) {
  const [query, setQuery] = createSignal("")
  const [open, setOpen] = createSignal(false)
  const [scope, setScope] = createSignal<"global" | "workspace">("global")
  const [busy, setBusy] = createSignal(false)

  const selected = () => props.catalog?.selected?.large
  const active = () => props.catalog?.active ?? selected()
  const activeModel = () => findModel(props.catalog, active()?.provider, active()?.model)
  const mismatch = () => {
    const s = selected()
    const a = props.catalog?.active
    if (!s || !a) return undefined
    if (s.provider === a.provider && s.model === a.model) return undefined
    return { selected: `${s.provider}/${s.model}`, actual: `${a.provider}/${a.model}` }
  }
  const filtered = createMemo(() => {
    const q = query().trim().toLowerCase()
    return (props.catalog?.providers ?? [])
      .map((p) => ({ ...p, models: p.models.filter((m) => !q || `${p.name} ${m.name} ${m.id}`.toLowerCase().includes(q)) }))
      .filter((p) => p.models.length > 0)
  })

  const choose = async (provider: string, model: ModelInfo, effort?: string, think?: boolean) => {
    if (busy()) return
    setBusy(true)
    try {
      const keepEffort = effort && model.reasoning_levels?.includes(effort) ? effort : undefined
      const next = await api.selectModel(props.store.id, {
        type: "large",
        provider,
        model: model.id,
        reasoning_effort: keepEffort,
        think: model.can_reason ? think : undefined,
        scope: scope(),
      })
      props.onChange(next)
      setOpen(false)
      const a = next.active
      if (a && (a.provider !== provider || a.model !== model.id)) {
        toast(t("model.mismatch", { actual: `${a.provider}/${a.model}`, selected: `${provider}/${model.id}` }), "error", 8000)
      }
    } catch (err) {
      toast(t("errors.generic", { error: (err as Error).message }), "error")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div class="picker-row">
      <Popover open={open()} onOpenChange={setOpen} placement="top-start" gutter={6}>
        <Popover.Trigger class="chip-button" disabled={props.disabled || !props.catalog?.providers?.length} aria-label={t("model.label")}>
          <span class="chip-label">{activeModel()?.name ?? active()?.model ?? t("model.unconfigured")}</span>
          <Icon name="chevron-down" size="small" />
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content class="popover model-popover">
            <Popover.Title class="sr-only">{t("model.label")}</Popover.Title>
            <input
              class="text-input"
              type="search"
              placeholder={t("model.search")}
              aria-label={t("model.search")}
              value={query()}
              onInput={(e) => setQuery(e.currentTarget.value)}
              autofocus
            />
            <div class="model-scope">
              <span>{t("model.scope")}</span>
              <Segmented
                label={t("model.scope")}
                value={scope()}
                onChange={setScope}
                options={[
                  { value: "global", label: t("model.scopeGlobal") },
                  { value: "workspace", label: t("model.scopeWorkspace") },
                ]}
              />
            </div>
            <div class="model-list" role="listbox" aria-label={t("model.label")}>
              <For each={filtered()}>
                {(p) => (
                  <div role="group" aria-label={p.name}>
                    <div class="model-provider">{p.name}</div>
                    <For each={p.models}>
                      {(m) => {
                        const current = () => active()?.provider === p.id && active()?.model === m.id
                        return (
                          <button
                            class="model-option"
                            role="option"
                            aria-selected={current()}
                            disabled={busy()}
                            onClick={() => choose(p.id, m, active()?.reasoning_effort, active()?.think)}
                          >
                            <span class="model-name">{m.name}</span>
                            <span class="model-meta">
                              {m.context_window ? t("model.contextWindow", { n: formatTokens(m.context_window) }) : ""}
                              {m.supports_images ? ` · ${t("model.images")}` : ""}
                            </span>
                            <Show when={current()}>
                              <Icon name="check" size="small" />
                            </Show>
                          </button>
                        )
                      }}
                    </For>
                  </div>
                )}
              </For>
            </div>
          </Popover.Content>
        </Popover.Portal>
      </Popover>
      <EffortControl model={activeModel()} active={active()} disabled={props.disabled || busy()} onChange={(effort, think) => {
        const a = active()
        const m = activeModel()
        if (a && m) void choose(a.provider, m, effort, think)
      }} />
      <Show when={mismatch()}>
        {(mm) => (
          <span class="warn-text" role="status">
            {t("model.mismatch", mm())}
          </span>
        )}
      </Show>
    </div>
  )
}

/** EffortControl appears only for models that offer reasoning effort
 * levels or a thinking switch; nothing is offered that would be ignored. */
function EffortControl(props: {
  model?: ModelInfo
  active?: { reasoning_effort?: string; think?: boolean }
  disabled?: boolean
  onChange: (effort: string | undefined, think: boolean | undefined) => void
}) {
  const levels = () => props.model?.reasoning_levels ?? []
  return (
    <Show when={props.model?.can_reason}>
      <Show
        when={levels().length > 0}
        fallback={
          <Switch checked={!!props.active?.think} disabled={props.disabled} onChange={(v: boolean) => props.onChange(undefined, v)}>
            {t("timeline.reasoning")}
          </Switch>
        }
      >
        <label class="select-chip">
          <span class="sr-only">{t("model.effort")}</span>
          <span aria-hidden="true">{t("model.effort")}</span>
          <select
            disabled={props.disabled}
            value={props.active?.reasoning_effort ?? ""}
            onChange={(e) => props.onChange(e.currentTarget.value || undefined, props.active?.think)}
          >
            <option value="">{t("model.effortDefault", { level: props.model?.default_reasoning_effort ?? "—" })}</option>
            <For each={levels()}>{(l) => <option value={l}>{l}</option>}</For>
          </select>
        </label>
      </Show>
    </Show>
  )
}
