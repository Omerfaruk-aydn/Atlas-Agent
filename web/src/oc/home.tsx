// The home screen: Atlas Agent's Home route (home/route.tsx) with its views
// copied unchanged under ./app/home, fed from Atlas projects and sessions.

import { ScrollView } from "@atlas/ui/scroll-view"
import { useDialog } from "@atlas/ui/context/dialog"
import { Button } from "@atlas/ui/button"
import { Dialog, DialogFooter, DialogHeader, DialogTitleGroup } from "@atlas/ui/dialog"
import { createMediaQuery } from "@solid-primitives/media"
import { makeEventListener } from "@solid-primitives/event-listener"
import { createMemo, createResource, createSignal, onCleanup, Show } from "solid-js"
import { createStore } from "solid-js/store"
import { api } from "../api/client"
import { app, closeProject, closeTab, newDraft, openProject, openSession, openSettings, setHelpDialog, setProjectDialog, workspaceByPath, workspaces } from "../state/app"
import { prefs, setPref } from "../state/prefs"
import { HomeProjectsView, HomeUtilityNav } from "@/home/projects/view"
import { HomeSessionsView } from "@/home/sessions/view"
import { homeSessionSearchKey, type HomeSessionGroup, type HomeSessionRecord, type OpenSessionOptions } from "@/home/sessions/controller"
import { createHomeScrollController } from "@/home/scroll"
import { atlasServer, ServerConnection } from "@/runtime/server/registry"
import type { HomeProjectSelection, LocalProject } from "@/shell/state/layout"
import { sessionLabel, sessionTitle } from "@/session/title"
import { displayName } from "@atlas/ui/project-avatar"
import type { SessionInfo } from "@atlas/client/promise"
import { useLanguage } from "@/runtime/i18n/language"
import { findSession, projectOf, sessionInfo } from "./adapters"
import { showToast } from "./toast"

const serverKey = ServerConnection.key(atlasServer)

// Selection survives leaving and re-entering home, like Atlas Agent's layout.
const [selection, setSelection] = createSignal<HomeProjectSelection>({ server: serverKey })
export { selection as homeSelection }

const [searchFocusRequest, setSearchFocusRequest] = createSignal(false)
/** requestHomeSearchFocus asks the next home mount to focus its search. */
export const requestHomeSearchFocus = () => setSearchFocusRequest(true)

function localDay(date: Date) {
  return date.getFullYear() * 10_000 + date.getMonth() * 100 + date.getDate()
}

function groupSessions(records: HomeSessionRecord[], language: ReturnType<typeof useLanguage>): HomeSessionGroup[] {
  const now = new Date()
  const today = localDay(now)
  const yesterday = localDay(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1))
  const day = (record: HomeSessionRecord) => localDay(new Date(record.session.time.updated ?? record.session.time.created))
  const todaySessions = records.filter((record) => day(record) === today)
  const yesterdaySessions = records.filter((record) => day(record) === yesterday)
  const olderSessions = records.filter((record) => day(record) !== today && day(record) !== yesterday)
  const olderTitle =
    todaySessions.length === 0 && yesterdaySessions.length === 0
      ? language.t("sidebar.project.recentSessions")
      : language.t("home.sessions.group.older")
  return [
    { id: "today" as const, title: language.t("home.sessions.group.today"), sessions: todaySessions },
    { id: "yesterday" as const, title: language.t("home.sessions.group.yesterday"), sessions: yesterdaySessions },
    { id: "older" as const, title: olderTitle, sessions: olderSessions },
  ].filter((group) => group.sessions.length > 0)
}

function basename(path: string) {
  return path.replaceAll("\\", "/").replace(/\/+$/, "").split("/").pop() || path
}

