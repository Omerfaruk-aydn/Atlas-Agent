import { createEffect, For, on } from "solid-js"
import { Button } from "@atlas/ui/button"
import { useDialog } from "@atlas/ui/context/dialog"
import { Dialog, DialogFooter, DialogHeader, DialogTitleGroup } from "@atlas/ui/dialog"
import { t, type MessageKey } from "../i18n"
import { helpDialog, setHelpDialog } from "../state/app"

const DIALOG_ID = "help-dialog"

const shortcuts: [string, MessageKey][] = [
  ["Enter", "help.send"],
  ["Shift + Enter", "help.newline"],
  ["Esc Esc", "help.stop"],
  ["Ctrl + B", "help.home"],
  ["Ctrl + \\", "help.panel"],
  ["Ctrl + Shift + O", "help.openProject"],
]

function HelpBody() {
  const dialog = useDialog()
  return (
    <Dialog fit>
      <DialogHeader hideClose>
        <DialogTitleGroup title={t("home.help")} description={t("help.description")} />
      </DialogHeader>
      <dl class="flex flex-col gap-1.5 px-4 pb-2" style={{ width: "min(520px, 88vw)" }}>
        <For each={shortcuts}>
          {([keys, label]) => (
            <div class="flex items-center gap-3 text-[13px]">
              <dt class="w-40 shrink-0">
                <kbd class="rounded border border-v2-border-border-muted px-1.5 py-0.5 font-mono text-[12px] text-v2-text-text-base">{keys}</kbd>
              </dt>
              <dd class="text-v2-text-text-muted">{t(label)}</dd>
            </div>
          )}
        </For>
      </dl>
      <DialogFooter>
        <Button variant="ghost" onClick={() => dialog.close(DIALOG_ID)}>
          {t("common.close")}
        </Button>
      </DialogFooter>
    </Dialog>
  )
}

/** HelpDialog lists keyboard shortcuts and how the web UI relates to the
 * terminal UI. */
export function HelpDialog() {
  const dialog = useDialog()
  createEffect(
    on(helpDialog, (open) => {
      if (open) dialog.show(() => <HelpBody />, () => setHelpDialog(false), DIALOG_ID)
      else dialog.close(DIALOG_ID)
    }, { defer: true }),
  )
  return null
}
