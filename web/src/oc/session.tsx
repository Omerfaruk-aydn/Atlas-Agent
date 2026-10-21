// The session screen: Atlas Agent's session/screen.tsx layout with its pieces
// copied from Atlas Agent — the session-ui timeline row renderer, the session
// title header, the composer dock with the permission and question docks,
// and the session-review-v2 panel — over Atlas session data.

import { createEffect, createMemo, createResource, createSignal, For, lazy, on, onCleanup, Show, type ParentProps } from "solid-js"
import { createStore } from "solid-js/store"
import { createMediaQuery } from "@solid-primitives/media"
import { Icon } from "@atlas/ui/icon"
import { IconButton } from "@atlas/ui/icon-button"
import { Keybind } from "@atlas/ui/keybind"
import { Tooltip } from "@atlas/ui/tooltip"
import { Menu } from "@atlas/ui/menu"
import { Tabs } from "@atlas/ui/tabs"
import { ScrollView } from "@atlas/ui/scroll-view"
import { ResizeHandle } from "@atlas/ui/resize-handle"
import { DiffChanges } from "@atlas/ui/diff-changes"
import { TextShimmer } from "@atlas/ui/text-shimmer"
import { FileComponentProvider } from "@atlas/ui/context/file"
import { displayName, getProjectAvatarSource, getProjectAvatarVariant, ProjectAvatar } from "@atlas/ui/project-avatar"
import { DataProvider } from "@atlas/session-ui/context"
import { MarkdownProvider } from "@atlas/session-ui/context/markdown"
import { createReactiveTimelineProjection, TimelineRow } from "@atlas/session-ui/timeline/projection"
import { createSessionTimelineRowRenderer } from "@atlas/session-ui/timeline/row"
import {
  SessionReviewV2,
  SessionReviewV2Sidebar,
  SessionReviewV2SidebarToggle,
  type SessionReviewExpandMode,
} from "@atlas/session-ui/v2/session-review-v2"
import { SessionReviewFilePreviewV2 } from "@atlas/session-ui/v2/session-review-file-preview-v2"
import { SessionReviewEmptyChangesV2 } from "@atlas/session-ui/v2/session-review-empty-changes-v2"
import { ReviewList, ReviewTree, applyFileListKeyDown, sortReviewPaths, type ChangeKind } from "./review/tree"
import type { FormAnswer, FormInfo, PermissionRequest as OCPermission, SessionStatus } from "@atlas/client/promise"
import { SessionPermissionDock } from "@/session/requests/session-permission-dock"
import { SessionQuestionDock } from "@/session/requests/session-question-dock"
import { setServerSDK } from "@/runtime/server/client"
import type { ServerScope } from "@/runtime/server/scope"
import { sessionTitle } from "@/session/title"
import { useLanguage } from "@/runtime/i18n/language"
import "@/session/session-identity-header.css"
import { api } from "../api/client"
import { StatusLine, TodoStrip } from "./status-strip"
import type { Catalog, PermissionRequest, QuestionRequest, QuestionResponse } from "../api/types"
import { goHome } from "../state/app"
import { prefs, setPref } from "../state/prefs"
import { panelOpen, setPanelOpen } from "../state/panel"
import type { WorkspaceStore } from "../state/workspace"
import { sessionChanges } from "../lib/diff"
import { Composer, createComposerModel } from "./composer"
import { projectOf, sessionInfo } from "./adapters"
import { toSessionMessages } from "./messages"
import { homeSelection } from "./home"
import { showToast } from "./toast"

const File = lazy(() => import("@atlas/session-ui/file").then((module) => ({ default: module.File })))

const mac = typeof navigator === "object" && /(Mac|iPod|iPhone|iPad)/.test(navigator.platform)
const mod = mac ? "⌘" : "Ctrl"