function createProjects(language: ReturnType<typeof useLanguage>) {
  const [known] = createResource(() => api.projects().catch(() => []))
  const projects = createMemo<LocalProject[]>(() => workspaces().map(projectOf))
  const recentlyClosed = createMemo<LocalProject[]>(() =>
    (known() ?? [])
      .filter((p) => !workspaceByPath(p.path))
      .slice(0, 8)
      .map((p) => ({ id: p.path, worktree: p.path, name: p.name })),
  )
  const selected = createMemo(() => projects().find((p) => p.worktree === selection().directory))
  const fail = (err: unknown) => showToast({ title: language.t("common.requestFailed"), description: err instanceof Error ? err.message : String(err) })

  return {
    projects,
    recentlyClosed,
    selected,
    newSessionDirectory: () => selected()?.worktree ?? workspaces().find((w) => w.id === app.current)?.state.view.path ?? projects()[0]?.worktree,
    select: (directory: string) =>
      setSelection((current) => ({ server: current.server, directory: current.directory === directory ? undefined : directory })),
    add: (directories: string[]) => {
      for (const dir of directories) void openProject(dir).catch(fail)
    },
    close: (directory: string) => {
      const store = workspaceByPath(directory)
      if (!store) return
      if (selection().directory === directory) setSelection({ server: serverKey })
      void closeProject(store.id).catch(fail)
    },
    hasUnread: (project: LocalProject) => {
      const store = workspaceByPath(project.worktree)
      return !!store?.state.sessions.some((s) => ["question", "permission"].includes(store.runState(s.id)))
    },
  }
}

function createSessions(language: ReturnType<typeof useLanguage>, projects: ReturnType<typeof createProjects>) {
  const dialog = useDialog()

  const allRecords = createMemo<HomeSessionRecord[]>(() => {
    const out: HomeSessionRecord[] = []
    for (const store of workspaces()) {
      const project = projectOf(store)
      for (const s of store.state.sessions) {
        if (s.parent_session_id) continue
        out.push({ session: sessionInfo(store, s), project, projectName: displayName(project) })
      }
    }
    return out.sort((a, b) => (b.session.time.updated ?? 0) - (a.session.time.updated ?? 0))
  })
  const records = createMemo(() => {
    const dir = selection().directory
    return dir ? allRecords().filter((r) => r.project.worktree === dir) : allRecords()
  })
  const groups = createMemo(() => groupSessions(records(), language))
  const loading = () => workspaces().some((w) => !w.state.sessionsLoaded)

  const open = (session: SessionInfo, options?: OpenSessionOptions) => {
    const hit = findSession(session.id)
    if (!hit) return
    if (options?.background) {
      if (!prefs().tabs.some((t) => !t.draft && t.sid === session.id)) setPref("tabs", [...prefs().tabs, { path: hit.store.state.view.path, sid: session.id }])
      return
    }
    openSession(hit.store.id, session.id)
  }

  const rename = async (_server: ServerConnection.Key, session: SessionInfo, title: string) => {
    const next = title.trim()
    if (!next || next === sessionLabel(session)) return true
    const hit = findSession(session.id)
    if (!hit) return false
    try {
      await api.renameSession(hit.store.id, session.id, next)
      return true
    } catch (err) {
      showToast({ title: language.t("common.requestFailed"), description: err instanceof Error ? err.message : undefined })
      return false
    }
  }

  const remove = async (session: SessionInfo) => {
    const hit = findSession(session.id)
    if (!hit) return
    try {
      await api.deleteSession(hit.store.id, session.id)
      closeTab(session.id)
    } catch (err) {
      showToast({ title: language.t("common.requestFailed"), description: err instanceof Error ? err.message : undefined })
    }
  }

  function DeleteDialog(props: { session: SessionInfo }) {
    const name = () => sessionTitle(props.session.title) ?? language.t("command.session.new")
    const confirm = async () => {
      await remove(props.session)
      dialog.close()
    }
    return (
      <Dialog fit>
        <DialogHeader hideClose>
          <DialogTitleGroup title={language.t("session.delete.title")} description={language.t("session.delete.confirm", { name: name() })} />
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={() => dialog.close()}>
            {language.t("common.cancel")}
          </Button>
          <Button variant="danger" onClick={confirm}>
            {language.t("session.delete.button")}
          </Button>
        </DialogFooter>
      </Dialog>
    )
  }

  const exportSession = async (_server: ServerConnection.Key, session: SessionInfo) => {
    const hit = findSession(session.id)
    if (!hit) return
    try {
      const messages = await api.messages(hit.store.id, session.id)
      const blob = new Blob([JSON.stringify({ session: hit.session, messages }, null, 2)], { type: "application/json" })
      const a = document.createElement("a")
      a.href = URL.createObjectURL(blob)
      a.download = `${(sessionTitle(session.title) || session.id).replace(/[^\p{L}\p{N}._-]+/gu, "-")}.json`
      a.click()
      setTimeout(() => URL.revokeObjectURL(a.href), 1000)
    } catch (err) {
      showToast({ title: language.t("common.requestFailed"), description: err instanceof Error ? err.message : undefined })
    }
  }

  return {
    allRecords,
    groups,
    loading,
    open,
    rename,
    exportSession,
    showDelete: (_server: ServerConnection.Key, session: SessionInfo) => dialog.show(() => <DeleteDialog session={session} />),
    isOpen: (record: HomeSessionRecord) => prefs().tabs.some((t) => !t.draft && t.sid === record.session.id),
    create: () => newDraft(projects.newSessionDirectory()),
  }
}

