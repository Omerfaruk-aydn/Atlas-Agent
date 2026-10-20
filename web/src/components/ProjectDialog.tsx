import { createEffect, createResource, createSignal, For, on, Show } from "solid-js"
import { Button } from "@atlas/ui/button"
import { useDialog } from "@atlas/ui/context/dialog"
import { Dialog, DialogHeader, DialogTitleGroup } from "@atlas/ui/dialog"
import { TextInput } from "@atlas/ui/text-input"
import { api } from "../api/client"
import { t } from "../i18n"
import { app, openProject, projectDialog, setProjectDialog } from "../state/app"

const DIALOG_ID = "project-dialog"

function ProjectForm() {
  const dialog = useDialog()
  const [path, setPath] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const [error, setError] = createSignal("")
  const [recent] = createResource(() => api.projects().catch(() => []))

  const open = async (p: string) => {
    if (!p.trim()) return
    setBusy(true)
    setError("")
    try {
      await openProject(p.trim())
      dialog.close(DIALOG_ID)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog fit>
      <DialogHeader>
        <DialogTitleGroup title={t("project.title")} description={t("project.pathHint", { platform: app.boot?.server.platform || "?" })} />
      </DialogHeader>
      <form
        class="flex flex-col gap-3 px-4 pb-4"
        style={{ width: "min(480px, 88vw)" }}
        onSubmit={(e) => {
          e.preventDefault()
          void open(path())
        }}
      >
        <label class="flex flex-col gap-1.5">
          <span class="text-[12px] text-v2-text-text-muted">{t("project.pathLabel")}</span>
          <div class="flex items-center gap-2">
            <TextInput
              appearance="base"
              class="min-w-0 flex-1 font-mono"
              value={path()}
              onInput={(e) => setPath(e.currentTarget.value)}
              placeholder={app.boot?.server.platform?.startsWith("windows") ? "D:\\code\\project" : "/home/me/project"}
              spellcheck={false}
              autocomplete="off"
              autofocus
            />
            <Button type="submit" variant="contrast" disabled={busy() || !path().trim()}>
              {busy() ? t("project.opening") : t("project.open")}
            </Button>
          </div>
        </label>
        <Show when={error()}>
          <p class="text-[12px] text-[#ef4444]" role="alert">
            {error()}
          </p>
        </Show>
        <h3 class="mt-1 text-[12px] text-v2-text-text-muted">{t("project.recent")}</h3>
        <ul class="flex max-h-[240px] flex-col overflow-y-auto">
          <For
            each={recent() ?? []}
            fallback={<li class="py-3 text-[13px] text-v2-text-text-muted">{recent.loading ? t("common.loading") : t("project.none")}</li>}
          >
            {(p) => (
              <li>
                <button
                  type="button"
                  class="flex w-full items-baseline gap-3 rounded-md px-2 py-1.5 text-left text-[13px] text-v2-text-text-base hover:bg-v2-overlay-simple-overlay-hover"
                  disabled={busy()}
                  onClick={() => void open(p.path)}
                >
                  <span class="shrink-0">{p.name}</span>
                  <span class="min-w-0 flex-1 truncate font-mono text-v2-text-text-muted">{p.path}</span>
                </button>
              </li>
            )}
          </For>
        </ul>
      </form>
    </Dialog>
  )
}

/** ProjectDialog opens a folder of the computer running Atlas. The browser
 * may be on another device, so it never offers the browser's own folders. */
export function ProjectDialog() {
  const dialog = useDialog()
  createEffect(
    on(projectDialog, (open) => {
      if (open) dialog.show(() => <ProjectForm />, () => setProjectDialog(false), DIALOG_ID)
      else dialog.close(DIALOG_ID)
    }, { defer: true }),
  )
  return null
}