export function SessionScreen(props: { store: WorkspaceStore; sessionID: string; catalog: Catalog | undefined; onCatalog: (c: Catalog) => void }) {
  const desktop = createMediaQuery("(min-width: 768px)")
  const busy = () => ["running", "starting"].includes(props.store.runState(props.sessionID))
  const status = createMemo<SessionStatus>(() => (busy() ? { type: "busy" } : { type: "idle" }))
  const session = createMemo(() => props.store.state.sessions.find((s) => s.id === props.sessionID))

  createEffect(() => void props.store.openSession(props.sessionID).catch(() => undefined))

  const data = createMemo(() => ({
    session: props.store.state.sessions.map((s) => {
      const info = sessionInfo(props.store, s)
      return { id: info.id, parentID: info.parentID, title: info.title, time: info.time }
    }),
    session_status: { [props.sessionID]: status() },
    session_diff: {},
  }))

  // Review panel open state and width, as Atlas Agent keeps them per layout.
  const reviewOpen = () => panelOpen()
  const toggleReview = () => setPanelOpen(!panelOpen())

  makeKey("e", true, toggleReview)

  return (
    <DataProvider data={data()} directory={props.store.state.view.path} sessionID={props.sessionID}>
      <MarkdownProvider>
        <FileComponentProvider component={File}>
          <div class="flex-1 min-h-0 flex flex-col gap-2 self-stretch px-[var(--shell-inline-inset,8px)] pb-[var(--shell-bottom-inset,8px)] pt-[var(--shell-top-inset,8px)]">
            <div class="relative flex-1 min-h-0 flex flex-col md:flex-row gap-2">
              <Show when={desktop()}>
                <div class="absolute end-3 top-0 z-30 h-12 flex items-center" data-slot="session-review-toggle">
                  <SessionHeaderActions label={useLanguage().t("command.review.toggle")} keybind={[mod, "Shift", "E"]} opened={reviewOpen()} onToggle={toggleReview} />
                </div>
              </Show>
              <div
                class="@container relative z-10 min-w-0 shrink-0 flex flex-col min-h-0 h-full flex-1 md:flex-none transition-[width] duration-[240ms] ease-[cubic-bezier(0.4,0,0.2,1)] motion-reduce:transition-none"
                data-slot="session-chat-panel"
                style={{ width: desktop() && reviewOpen() ? `calc(100% - ${prefs().panelWidth}px - 8px)` : "100%" }}
              >
                <div class="relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-[10px] bg-v2-background-bg-base shadow-[var(--v2-elevation-raised)]">
                  <div class="relative flex-1 min-h-0 overflow-hidden">
                    <Timeline store={props.store} sessionID={props.sessionID} status={status} busy={busy} title={sessionTitle(session()?.title)} />
                  </div>
                  <ComposerDock store={props.store} sessionID={props.sessionID} catalog={props.catalog} onCatalog={props.onCatalog} />
                </div>
                <Show when={desktop() && reviewOpen()}>
                  <ResizeHandle
                    class="-end-1"
                    direction="horizontal"
                    edge="end"
                    size={prefs().panelWidth}
                    min={320}
                    max={Math.round(innerWidth * 0.7)}
                    onResize={(w) => setPref("panelWidth", Math.round(innerWidth - w))}
                  />
                </Show>
              </div>
              <Show when={desktop() && reviewOpen()}>
                <div data-slot="session-side-panel-presence" data-opened="true" class="relative z-0 min-w-0 h-full flex-1 overflow-visible">
                  <div data-slot="session-side-panel-content" class="absolute inset-y-0 start-0 size-full">
                    <aside
                      id="review-panel"
                      class="relative min-w-0 flex h-full overflow-hidden rounded-[10px] bg-v2-background-bg-base shadow-[var(--v2-elevation-raised)]"
                    >
                      <div data-slot="session-review-content" class="flex min-h-0 min-w-0 flex-1 flex-col">
                        <ReviewRegion store={props.store} sessionID={props.sessionID} />
                      </div>
                    </aside>
                  </div>
                </div>
              </Show>
            </div>
          </div>
        </FileComponentProvider>
      </MarkdownProvider>
    </DataProvider>
  )
}