function createSearch(language: ReturnType<typeof useLanguage>, projects: ReturnType<typeof createProjects>, sessions: ReturnType<typeof createSessions>) {
  const [state, setState] = createStore({ value: "", focused: false, highlighted: "" })
  let root: HTMLDivElement | undefined
  let input: HTMLInputElement | undefined
  let list: HTMLDivElement | undefined
  const query = createMemo(() => state.value.trim())
  const results = createMemo(() => {
    const value = query().toLowerCase()
    if (!value) return []
    return sessions
      .allRecords()
      .filter((record) => `${sessionLabel(record.session)} ${record.projectName}`.toLowerCase().includes(value) || record.session.id === query())
  })
  const active = createMemo(() => {
    const records = results()
    if (records.some((record) => homeSessionSearchKey(record) === state.highlighted)) return state.highlighted
    return records[0] ? homeSessionSearchKey(records[0]) : ""
  })
  const open = createMemo(() => state.focused && query().length > 0)
  const placeholder = createMemo(() => {
    const project = projects.selected()
    if (project) return language.t("home.sessions.search.placeholder.scoped", { scope: displayName(project) })
    return language.t("home.sessions.search.placeholder")
  })
  onCleanup(
    makeEventListener(document, "pointerdown", (event) => {
      if (!open()) return
      const target = event.target
      if (!(target instanceof Node) || root?.contains(target)) return
      close()
    }),
  )
  onCleanup(
    makeEventListener(window, "keydown", (event) => {
      if ((event.ctrlKey || event.metaKey) && !event.shiftKey && !event.altKey && event.key.toLowerCase() === "f") {
        event.preventDefault()
        focus()
      }
    }),
  )
  function focus() {
    input?.focus()
    setState("focused", true)
  }
  function close() {
    setState({ value: "", focused: false })
  }
  function select(record: HomeSessionRecord, options?: { background?: boolean }) {
    sessions.open(record.session, options)
    if (!options?.background) close()
  }
  return {
    value: () => state.value,
    placeholder,
    open,
    focus,
    input: (value: string) => setState({ value, highlighted: "" }),
    close,
    results,
    active,
    highlight: (record: HomeSessionRecord) => setState("highlighted", homeSessionSearchKey(record)),
    move: (delta: number) => {
      const records = results()
      if (records.length === 0) return
      const index = records.findIndex((record) => homeSessionSearchKey(record) === active())
      const next = ((index === -1 ? 0 : index) + delta + records.length) % records.length
      setState("highlighted", homeSessionSearchKey(records[next]))
      list?.querySelector<HTMLElement>(`[data-key="${state.highlighted}"]`)?.scrollIntoView({ block: "nearest" })
    },
    select,
    selectActive: () => {
      const record = results().find((item) => homeSessionSearchKey(item) === active())
      if (record) select(record)
    },
    setRoot: (element: HTMLDivElement) => (root = element),
    setInput: (element: HTMLInputElement) => {
      input = element
      if (searchFocusRequest()) {
        setSearchFocusRequest(false)
        queueMicrotask(focus)
      }
    },
    setList: (element: HTMLDivElement) => (list = element),
  }
}

