// The composer: Atlas Agent's composer/composer.tsx and editor (copied under
// ./app/composer) over an Atlas-backed model. Atlas Agent's editor, mentions,
// history and attachment handling run unchanged; sending, stopping, the
// queue and the model/effort controls go to the Atlas server.

import { createEffect, createMemo, createResource, createSignal, For, on, onCleanup, onMount, Show, untrack, type Accessor } from "solid-js"
import { createMediaQuery } from "@solid-primitives/media"
import { Button } from "@atlas/ui/button"
import { useDialog } from "@atlas/ui/context/dialog"
import { Dialog, DialogFooter, DialogHeader, DialogTitleGroup } from "@atlas/ui/dialog"
import { TextInput } from "@atlas/ui/text-input"
import { Icon } from "@atlas/ui/icon"
import { ImagePreview } from "@atlas/ui/image-preview"
import { Keybind } from "@atlas/ui/keybind"
import { Tooltip } from "@atlas/ui/tooltip"
import { ComposerEditor } from "@/composer/editor/editor"
import { createComposerEditor, type ComposerEditorModel } from "@/composer/editor/interaction"
import { createMemoryComposerState } from "@/composer/state"
import { createComposerHistory } from "@/composer/history/store"
import { useAttachmentDestination } from "@/composer/attachments/destination"
import { isAttachment } from "@/composer/prompt-parts"
import type { ComposerControls, ComposerQueue } from "@/composer/adapter"
import type { ComposerSuggestion } from "@/composer/types"
import type { ModelChoice, ModelSelection } from "@/providers/models/selection"
import { ProviderModelIcon } from "@/providers/models/provider-group"
import { useLanguage } from "@/runtime/i18n/language"
import { api, ApiError, newRunID } from "../api/client"
import type { Attachment, Catalog, ModelInfo, SlashCommand } from "../api/types"
import type { WorkspaceStore } from "../state/workspace"
import { goBack, goHome, newDraft, openSettings, setHelpDialog } from "../state/app"
import { panelOpen, setPanelOpen } from "../state/panel"
import { prefs, setPref } from "../state/prefs"
import { cliCommands, type CliCommand } from "./cli-commands"
import { findModel } from "../components/ModelPicker"
import { ContextRing, type ContextUsage } from "./context-ring"
import { ModelPopover, type ModelPicker } from "./model-popover"
import { openPaletteDialog, openWorkflowView, runPaletteAction, type PaletteContext } from "./palette"
import { showToast } from "./toast"

export type ComposerModel = ComposerEditorModel & { readonly model: ComposerControls["model"]; readonly picker: ModelPicker; readonly usage: Accessor<ContextUsage> }

const mac = typeof navigator === "object" && /(Mac|iPod|iPhone|iPad)/.test(navigator.platform)
const mod = mac ? "⌘" : "Ctrl"

// Drafts live per session (or per draft tab) for the page's lifetime, so
// switching tabs keeps what was typed, as Atlas Agent's persisted drafts do.
const drafts = new Map<string, ReturnType<typeof createMemoryComposerState>>()

function draftState(key: string) {
  let state = drafts.get(key)
  if (!state) {
    state = createMemoryComposerState()
    drafts.set(key, state)
  }
  return state
}

function modelSelection(catalog: Accessor<Catalog | undefined>, store: Accessor<WorkspaceStore>, onCatalog: (c: Catalog) => void): ModelSelection {
  const active = () => catalog()?.active ?? catalog()?.selected?.large
  const current = createMemo<ModelChoice | undefined>(() => {
    const a = active()
    if (!a) return undefined
    const provider = catalog()?.providers?.find((p) => p.id === a.provider)
    const model = provider?.models.find((m) => m.id === a.model)
    return {
      id: a.model,
      name: model?.name ?? a.model,
      provider: { id: a.provider, canonical: a.provider, name: provider?.name ?? a.provider },
      capabilities: { input: { image: !!model?.supports_images, pdf: false } },
      variants: model?.reasoning_levels ?? [],
    }
  })
  return {
    current,
    variant: {
      current: () => active()?.reasoning_effort || undefined,
      list: () => current()?.variants ?? [],
      set: (value) => {
        const a = active()
        if (!a) return
        void api
          .selectModel(store().id, { type: "large", provider: a.provider, model: a.model, reasoning_effort: value ?? "", scope: "global" })
          .then(onCatalog)
          .catch((err) => showToast({ variant: "error", title: String((err as Error).message ?? err) }))
      },
    },
  }
}