function makeKey(key: string, shift: boolean, run: () => void) {
  const handler = (e: KeyboardEvent) => {
    if ((mac ? e.metaKey : e.ctrlKey) && e.shiftKey === shift && !e.altKey && e.key.toLowerCase() === key) {
      e.preventDefault()
      run()
    }
  }
  addEventListener("keydown", handler)
  onCleanup(() => removeEventListener("keydown", handler))
}

/** SessionHeaderActions is Atlas Agent's review toggle (session/header/session-header-actions.tsx). */
function SessionHeaderActions(props: { label: string; keybind: string[]; opened: boolean; onToggle: () => void }) {
  return (
    <div class="flex items-center gap-2">
      <Tooltip
        class="shrink-0"
        placement="bottom"
        value={
          <>
            {props.label}
            <Keybind keys={props.keybind} variant="neutral" />
          </>
        }
      >
        <IconButton
          type="button"
          variant="ghost-muted"
          size="large"
          class="shrink-0"
          style={{
            "--v2-overlay-simple-overlay-hover": "var(--v2-background-bg-layer-01)",
            "--v2-overlay-simple-overlay-pressed": "var(--v2-background-bg-layer-02)",
          }}
          state={props.opened ? "pressed" : undefined}
          onClick={props.onToggle}
          aria-label={props.label}
          aria-expanded={props.opened}
          aria-controls="review-panel"
          icon={<Icon name="sidebar-right" />}
        />
      </Tooltip>
    </div>
  )
}

/** SessionTitleHeader is Atlas Agent's sticky session title bar. */
function SessionTitleHeader(props: ParentProps) {
  return (
    <>
      <div data-session-title class="pointer-events-none sticky top-0 z-30 w-full pb-4">
        <div class="pointer-events-auto bg-v2-background-bg-base pe-3 ps-2.5">{props.children}</div>
      </div>
      <div
        aria-hidden="true"
        class="pointer-events-none sticky top-12 z-[5] -mt-4 h-4 w-full bg-[linear-gradient(to_bottom,var(--v2-background-bg-base),transparent)]"
      />
    </>
  )
}

/** SessionProjectMenu is Atlas Agent's project menu in the session title bar. */
function SessionProjectMenu(props: { store: WorkspaceStore }) {
  const language = useLanguage()
  const project = () => projectOf(props.store)
  const name = () => displayName(project())
  return (
    <Menu placement="bottom-start" gutter={4} shift={-10} modal={false}>
      <Tooltip placement="bottom" value={<bdi>{name()}</bdi>} class="flex shrink-0">
        <Menu.Trigger
          as={IconButton}
          variant="ghost-muted"
          aria-label={name()}
          data-slot="session-project-trigger"
          icon={
            <span class="text-v2-icon-icon-muted">
              <Icon name="monitor" />
            </span>
          }
        />
      </Tooltip>
      <Menu.Portal>
        <Menu.Content class="w-max max-w-[min(320px,calc(100vw-16px))]" aria-label={name()}>
          <Menu.Item
            class="min-w-0 w-full"
            onSelect={() => {
              homeSelection
              goHome()
            }}
          >
            <span class="session-project-link-content">
              <ProjectAvatar class="shrink-0" aria-hidden="true" fallback={name()} src={getProjectAvatarSource(project().id)} variant={getProjectAvatarVariant()} />
              <bdi class="min-w-0 truncate text-13-medium">{name()}</bdi>
            </span>
          </Menu.Item>
          <Menu.Item class="session-project-link min-w-0 w-full cursor-default" aria-disabled closeOnSelect={false}>
            <span class="session-project-link-content">
              <Icon name="folder" class="shrink-0 text-v2-icon-icon-muted" />
              <bdi dir="ltr" class="min-w-0 truncate text-v2-text-text-muted">
                {props.store.state.view.path}
              </bdi>
            </span>
          </Menu.Item>
          <Menu.Separator />
          <Menu.Item disabled>
            <Icon name="settings-gear" class="text-v2-icon-icon-muted" />
            {language.t("project.settings.title")}
          </Menu.Item>
        </Menu.Content>
      </Menu.Portal>
    </Menu>
  )
}

