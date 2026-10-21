// The new-session (draft) screen: Atlas Agent's new-session/view.tsx layout,
// with the wordmark copied unchanged and the Atlas Agent composer. The first
// send creates the Atlas session and turns the draft tab into it.
import { Show } from "solid-js"
import type { Catalog } from "../api/types"
import { NewSessionWordmark } from "@/new-session/wordmark"
import { NEW_SESSION_CONTENT_WIDTH } from "@/new-session/layout"
import { api } from "../api/client"
import { promoteDraft, workspaceByPath } from "../state/app"
import { prefs } from "../state/prefs"
import type { WorkspaceStore } from "../state/workspace"
import { Composer, createComposerModel } from "./composer"
import { ProjectSelector } from "./project-selector"

export function NewSession(props: { draftID: string; catalog: Catalog | undefined; onCatalog: (c: Catalog) => void }) {
  const tab = () => prefs().tabs.find((t) => t.draft === props.draftID)
  const store = () => (tab() ? workspaceByPath(tab()!.path) : undefined)
  return (
    <div class="flex min-h-0 flex-1 flex-col self-stretch mx-[var(--shell-inline-inset,8px)] mb-[var(--shell-bottom-inset,8px)] mt-[var(--shell-top-inset,8px)]">
      <div class="@container relative flex flex-col min-h-0 h-full flex-1">
        <div data-component="new-session" class="relative flex-1 min-h-0 overflow-hidden rounded-[10px] bg-v2-background-bg-base shadow-[var(--v2-elevation-raised)]">
          <div class="absolute inset-x-0 top-[25.375%] flex justify-center px-6">
            <div class={NEW_SESSION_CONTENT_WIDTH}>
              <NewSessionWordmark />
              <div class="mt-8 flex flex-col gap-8">
                <Show when={store()} keyed>
                  {(s) => <DraftComposer draftID={props.draftID} store={s} catalog={props.catalog} onCatalog={props.onCatalog} />}
                </Show>
                <div class="flex min-h-7 min-w-0 flex-row flex-wrap items-center justify-center gap-0 text-v2-text-text-faint">
                  <ProjectSelector draftID={props.draftID} />
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

function DraftComposer(props: { draftID: string; store: WorkspaceStore; catalog: Catalog | undefined; onCatalog: (c: Catalog) => void }) {
  const model = createComposerModel({
    key: () => `draft:${props.draftID}`,
    store: () => props.store,
    sessionID: () => undefined,
    catalog: () => props.catalog,
    onCatalog: props.onCatalog,
    ensureSession: async () => {
      const session = await api.createSession(props.store.id)
      promoteDraft(props.draftID, props.store.id, session.id)
      return session.id
    },
  })
  return <Composer model={model} />
}