async function attachmentPayload(part: { filename: string; mime: string; blob?: { url: string } }): Promise<Attachment | undefined> {
  const url = part.blob?.url
  if (!url) return undefined
  const buf = await (await fetch(url)).arrayBuffer()
  let binary = ""
  const bytes = new Uint8Array(buf)
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return { file_name: part.filename, mime_type: part.mime, content: btoa(binary) }
}

/** createComposerModel builds the composer for a session, or for a draft
 * tab when `sessionID` is empty; `ensureSession` then creates the session
 * on the first send. */
export function createComposerModel(input: {
  key: Accessor<string>
  store: Accessor<WorkspaceStore>
  sessionID: Accessor<string | undefined>
  catalog: Accessor<Catalog | undefined>
  onCatalog: (c: Catalog) => void
  ensureSession?: () => Promise<string>
}): ComposerModel {
  const language = useLanguage()
  const dialog = useDialog()
  const history = createComposerHistory()
  const prompt = createMemo(() => draftState(input.key()))
  const [sending, setSending] = createSignal(false)

  const state = () => {
    const sid = input.sessionID()
    return sid ? input.store().runState(sid) : "idle"
  }
  const working = () => ["running", "starting", "question", "permission"].includes(state())
  const queued = () => {
    const sid = input.sessionID()
    return sid ? (input.store().state.status[sid]?.queued ?? 0) : 0
  }

  const selection = modelSelection(input.catalog, input.store, input.onCatalog)
  const controls = createMemo<ComposerControls>(() => ({
    agents: { available: [], options: [], current: "", visible: false, select: () => {} },
    model: { selection, paid: true, loading: !input.catalog() },
  }))

  const queue: ComposerQueue = {
    count: queued,
    undoing: () => false,
    delivery: () => "queue",
    alternate: () => undefined,
    editing: () => undefined,
    confirmEdit: () => {},
    cancelEdit: () => {},
    editFirst: () => false,
  }

  const blank = () => {
    const parts = prompt().current()
    const text = parts.map((part) => ("content" in part ? part.content : "")).join("")
    return text.trim().length === 0 && !parts.some(isAttachment)
  }

  const placeholder = () => {
    if (controller.state.mode === "shell") return language.t("prompt.placeholder.shell", { example: "git status" })
    if (working() || queued() > 0) return language.t("ui.promptInput.placeholder.followUp", { slash: "/", at: "@" })
    return language.t("ui.promptInput.placeholder.normal", { slash: "/", at: "@" })
  }

  const submit = async () => {
    if (untrack(sending)) return
    const parts = prompt().current()
    const text = parts
      .filter((part) => !isAttachment(part))
      .map((part) => ("content" in part ? part.content : ""))
      .join("")
      .trim()
    const images = parts.filter(isAttachment)
    if (!text && images.length === 0) return
    const shell = controller.state.mode === "shell"
    setSending(true)
    let sid = input.sessionID()
    try {
      if (!sid) sid = await input.ensureSession?.()
      if (!sid) return
      if (shell) {
        await api.shell(input.store().id, sid, text)
      } else {
        const attachments = (await Promise.all(images.map((part) => attachmentPayload(part as never)))).filter((a): a is Attachment => !!a)
        const runID = newRunID()
        input.store().markStarting(sid, runID)
        await api.prompt(input.store().id, sid, text, runID, attachments)
      }
      controller.addHistory(parts, shell ? "shell" : "normal")
      prompt().reset()
      controller.resetHistory()
    } catch (err) {
      if (sid) input.store().clearStarting(sid)
      showToast({
        variant: "error",
        title: language.t(shell ? "prompt.toast.shellSendFailed.title" : "prompt.toast.promptSendFailed.title"),
        description: err instanceof ApiError ? err.message : String((err as Error)?.message ?? err),
      })
    } finally {
      setSending(false)
    }
  }

  const stop = async () => {
    const sid = input.sessionID()
    if (!sid) return
    try {
      await api.cancel(input.store().id, sid)
    } catch (err) {
      showToast({ variant: "error", title: language.t("common.requestFailed"), description: String((err as Error)?.message ?? err) })
    }
  }

  // Files are found by walking the project a few levels down, skipping
  // dependency and hidden folders, and kept for a short while so typing
  // after "@" filters a list instead of asking the server again.
  type Entry = { path: string; dir: boolean }
  const index = { id: "", at: 0, entries: undefined as Promise<Entry[]> | undefined }
  const [indexed, setIndexed] = createSignal<Entry[]>([])
  const skipped = new Set(["node_modules", ".git", "dist", "build", "vendor", "target", "__pycache__"])
  const walk = async (id: string): Promise<Entry[]> => {
    const out: Entry[] = []
    let level = [""]
    for (let depth = 0; depth < 4 && level.length > 0 && out.length < 4000; depth++) {
      const listings = await Promise.all(level.map((dir) => api.files(id, dir).catch(() => undefined)))
      const next: string[] = []
      listings.forEach((listing) => {
        for (const entry of listing?.entries ?? []) {
          if (entry.name.startsWith(".") && entry.name !== ".atlas") continue
          out.push({ path: entry.path, dir: entry.is_dir })
          if (entry.is_dir && !skipped.has(entry.name)) next.push(entry.path)
        }
      })
      level = next.slice(0, 60)
    }
    return out
  }
  const entries = (): Promise<Entry[]> => {
    const id = input.store().id
    if (index.id !== id || !index.entries || Date.now() - index.at > 30_000) {
      index.id = id
      index.at = Date.now()
      index.entries = walk(id)
      void index.entries.then(setIndexed)
    }
    return index.entries
  }
  createEffect(on(() => input.store().id, () => void entries()))
  const suggestion = (e: Entry, recent?: boolean): ComposerSuggestion => {
    const path = e.dir ? `${e.path}/` : e.path
    return { id: `file:${path}`, kind: "file", label: path, path, recent, mention: { type: "file", path, content: `@${path}`, start: 0, end: 0 } }
  }
  // Shown as soon as "@" is typed: the files nearest the project root.
  const nearby = createMemo<ComposerSuggestion[]>(() =>
    indexed()
      .filter((e) => !e.dir)
      .sort((a, b) => a.path.split("/").length - b.path.split("/").length || a.path.localeCompare(b.path))
      .slice(0, 8)
      .map((e) => suggestion(e, true)),
  )
  const searchContextFiles = async (query: string): Promise<ComposerSuggestion[]> => {
    const q = query.toLowerCase().replaceAll("\\", "/")
    try {
      const all = await entries()
      const rank = (path: string) => {
        const lower = path.toLowerCase()
        const name = lower.slice(lower.lastIndexOf("/") + 1)
        return name.startsWith(q) ? 0 : name.includes(q) ? 1 : 2
      }
      return all
        .filter((e) => e.path.toLowerCase().replaceAll("\\", "/").includes(q))
        .sort((a, b) => rank(a.path) - rank(b.path) || a.path.length - b.path.length)
        .slice(0, 50)
        .map((e) => suggestion(e))
    } catch {
      return []
    }
  }

  // The "/" menu: a few client commands plus what the server lists, which
  // is what the terminal UI offers in its command palette.
  const [slash] = createResource(
    () => input.store().id,
    (id) => api.commands(id).catch(() => [] as SlashCommand[]),
  )
  const builtins = createMemo<ComposerSuggestion[]>(() =>
    cliCommands.map((c) => ({ id: `builtin:${c.name}`, kind: "command" as const, label: `/${c.name}`, trigger: c.name, title: c.name, description: c.description })),
  )
  const commands = createMemo<ComposerSuggestion[]>(() => [
    ...builtins(),
    ...(slash.latest ?? []).map((c) => ({ id: `command:${c.id}`, kind: "command" as const, label: `/${c.name}`, trigger: c.name, title: c.name, description: c.description })),
  ])

  const setPrompt = (text: string) => {
    prompt().set([{ type: "text", content: text, start: 0, end: text.length }], text.length)
    requestAnimationFrame(() => document.querySelector<HTMLElement>('[data-component="composer"] [contenteditable]')?.focus())
  }

  const runCommand = async (command: SlashCommand, args?: Record<string, string>) => {
    try {
      const result = await api.expandCommand(input.store().id, command.id, args)
      setPrompt(result.prompt)
    } catch (err) {
      showToast({ variant: "error", title: language.t("common.requestFailed"), description: String((err as Error)?.message ?? err) })
    }
  }

  const palette: PaletteContext = {
    store: input.store,
    sessionID: input.sessionID,
    catalog: input.catalog,
    usage: () => usage(),
    history: () =>
      history.entries("normal").map((entry) =>
        entry.prompt.map((part) => ("content" in part ? part.content : "")).join("").trim(),
      ).filter(Boolean),
    setPrompt: (text) => setPrompt(text),
  }

  const runBuiltin = (command: CliCommand) => {
    const action = command.action
    if (action.type === "new") return newDraft(input.store().state.view.path)
    if (action.type === "home") return goHome()
    if (action.type === "help") return setHelpDialog(true)
    if (action.type === "settings") return openSettings(action.section)
    if (action.type === "prompt") return setPrompt(action.text)
    if (action.type === "dialog") return openPaletteDialog(action.name, palette, dialog)
    if (action.type === "run") return void runPaletteAction(action.name, palette)
    if (action.type === "workflow") return openWorkflowView(palette, dialog, action.title, action.section)
    if (action.type === "ui") {
      switch (action.name) {
        case "sidebar":
          return setPanelOpen(!panelOpen())
        case "pills":
          setPref("pills", !prefs().pills)
          return showToast({ title: prefs().pills ? "To-do and queue strip shown" : "To-do and queue strip hidden" })
        case "attach":
          return controller.attach()
        case "back":
          if (!goBack()) showToast({ title: "Nothing to go back to", description: "You have not stepped away from another session." })
          return
        case "transparent":
          return showToast({ title: "Not needed in the browser", description: "The web app uses its own background; choose a theme in Settings." })
        case "quit":
          return showToast({ title: "Atlas keeps running", description: "Close this tab when you are done; runs in progress carry on in the server." })
        case "editor":
          return showToast({ title: "Not available in the browser", description: "Write in the prompt box; Shift+Enter adds a line." })
      }
    }
    showToast({ title: `/${command.name}`, description: "This works in the Atlas terminal; it needs your computer, which a browser does not have." })
  }

  // The terminal's keys for the same commands. A browser keeps a few of
  // them for itself; those that it lets through are taken here.
  const shortcuts: Record<string, string> = {
    "ctrl+n": "new",
    "ctrl+l": "model",
    "ctrl+y": "yolo",
    "ctrl+shift+y": "permissions",
    "ctrl+shift+r": "rewind",
    "alt+a": "agents",
    f5: "search",
    f6: "usage",
    f7: "snippets",
    f8: "history",
    f9: "find",
  }
  onMount(() => {
    const onKey = (event: KeyboardEvent) => {
      const key = `${event.ctrlKey || event.metaKey ? "ctrl+" : ""}${event.altKey ? "alt+" : ""}${event.shiftKey ? "shift+" : ""}${event.key.toLowerCase()}`
      const name = shortcuts[key]
      const command = name ? cliCommands.find((c) => c.name === name) : undefined
      if (!command || dialog.active) return
      event.preventDefault()
      runBuiltin(command)
    }
    window.addEventListener("keydown", onKey)
    onCleanup(() => window.removeEventListener("keydown", onKey))
  })

  const onSuggestionSelect = (item: ComposerSuggestion): (() => void) | void => {
    if (item.kind !== "command") return
    if (item.id.startsWith("builtin:")) {
      const builtin = cliCommands.find((c) => `builtin:${c.name}` === item.id)
      return builtin ? () => runBuiltin(builtin) : undefined
    }
    const command = slash()?.find((c) => `command:${c.id}` === item.id)
    if (!command) return
    return () => {
      if (command.arguments?.length) {
        dialog.show(() => <CommandArguments command={command} onRun={(args) => void runCommand(command, args)} />)
        return
      }
      void runCommand(command)
    }
  }

  const controller = createComposerEditor({
    store: () => prompt().store,
    history: {
      entries: (mode) => history.entries(mode).map((entry) => ({ prompt: entry.prompt, metadata: entry.comments })),
      add: (value, mode) => history.add(value, mode, []),
      capture: () => [],
      restore: () => {},
    },
    commands,
    context: nearby,
    searchContextFiles,
    onSuggestionSelect,
    openAttachment: (attachment) => {
      if (attachment.type !== "image" || !attachment.blob.url) return
      dialog.show(() => <ImagePreview src={attachment.blob.url} alt={attachment.filename} />)
    },
    attachments: {
      directory: () => input.store().state.view.path,
      destination: useAttachmentDestination(controls),
      isDialogActive: () => !!dialog.active,
      duplicate: () => showToast({ title: language.t("prompt.toast.attachmentDuplicate.title") }),
      onUploadError: (error) => showToast({ variant: "error", title: language.t("prompt.toast.uploadFailed.title"), description: String((error as Error)?.message ?? error) }),
      onError: (error) => showToast({ variant: "error", title: language.t("common.requestFailed"), description: error instanceof Error ? error.message : String(error) }),
    },
    view: {
      placeholder,
      variant: {
        options: () => ["default", ...selection.variant.list()].map((value) => ({ id: value, label: value })),
        current: () => selection.variant.current() ?? "default",
        onSelect: (value) => selection.variant.set(value === "default" ? undefined : value),
      },
      submit: {
        available: () => !sending(),
        stopping: () => working() && blank(),
        working,
        queue,
        onSubmit: () => void submit(),
        onStop: () => void stop(),
      },
    },
  })

  const picker: ModelPicker = {
    catalog: input.catalog,
    active: () => input.catalog()?.active ?? input.catalog()?.selected?.large,
    choose: async (provider: string, model: ModelInfo) => {
      const effort = picker.active() && input.catalog()?.selected?.large?.reasoning_effort
      try {
        const next = await api.selectModel(input.store().id, {
          type: "large",
          provider,
          model: model.id,
          reasoning_effort: effort && model.reasoning_levels?.includes(effort) ? effort : undefined,
          scope: "global",
        })
        input.onCatalog(next)
        const a = next.active
        if (a && (a.provider !== provider || a.model !== model.id)) showToast({ variant: "error", title: `${a.provider}/${a.model}`, description: `${provider}/${model.id}` })
      } catch (err) {
        showToast({ variant: "error", title: language.t("common.requestFailed"), description: String((err as Error)?.message ?? err) })
      }
    },
  }
  // What the session has used of the active model's context window.
  const usage = createMemo<ContextUsage>(() => {
    const sid = input.sessionID()
    const session = sid ? input.store().state.sessions.find((item) => item.id === sid) : undefined
    const active = input.catalog()?.active ?? input.catalog()?.selected?.large
    return {
      tokens: session ? session.prompt_tokens + session.completion_tokens : 0,
      window: findModel(input.catalog(), active?.provider, active?.model)?.context_window,
      cost: session?.cost ?? 0,
      messages: session?.message_count ?? 0,
    }
  })
  Object.defineProperty(controller, "usage", { value: usage })
  Object.defineProperty(controller, "picker", { value: picker })
  Object.defineProperty(controller, "model", { get: () => controls().model })
  return controller as ComposerModel
}