function Timeline(props: { store: WorkspaceStore; sessionID: string; status: () => SessionStatus; busy: () => boolean; title?: string }) {
  const language = useLanguage()
  const messages = createMemo(() => toSessionMessages(props.store.state.messages[props.sessionID] ?? [], props.busy()))
  const projection = createReactiveTimelineProjection({
    sessionMessages: messages,
    status: props.status,
    reasoningMode: () => (prefs().showReasoning ? "compact" : "hidden"),
    shellToolDefaultOpen: () => false,
    editToolDefaultOpen: () => false,
  })
  const [toolOpen, setToolOpen] = createStore<Record<string, boolean | undefined>>({})
  const renderer = createSessionTimelineRowRenderer({
    sessionID: () => props.sessionID,
    status: props.status,
    projection,
    presentation: () => undefined,
    reasoningMode: () => (prefs().showReasoning ? "compact" : "hidden"),
    shellToolDefaultOpen: () => false,
    editToolDefaultOpen: () => false,
    disclosure: { value: (key) => toolOpen[key], set: (key, open) => setToolOpen(key, open) },
    centered: () => true,
    padding: () => "px-4 md:px-6",
  })
  const rowKeys = createMemo(() => projection.rows().map(TimelineRow.key))

  function Row(rowProps: { rowKey: string }) {
    const initial = projection.rowByKey().get(rowProps.rowKey)!
    const row = createMemo(() => projection.rowByKey().get(rowProps.rowKey) ?? initial)
    return <renderer.Row row={row} />
  }

  // Pinned to the bottom unless the reader scrolled up, as Atlas Agent's timeline.
  let viewport: HTMLDivElement | undefined
  const [pinned, setPinned] = createSignal(true)
  const toBottom = () => viewport && (viewport.scrollTop = viewport.scrollHeight)
  createEffect(on([rowKeys, messages], () => pinned() && queueMicrotask(toBottom)))
  createEffect(on(() => props.sessionID, () => {
    setPinned(true)
    requestAnimationFrame(toBottom)
  }))

  const working = () => props.busy() && !props.store.state.permissions.some((p) => p.session_id === props.sessionID) && !props.store.state.questions.some((q) => q.session_id === props.sessionID)

  return (
    <div class="relative w-full h-full min-w-0" data-local-session="">
      <div
        class="absolute left-1/2 -translate-x-1/2 z-[60] pointer-events-none transition-all duration-200 ease-out bottom-8"
        classList={{
          "opacity-100 translate-y-0 scale-100": !pinned(),
          "opacity-0 translate-y-2 pointer-events-none scale-[0.8]": pinned(),
        }}
      >
        <button
          type="button"
          aria-label={language.t("session.messages.jumpToLatest")}
          class="pointer-events-auto flex items-center justify-center w-8 h-7 px-2 py-1.5 rounded-lg border-none cursor-pointer text-v2-text-text-base backdrop-blur-[2px]"
          style={{
            background: "color-mix(in srgb, var(--v2-background-bg-base) 92%, transparent)",
            "box-shadow": "var(--v2-elevation-raised), 0px 2px 8px var(--v2-background-bg-base)",
          }}
          onClick={() => {
            setPinned(true)
            viewport?.scrollTo({ top: viewport.scrollHeight, behavior: "smooth" })
          }}
        >
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path d="M12.3333 8.66665L8 13L3.66667 8.66665M8 12.6667V2.83332" stroke="currentColor" stroke-linecap="square" />
          </svg>
        </button>
      </div>
    <ScrollView
      data-slot="session-timeline-scroll"
      class="relative min-w-0 w-full h-full"
      style={{ "--sticky-accordion-top": "48px" }}
      viewportRef={(el) => (viewport = el)}
      onScroll={(e) => {
        const el = e.currentTarget
        setPinned(el.scrollHeight - el.scrollTop - el.clientHeight < 40)
      }}
    >
      <SessionTitleHeader>
        <div class="h-12 w-full flex items-center justify-between gap-2">
          <div class="flex items-center gap-1 min-w-0 flex-1">
            <div class="flex items-center gap-0.5 min-w-0 flex-1 w-full">
              <SessionProjectMenu store={props.store} />
              <Show when={props.title}>
                <h1 dir="auto" class="w-fit truncate rounded-[6px] px-1 py-1 text-[13px] font-[530] leading-4 tracking-[-0.04px] text-v2-text-text-base">
                  {props.title}
                </h1>
              </Show>
            </div>
          </div>
        </div>
      </SessionTitleHeader>
      <div data-component="session-timeline" class="pb-6">
        <For each={rowKeys()}>{(key) => <Row rowKey={key} />}</For>
        <Show when={working()}>
          <div class="min-w-0 w-full max-w-full md:max-w-session md:mx-auto">
            <div class="flex h-9 items-center gap-2 pt-3 text-[13px] font-[530] leading-text-compact px-4 md:px-6">
              <div data-component="session-working" role="status">
                <TextShimmer text={language.t("session.timeline.working")} active />
              </div>
            </div>
          </div>
        </Show>
      </div>
    </ScrollView>
    </div>
  )
}

