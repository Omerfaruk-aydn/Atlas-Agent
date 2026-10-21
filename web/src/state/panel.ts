import { createRoot, createSignal } from "solid-js"
import { prefs, setPref } from "./prefs"

export type PanelTab = "changes" | "files" | "jobs" | "agents"

/** The file the files tab shows, with a line to reveal. */
export const [openedFile, setOpenedFile] = createRoot(() => createSignal<{ path: string; line?: number }>())

export function panelOpen() {
  return prefs().panelOpen
}

export function setPanelOpen(open: boolean) {
  setPref("panelOpen", open)
}

export function panelTab(): PanelTab {
  return prefs().panelTab
}

export function showPanel(tab: PanelTab) {
  setPref("panelTab", tab)
  setPref("panelOpen", true)
}

export function openFile(path: string, line?: number) {
  setOpenedFile({ path, line })
  showPanel("files")
}