/** Composer is Atlas Agent's composer/composer.tsx. */
export function Composer(props: { class?: string; model: ComposerModel; borderUnderlay?: boolean; readOnly?: boolean; suggestionBoundary?: () => HTMLElement | undefined }) {
  const language = useLanguage()
  return (
    <div class="flex flex-col gap-3">
      <ComposerEditor
        controller={props.model}
        borderUnderlay={props.borderUnderlay}
        readOnly={props.readOnly}
        class={props.class}
        modelControlsVisible={!props.model.model.loading}
        actionsLead={<ContextRing usage={props.model.usage()} />}
        attachKeybind={[mod, "U"]}
        attachShortcut={`${mod}+U`}
        alternateKeybind={[mod, "↵"]}
        exitShellKeybind={["Esc"]}
        suggestionBoundary={props.suggestionBoundary}
        modelControl={
          <Show when={!props.model.model.loading}>
            <ModelPopover
              picker={props.model.picker}
              title={language.t("command.model.choose")}
              provider={props.model.model.selection.current()?.provider}
              modelName={props.model.model.selection.current()?.name ?? language.t("dialog.model.select.title")}
            />
          </Show>
        }
      />
    </div>
  )
}

function CommandArguments(props: { command: SlashCommand; onRun: (args: Record<string, string>) => void }) {
  const dialog = useDialog()
  const language = useLanguage()
  const [values, setValues] = createSignal<Record<string, string>>({})
  const missing = () => (props.command.arguments ?? []).some((a) => a.required && !values()[a.id]?.trim())
  const run = () => {
    if (missing()) return
    props.onRun(values())
    dialog.close()
  }
  return (
    <Dialog fit>
      <DialogHeader hideClose>
        <DialogTitleGroup title={`/${props.command.name}`} description={props.command.description} />
      </DialogHeader>
      <form
        class="flex flex-col gap-3 px-4 pb-2"
        onSubmit={(event) => {
          event.preventDefault()
          run()
        }}
      >
        <For each={props.command.arguments}>
          {(arg, index) => (
            <label class="flex flex-col gap-1 text-[13px] text-v2-text-text-base">
              <span>{arg.title}</span>
              <TextInput
                appearance="base"
                value={values()[arg.id] ?? ""}
                onInput={(event) => setValues((prev) => ({ ...prev, [arg.id]: event.currentTarget.value }))}
                placeholder={arg.description}
                autofocus={index() === 0}
                spellcheck={false}
                autocomplete="off"
              />
            </label>
          )}
        </For>
      </form>
      <DialogFooter>
        <Button variant="ghost" onClick={() => dialog.close()}>
          {language.t("common.cancel")}
        </Button>
        <Button variant="submit" disabled={missing()} onClick={run}>
          {language.t("common.continue")}
        </Button>
      </DialogFooter>
    </Dialog>
  )
}