function toPermission(p: PermissionRequest): OCPermission {
  return {
    id: p.id,
    sessionID: p.session_id,
    action: p.action || p.tool_name,
    // The terminal shows what the tool wants to do ("Create file x") and
    // where; the card needs both.
    resources: [...new Set([p.description, p.path].filter(Boolean))],
    message: p.description,
  } as OCPermission
}

function toForm(q: QuestionRequest): FormInfo {
  const fields = q.questions.map((item) => {
    const base = { key: item.id, title: item.label || undefined, description: item.question }
    if (item.type === "multi_choice")
      return { ...base, type: "multiselect" as const, options: (item.choices ?? []).map((c) => ({ value: c.id, label: c.label, description: c.description })) }
    if (item.type === "yes_no")
      return { ...base, type: "string" as const, options: [{ value: "yes", label: "Yes" }, { value: "no", label: "No" }] }
    if (item.type === "single_choice")
      return { ...base, type: "string" as const, options: (item.choices ?? []).map((c) => ({ value: c.id, label: c.label, description: c.description })) }
    return { ...base, type: "string" as const }
  })
  return { id: q.id, sessionID: q.session_id, title: q.confirm_title ?? "", fields } as unknown as FormInfo
}

function toResponses(q: QuestionRequest, answer: FormAnswer): QuestionResponse[] {
  return q.questions.map((item) => {
    const value = answer[item.id]
    if (item.type === "yes_no") return { request_id: item.id, yes: value === "yes" }
    if (item.type === "multi_choice") {
      const ids = new Set((item.choices ?? []).map((c) => c.id))
      const list = Array.isArray(value) ? value : value === undefined ? [] : [String(value)]
      const custom = list.filter((v) => !ids.has(v))
      return { request_id: item.id, selected_ids: list.filter((v) => ids.has(v)), fill_in_text: custom.join(", ") || undefined }
    }
    if (item.type === "single_choice") {
      const v = value === undefined ? "" : String(value)
      return (item.choices ?? []).some((c) => c.id === v) ? { request_id: item.id, selected_ids: [v] } : { request_id: item.id, fill_in_text: v }
    }
    return { request_id: item.id, fill_in_text: value === undefined ? "" : String(value) }
  })
}

