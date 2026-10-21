// The composer's model control: Atlas Agent's model chip (button with the
// provider icon and name) opening a searchable list of the models the Atlas
// server offers. Choosing one goes through the server's preferred-model path.

import { createMemo, createSignal, For, Show } from "solid-js"
import { Button } from "@atlas/ui/button"
import { Icon } from "@atlas/ui/icon"
import { Popover } from "@atlas/ui/popover"
import { TextInput } from "@atlas/ui/text-input"
import { ProviderModelIcon } from "@/providers/models/provider-group"
import { useLanguage } from "@/runtime/i18n/language"
import type { Catalog, ModelInfo } from "../api/types"

export type ModelPicker = {
  catalog: () => Catalog | undefined
  active: () => { provider: string; model: string } | undefined
  choose: (provider: string, model: ModelInfo) => Promise<void>
}

export function ModelPopover(props: { picker: ModelPicker; provider?: { id: string; canonical?: string; name: string }; modelName: string; title: string }) {
  const language = useLanguage()
  const [open, setOpen] = createSignal(false)
  const [query, setQuery] = createSignal("")
  const [busy, setBusy] = createSignal(false)

  const groups = createMemo(() => {
    const q = query().trim().toLowerCase()
    return (props.picker.catalog()?.providers ?? [])
      .map((p) => ({ ...p, models: p.models.filter((m) => !q || `${p.name} ${m.name} ${m.id}`.toLowerCase().includes(q)) }))
      .filter((p) => p.models.length > 0)
  })

  const choose = async (provider: string, model: ModelInfo) => {
    if (busy()) return
    setBusy(true)
    try {
      await props.picker.choose(provider, model)
      setOpen(false)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Popover
      open={open()}
      onOpenChange={(next: boolean) => {
        setOpen(next)
        if (next) setQuery("")
      }}
      placement="top-start"
      gutter={6}
      triggerAs={Button}
      triggerProps={{
        "data-action": "composer-model",
        "data-control-type": "popover",
        variant: "ghost-muted",
        size: "normal",
        class: "min-w-0 max-w-[220px] justify-start ![font-weight:440] group",
        style: { height: "28px" },
        "aria-label": props.title,
      }}
      trigger={
        <>
          <Show when={props.provider}>
            {(provider) => <ProviderModelIcon provider={provider()} class="shrink-0 opacity-40 transition-opacity duration-150 group-hover:opacity-100" />}
          </Show>
          <span class="truncate leading-4">{props.modelName}</span>
          <span class="-ml-0.5 -mr-1 flex shrink-0">
            <Icon name="chevron-down" />
          </span>
        </>
      }
      class="w-[340px] max-w-[calc(100vw-24px)]"
    >
      <div class="flex max-h-[min(420px,60vh)] flex-col gap-1" data-component="composer-model-list">
        <div class="px-1 pt-1">
          <TextInput
            type="search"
            appearance="base"
            leadingIcon={<Icon name="magnifying-glass" size="small" />}
            value={query()}
            onInput={(event) => setQuery(event.currentTarget.value)}
            placeholder={language.t("dialog.model.search.placeholder")}
            aria-label={language.t("dialog.model.search.placeholder")}
            autofocus
            spellcheck={false}
            autocomplete="off"
          />
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto px-1 pb-1" role="listbox" aria-label={props.title}>
          <Show when={groups().length > 0} fallback={<div class="px-2 py-3 text-[13px] text-v2-text-text-muted">{language.t("palette.empty")}</div>}>
            <For each={groups()}>
              {(provider) => (
                <div role="group" aria-label={provider.name} class="pb-1">
                  <div class="px-2 pb-0.5 pt-2 text-[11px] text-v2-text-text-faint [font-weight:530]">{provider.name}</div>
                  <For each={provider.models}>
                    {(model) => {
                      const current = () => props.picker.active()?.provider === provider.id && props.picker.active()?.model === model.id
                      return (
                        <button
                          type="button"
                          role="option"
                          aria-selected={current()}
                          disabled={busy()}
                          class="flex h-7 w-full min-w-0 items-center gap-2 rounded-[6px] px-2 text-start text-[13px] text-v2-text-text-base hover:bg-v2-overlay-simple-overlay-hover disabled:opacity-60"
                          classList={{ "bg-v2-overlay-simple-overlay-pressed": current() }}
                          onClick={() => void choose(provider.id, model)}
                        >
                          <span class="min-w-0 flex-1 truncate">{model.name}</span>
                          <Show when={current()}>
                            <Icon name="check-small" size="small" />
                          </Show>
                        </button>
                      )
                    }}
                  </For>
                </div>
              )}
            </For>
          </Show>
        </div>
      </div>
    </Popover>
  )
}
