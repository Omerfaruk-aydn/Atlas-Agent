// The new-session project picker: Atlas Agent's new-session/project/selector
// (copied unchanged) over the Atlas projects open in this web UI.
import { createMemo, Show } from "solid-js"
import { createPromptProjectController, PromptProjectAddButton, PromptProjectSelector } from "@/new-session/project/selector"
import { setDraftProject, setProjectDialog, workspaces } from "../state/app"
import { prefs } from "../state/prefs"

export function ProjectSelector(props: { draftID: string }) {
  const tab = () => prefs().tabs.find((t) => t.draft === props.draftID)
  const controls = createMemo(() => ({
    available: workspaces().map((w) => ({ id: w.id, name: w.state.view.name, worktree: w.state.view.path })),
    directory: tab()?.path ?? "",
    select: (worktree: string) => setDraftProject(props.draftID, worktree),
    add: () => setProjectDialog(true),
  }))
  const controller = createPromptProjectController({ controls, onDone: () => {} })
  return (
    <Show when={!controller.empty()} fallback={<PromptProjectAddButton controller={controller} />}>
      <PromptProjectSelector controller={controller} placement="bottom" />
    </Show>
  )
}