export function Home() {
  const language = useLanguage()
  const mobile = createMediaQuery("(max-width: 767px)")
  const projects = createProjects(language)
  const sessions = createSessions(language, projects)
  const search = createSearch(language, projects, sessions)
  const scroll = createHomeScrollController(sessions.groups)

  const projectsView = (dropdown?: boolean) => (
    <HomeProjectsView
      dropdown={dropdown}
      language={language}
      servers={[atlasServer]}
      projects={projects.projects()}
      recentlyClosed={projects.recentlyClosed()}
      selection={selection()}
      homedir=""
      serverHealth={() => ({ healthy: app.boot?.server.reachable !== false })}
      projectsForServer={() => projects.projects()}
      collapsed={() => false}
      canRevealProject={() => false}
      hasUnread={(_server, project) => projects.hasUnread(project)}
      onWheel={scroll.viewport.containWheel}
      onChooseProject={() => setProjectDialog(true)}
      onFocusServer={() => setSelection({ server: serverKey })}
      onToggleCollapsed={() => {}}
      onEditServer={() => {}}
      canRemoveServer={() => false}
      onRemoveServer={() => {}}
      canHideServer={() => false}
      onHideServer={() => {}}
      onMoveProject={() => {}}
      onSelectProject={(_server, directory) => projects.select(directory)}
      onAddProjects={(_server, directories) => projects.add(directories)}
      onOpenProjectNewSession={(_server, directory) => newDraft(directory)}
      canImportSession={false}
      onImportSession={() => {}}
      onEditProject={() => {}}
      onRevealProject={() => {}}
      onClearNotifications={() => {}}
      onCloseProject={(_server, directory) => projects.close(directory)}
      onOpenSettings={() => openSettings()}
      onOpenHelp={() => setHelpDialog(true)}
    />
  )

  return (
    <div
      data-slot="home-panel"
      class={`
        mx-[var(--shell-inline-inset,8px)] mb-[var(--shell-bottom-inset,8px)] mt-[var(--shell-top-inset,8px)] flex min-h-0 flex-1 flex-col self-stretch overflow-hidden rounded-[10px]
        bg-v2-background-bg-base shadow-[var(--v2-elevation-raised)]
      `}
    >
      <Show when={mobile()}>
        <div class="relative z-40 -mb-3 shrink-0 px-3 pt-3">{projectsView(true)}</div>
      </Show>
      <ScrollView
        class="min-h-0 flex-1 [container-type:size]"
        thumbContainer={scroll.viewport.thumbTrack()}
        thumbHoverTarget={scroll.viewport.hoverTarget()}
        viewportRef={scroll.viewport.setViewport}
        onScroll={(event) => scroll.viewport.update(event.currentTarget.scrollTop)}
        onWheel={scroll.viewport.containOuterWheel}
      >
        <div
          class={`
            mx-auto grid min-h-full w-full max-w-[1080px] grid-rows-[auto_minmax(0,1fr)] gap-4 px-3
            max-md:grid-rows-[minmax(0,1fr)] lg:grid-cols-[280px_minmax(0,720px)] lg:grid-rows-1 lg:gap-8 lg:px-6
          `}
        >
          <Show when={!mobile()}>{projectsView()}</Show>
          <HomeSessionsView
            language={language}
            groups={sessions.groups()}
            loading={sessions.loading()}
            desktop={false}
            location={(record) => ({ worktree: basename(record.session.location.directory), branch: undefined })}
            onRevealLocations={() => {}}
            showProjectName={!projects.selected()}
            server={serverKey}
            canCreateSession={!!projects.newSessionDirectory()}
            searchValue={search.value()}
            searchPlaceholder={search.placeholder()}
            searchOpen={search.open()}
            searchLoading={false}
            searchResults={search.results()}
            searchActive={search.active()}
            searchNoResultsLabel={language.t("home.sessions.search.noResults", { query: search.value().trim() })}
            titleOpacity={scroll.header.titleOpacity}
            isOpenTab={sessions.isOpen}
            onCreateSession={sessions.create}
            onOpenSession={sessions.open}
            onArchiveSession={async () => {}}
            onRenameSession={sessions.rename}
            onExportSession={sessions.exportSession}
            onDeleteSession={sessions.showDelete}
            onSetHoverTarget={scroll.viewport.setHoverTarget}
            onSetThumbTrack={scroll.viewport.setThumbTrack}
            onSetContent={scroll.header.setContent}
            onSetHeader={scroll.header.setHeader}
            onWheel={scroll.viewport.containWheel}
            onSetSearchRoot={search.setRoot}
            onSetSearchInput={search.setInput}
            onSetSearchList={search.setList}
            onSearchFocus={search.focus}
            onSearchInput={search.input}
            onSearchClose={search.close}
            onSearchMove={search.move}
            onSearchSelectActive={search.selectActive}
            onSearchHighlight={search.highlight}
            onSearchSelect={search.select}
          />
        </div>
      </ScrollView>
      <div class="hidden shrink-0 px-3 py-2 md:block lg:hidden">
        <HomeUtilityNav class="flex" onOpenSettings={() => openSettings()} onOpenHelp={() => setHelpDialog(true)} language={language} />
      </div>
    </div>
  )
}