function ComposerDock(props: { store: WorkspaceStore; sessionID: string; catalog: Catalog | undefined; onCatalog: (c: Catalog) => void }) {
  const language = useLanguage()
  const model = createComposerModel({
    key: () => props.sessionID,
    store: () => props.store,
    sessionID: () => props.sessionID,
    catalog: () => props.catalog,
    onCatalog: props.onCatalog,
  })
  const permission = createMemo(() => props.store.state.permissions.find((p) => p.session_id === props.sessionID))
  const question = createMemo(() => props.store.state.questions.find((q) => q.session_id === props.sessionID))
  const [responding, setResponding] = createSignal(false)

  createEffect(() => {
    const store = props.store
    setServerSDK({
      scope: "atlas" as ServerScope,
      api: {
        session: {
          form: {
            reply: async ({ formID, answer }) => {
              const q = store.state.questions.find((x) => x.id === formID)
              if (!q) return
              await api.answerQuestion(store.id, q.id, toResponses(q, answer))
            },
            cancel: async ({ formID }) => {
              await api.cancelQuestion(store.id, formID)
            },
          },
        },
      },
    })
  })

  const decide = async (response: "once" | "always" | "reject") => {
    const p = permission()
    if (!p) return
    setResponding(true)
    try {
      await api.decidePermission(props.store.id, p.id, response === "once" ? "allow" : response === "always" ? "allow_session" : "deny")
    } catch (err) {
      showToast({ variant: "error", title: language.t("common.requestFailed"), description: String((err as Error)?.message ?? err) })
    } finally {
      setResponding(false)
    }
  }

  return (
    <div data-component="session-composer-dock" class="w-full shrink-0 flex flex-col justify-center items-center pb-3 pointer-events-none bg-v2-background-bg-base">
      <div class="w-full px-3 pointer-events-auto md:max-w-session md:mx-auto">
        <Show when={question()} keyed>
          {(q) => (
            <div>
              <SessionQuestionDock request={toForm(q)} onSubmit={() => {}} />
            </div>
          )}
        </Show>
        <Show when={permission()} keyed>
          {(p) => (
            <div>
              <SessionPermissionDock request={toPermission(p)} responding={responding()} onDecide={(r) => void decide(r)} />
            </div>
          )}
        </Show>
        <Show when={!question() && !permission()}>
          <TodoStrip store={props.store} sessionID={props.sessionID} />
          <div class="relative z-[70]">
            <div class="relative">
              <div class="relative z-10">
                <Composer model={model} borderUnderlay />
              </div>
            </div>
          </div>
          <StatusLine store={props.store} sessionID={props.sessionID} catalog={() => props.catalog} />
        </Show>
      </div>
    </div>
  )
}

/** ReviewRegion is Atlas Agent's review extension panel (gui-extensions/review/panel.tsx)
 * fed from the Atlas session's file history. */
function ReviewRegion(props: { store: WorkspaceStore; sessionID: string }) {
  const language = useLanguage()
  const [history] = createResource(
    () => ({ id: props.store.id, sid: props.sessionID, v: props.store.state.versions.history[props.sessionID] ?? 0 }),
    ({ id, sid }) => api.history(id, sid),
  )
  const diffs = createMemo(() =>
    sessionChanges(history.latest ?? [],props.store.state.view.path).map((d) => ({
      file: d.path,
      before: d.before,
      after: d.after,
      additions: d.additions,
      deletions: d.deletions,
      status: (d.created ? "added" : "modified") as "added" | "modified",
    })),
  )
  const [state, setState] = createStore({ filter: "", active: undefined as string | undefined, sidebar: true, expand: "collapse" as SessionReviewExpandMode })
  const searching = createMemo(() => state.filter.trim().length > 0)
  const filtered = createMemo(() => diffs().map((d) => d.file).filter((f) => f.toLowerCase().includes(state.filter.trim().toLowerCase())))
  // The tree's depth-first order is the order the preview steps through.
  const files = createMemo(() => (searching() ? filtered() : sortReviewPaths(filtered())))
  // Changes-only trees omit "M": every row is already a change; added files stay marked.
  const kinds = createMemo(() => {
    const out = new Map<string, ChangeKind>()
    for (const d of diffs()) {
      const file = d.file.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "")
      const kind: ChangeKind = d.status === "added" ? "add" : "mix"
      const parts = file.split("/")
      parts.slice(0, -1).forEach((_, i) => {
        const dir = parts.slice(0, i + 1).join("/")
        const prev = out.get(dir)
        out.set(dir, !prev || prev === kind ? kind : "mix")
      })
      if (kind === "add") out.set(file, kind)
    }
    return out
  })
  const [highlight, setHighlight] = createSignal<string>()
  const highlighted = createMemo(() => {
    if (!searching() || filtered().length === 0) return undefined
    const h = highlight()
    return h && filtered().includes(h) ? h : filtered()[0]
  })
  const active = createMemo(() => (state.active && files().includes(state.active) ? state.active : files()[0]))
  const activeDiff = createMemo(() => diffs().find((d) => d.file === active()))
  // ReviewTitle (review/parts.tsx) for the single mode Atlas has: the session's changes.
  const title = (
    <span class="inline-flex h-6 items-center ps-2 pe-1 text-[13px] leading-[var(--line-height-compact)] font-[530] tracking-[-0.04px] text-v2-text-text-base">
      {language.t("ui.sessionReview.title")}
    </span>
  )
  // The pinned review tab (review/renderer.tsx): its count once files changed.
  const tabTitle = () => (diffs().length > 0 ? language.plural("review.tab.count", diffs().length) : language.t("review.tab.title"))

  return (
    <Tabs value="review" class="flex min-h-0 min-w-0 flex-1 flex-col">
      <div class="session-review-v2-tabs-bar sticky top-0 shrink-0 flex items-center">
        <Tabs.List>
          <Tabs.Trigger value="review" id="session-side-panel-review-tab" aria-controls="session-side-panel-review-tabpanel">
            {tabTitle()}
          </Tabs.Trigger>
        </Tabs.List>
        <div data-slot="session-side-panel-actions" class="session-review-v2-open-in-app-slot h-[var(--tabs-bar-height)] shrink-0 flex items-center gap-2 pe-3">
          <div class="size-7 shrink-0" aria-hidden />
        </div>
      </div>
      <div
        id="session-side-panel-review-tabpanel"
        role="tabpanel"
        aria-labelledby="session-side-panel-review-tab"
        data-slot="tabs-content"
        class="flex h-full min-h-0 flex-1 overflow-hidden"
      >
        <SessionReviewV2
          title={title}
          stats={<DiffChanges changes={diffs()} />}
          empty={
            <Show when={!history.loading} fallback={<div class="px-6 py-4 text-text-weak">{language.t("review.loadingChanges")}</div>}>
              <SessionReviewEmptyChangesV2 />
            </Show>
          }
          sidebarOpen={state.sidebar}
          sidebarToggle={<SessionReviewV2SidebarToggle opened={state.sidebar} onToggle={() => setState("sidebar", !state.sidebar)} />}
          sidebar={
            <SessionReviewV2Sidebar open={state.sidebar} transition title={title} stats={<DiffChanges changes={diffs()} />} filter={state.filter} onFilterChange={(v) => setState("filter", v)}
              onFilterKeyDown={(e: KeyboardEvent) =>
                searching() && applyFileListKeyDown(e, filtered(), highlighted(), { onHighlight: setHighlight, onSelect: (f) => setState("active", f) })
              }
            >
              <Show
                when={searching()}
                fallback={<ReviewTree allowed={filtered()} kinds={kinds()} active={active()} onFileClick={(f) => setState("active", f)} />}
              >
                <Show when={filtered().length > 0} fallback={<div class="px-2 py-2 text-12-regular text-text-weak">{language.t("palette.empty")}</div>}>
                  <ReviewList
                    files={filtered()}
                    kinds={kinds()}
                    active={active()}
                    highlighted={highlighted()}
                    onFileClick={(f) => {
                      setHighlight(f)
                      setState("active", f)
                    }}
                  />
                </Show>
              </Show>
            </SessionReviewV2Sidebar>
          }
          activeFile={active()}
          files={files()}
          onSelectFile={(f) => setState("active", f)}
          diffStyle={prefs().diffStyle}
          onDiffStyleChange={(s) => setPref("diffStyle", s)}
          expandMode={state.expand}
          onExpandModeChange={(m) => setState("expand", m)}
          hasDiffs={diffs().length > 0}
          preview={
            <Show when={active()} keyed>
              {(file) => (
                <Show when={activeDiff()}>
                  {(diff) => <SessionReviewFilePreviewV2 file={file} diff={diff()} diffStyle={prefs().diffStyle} expandMode={state.expand} />}
                </Show>
              )}
            </Show>
          }
        />
      </div>
    </Tabs>
  )
}
