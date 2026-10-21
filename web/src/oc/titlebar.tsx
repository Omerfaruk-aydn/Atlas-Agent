// The titlebar: Atlas Agent's shell/titlebar/titlebar.tsx and tab-strip.tsx
// (web, horizontal tabs), with the tab items themselves copied unchanged
// under ./app/shell/titlebar. Tabs are Atlas session and draft tabs.

import { createMemo, createSignal, For, Show } from "solid-js"
import { createStore } from "solid-js/store"
import { DragDropProvider, PointerSensor } from "@dnd-kit/solid"
import { isSortable, useSortable } from "@dnd-kit/solid/sortable"
import { Accessibility, AutoScroller, Feedback, PointerActivationConstraints } from "@dnd-kit/dom"
import { RestrictToHorizontalAxis, RestrictToVerticalAxis } from "@dnd-kit/abstract/modifiers"
import { RestrictToElement } from "@dnd-kit/dom/modifiers"
import { arrayMove } from "@dnd-kit/helpers"
import { createMediaQuery } from "@solid-primitives/media"
import { makeEventListener } from "@solid-primitives/event-listener"
import { IconButton } from "@atlas/ui/icon-button"
import { Icon } from "@atlas/ui/icon"
import { Keybind } from "@atlas/ui/keybind"
import { Tooltip } from "@atlas/ui/tooltip"
import { Menu } from "@atlas/ui/menu"
import { DraftTabItem, TabNavItem } from "@/shell/titlebar/tab-nav"
import { SessionTabAvatarView } from "@/shell/layout/session-tab-avatar"
import { sessionTabTitle } from "@/shell/titlebar/tab-title"
import { MobileDrawer, MobileDrawerContent, MobileDrawerLabel, MobileDrawerTrigger } from "@/shell/mobile-drawer"
import { useSessionTabAvatarState } from "@/shell/layout/project-avatar-state"
import { atlasServer, ServerConnection } from "@/runtime/server/registry"
import { useLanguage } from "@/runtime/i18n/language"
import "@/shell/titlebar/titlebar.css"
import { api } from "../api/client"
import { app, closeTab, goHome, newDraft, openSettings, selectTab, setHelpDialog, tabKey, workspaceByPath } from "../state/app"
import { prefs, setPref, type SessionTab } from "../state/prefs"
import { projectOf, sessionInfo } from "./adapters"
import { requestHomeSearchFocus } from "./home"
import { showToast } from "./toast"

const serverKey = ServerConnection.key(atlasServer)
const mac = typeof navigator === "object" && /(Mac|iPod|iPhone|iPad)/.test(navigator.platform)
const mod = mac ? "⌘" : "Ctrl"

// Recently closed session tabs, newest first, for the new-tab context menu.
const [closed, setClosed] = createSignal<SessionTab[]>([])

function closeTabRemembered(tab: SessionTab) {
  if (!tab.draft) setClosed((list) => [tab, ...list.filter((t) => t.sid !== tab.sid)].slice(0, 20))
  closeTab(tabKey(tab))
}

function reopenClosed(tab?: SessionTab) {
  const next = tab ?? closed()[0]
  if (!next) return
  setClosed((list) => list.filter((t) => t.sid !== next.sid))
  if (!prefs().tabs.some((t) => !t.draft && t.sid === next.sid)) setPref("tabs", [...prefs().tabs, next])
  selectTab(next)
}

function isActive(tab: SessionTab) {
  return tab.draft ? app.screen === "draft" && app.draft === tab.draft : app.screen === "session" && app.session === tab.sid
}

function tabHref(tab: SessionTab) {
  if (tab.draft) return `?draft=${encodeURIComponent(tab.draft)}`
  const store = workspaceByPath(tab.path)
  return store ? `?w=${encodeURIComponent(store.id)}&s=${encodeURIComponent(tab.sid)}` : "#"
}

function SessionTabSlot(props: { tab: SessionTab; index: number; vertical: boolean; onNavigate: (el: HTMLDivElement) => void }) {
  const language = useLanguage()
  const id = () => tabKey(props.tab)
  const sortable = useSortable({
    get id() {
      return id()
    },
    get index() {
      return props.index
    },
  })
  let ref!: HTMLDivElement
  const store = () => workspaceByPath(props.tab.path)
  const session = createMemo(() => {
    const s = store()
    const raw = s?.state.sessions.find((x) => x.id === props.tab.sid)
    return s && raw ? sessionInfo(s, raw) : undefined
  })
  const status = useSessionTabAvatarState(
    () => serverKey,
    () => props.tab.sid,
  )
  const rename = async (title: string) => {
    const s = store()
    if (!s) return
    try {
      await api.renameSession(s.id, props.tab.sid, title)
    } catch (err) {
      showToast({ title: language.t("common.requestFailed"), description: err instanceof Error ? err.message : undefined })
    }
  }

  return (
    <div
      ref={sortable.ref}
      data-titlebar-tab-slot
      data-tab-key={id()}
      data-active={isActive(props.tab)}
      data-orientation={props.vertical ? "vertical" : "horizontal"}
      class="relative flex"
      classList={{
        "w-56 min-w-7 max-w-56 flex-shrink": !props.vertical,
        "w-full shrink-0": props.vertical,
      }}
    >
      <Show
        when={!props.tab.draft}
        fallback={
          <DraftTabItem
            ref={(el) => (ref = el)}
            href={tabHref(props.tab)}
            title={language.t("session.tab.session")}
            onNavigate={() => props.onNavigate(ref)}
            onClose={() => closeTabRemembered(props.tab)}
            active={isActive(props.tab)}
            dragging={sortable.isDragSource()}
            orientation={props.vertical ? "vertical" : "horizontal"}
          />
        }
      >
        <TabNavItem
          ref={(el) => (ref = el)}
          href={tabHref(props.tab)}
          server={serverKey}
          session={session()}
          preparing={false}
          unread={status.unread()}
          loading={status.loading()}
          fallbackTitle={session() ? undefined : language.t("session.tab.unknown")}
          onRename={rename}
          onNavigate={() => props.onNavigate(ref)}
          onClose={() => closeTabRemembered(props.tab)}
          active={isActive(props.tab)}
          dragging={sortable.isDragSource()}
          orientation={props.vertical ? "vertical" : "horizontal"}
        />
      </Show>
    </div>
  )
}

function TitlebarTabStrip(props: { vertical?: boolean; onNavigate?: () => void }) {
  let listRef!: HTMLDivElement
  const tabs = () => prefs().tabs
  const vertical = () => !!props.vertical

  return (
    <div
      data-slot={vertical() ? "vertical-tabs" : "titlebar-tabs"}
      data-orientation={vertical() ? "vertical" : "horizontal"}
      class="relative min-w-0"
      classList={{ "min-h-0 overflow-hidden": vertical() }}
    >
      <div
        data-slot={vertical() ? "vertical-tabs-scroll" : "titlebar-tabs-scroll"}
        class="flex min-w-0 no-scrollbar [app-region:no-drag]"
        classList={{
          "flex-row items-center gap-1.5 overflow-x-auto": !vertical(),
          "max-h-full flex-col overflow-y-auto overflow-x-hidden": vertical(),
        }}
      >
        <DragDropProvider
          sensors={[
            PointerSensor.configure({
              activationConstraints: (event) =>
                event.pointerType === "touch"
                  ? [new PointerActivationConstraints.Distance({ value: 8 })]
                  : [new PointerActivationConstraints.Distance({ value: 4 })],
              preventActivation: (event) =>
                event.target instanceof Element && !!event.target.closest('[data-slot="tab-close"], [contenteditable="true"]'),
            }),
          ]}
          modifiers={[vertical() ? RestrictToVerticalAxis : RestrictToHorizontalAxis, RestrictToElement.configure({ element: () => listRef })]}
          plugins={(defaults) => [
            ...defaults.filter((plugin) => plugin !== Accessibility),
            AutoScroller.configure({ acceleration: 8, threshold: vertical() ? { x: 0, y: 0.05 } : { x: 0.05, y: 0 } }),
            Feedback.configure({ dropAnimation: null }),
          ]}
          onDragStart={(event) => {
            const source = event.operation.source
            if (!source || vertical()) return
            const tab = tabs().find((item) => tabKey(item) === source.id.toString())
            if (tab) selectTab(tab)
          }}
          onDragEnd={(event) => {
            const source = event.operation.source
            if (event.canceled || !isSortable(source)) return
            if (source.initialIndex === source.index) return
            setPref("tabs", arrayMove([...tabs()], source.initialIndex, source.index))
          }}
        >
          <div
            data-titlebar-tab-list
            data-orientation={vertical() ? "vertical" : "horizontal"}
            class="flex w-full min-w-0"
            classList={{ "flex-row items-center": !vertical(), "flex-col items-stretch": vertical() }}
            ref={listRef}
          >
            <For each={tabs()}>
              {(tab, index) => (
                <SessionTabSlot
                  tab={tab}
                  index={index()}
                  vertical={vertical()}
                  onNavigate={(el) => {
                    selectTab(tab)
                    el?.scrollIntoView({ behavior: "instant", block: "nearest" })
                    props.onNavigate?.()
                  }}
                />
              )}
            </For>
          </div>
        </DragDropProvider>
      </div>
      <Show when={!vertical()}>
        <div
          data-slot="titlebar-tabs-fade-left"
          aria-hidden="true"
          class="pointer-events-none absolute inset-y-0 left-0 z-10 w-6 bg-[linear-gradient(to_right,var(--v2-background-bg-deep),transparent)]"
        />
        <div
          data-slot="titlebar-tabs-fade-right"
          aria-hidden="true"
          class="pointer-events-none absolute inset-y-0 right-0 z-10 w-6 bg-[linear-gradient(to_left,var(--v2-background-bg-deep),transparent)]"
        />
      </Show>
    </div>
  )
}

function RecentlyClosedTabsMenu(props: { onNewTab: () => void; tooltip?: any }) {
  const language = useLanguage()
  return (
    <Menu.Context modal={false}>
      <Tooltip placement="bottom" value={props.tooltip}>
        <Menu.Context.Trigger
          as={IconButton}
          type="button"
          variant="ghost-muted"
          size="large"
          class="shrink-0"
          icon={<Icon name="plus" />}
          onClick={props.onNewTab}
          aria-label={language.t("command.session.new")}
        />
      </Tooltip>
      <Menu.Context.Portal>
        <Menu.Context.Content class="max-h-[66.667dvh] w-56 max-w-56 overflow-y-auto">
          <Menu.Group>
            <Menu.GroupLabel>{language.t("home.recentlyClosed")}</Menu.GroupLabel>
            <For each={closed()}>
              {(tab) => {
                const store = () => workspaceByPath(tab.path)
                const title = () => sessionTabTitle(store()?.state.sessions.find((s) => s.id === tab.sid)?.title, language.t("session.tab.session"))
                return (
                  <Menu.Item class="max-w-56" onSelect={() => reopenClosed(tab)}>
                    <span class="flex size-4 shrink-0 items-center justify-center">
                      <SessionTabAvatarView project={store() ? projectOf(store()!) : undefined} directory={tab.path} unread={false} loading={false} />
                    </span>
                    <span dir="auto" class="block min-w-0 flex-1 truncate text-start">
                      {title()}
                    </span>
                  </Menu.Item>
                )
              }}
            </For>
          </Menu.Group>
        </Menu.Context.Content>
      </Menu.Context.Portal>
    </Menu.Context>
  )
}

/** toggleHome goes home, or back to the last tab when already home. */
function toggleHome() {
  if (app.screen !== "home") return goHome()
  const tabs = prefs().tabs
  const last = tabs[tabs.length - 1]
  if (last) selectTab(last)
}

export function Titlebar() {
  const language = useLanguage()
  const mobile = createMediaQuery("(max-width: 767px)")
  const [mobileTabs, setMobileTabs] = createStore({ open: false, settings: false })

  const openNewTab = () => newDraft(workspaceByPath(prefs().tabs.find(isActive)?.path ?? "")?.state.view.path)

  makeEventListener(window, "keydown", (event) => {
    const modKey = mac ? event.metaKey : event.ctrlKey
    if (!modKey || event.altKey) return
    const key = event.key.toLowerCase()
    if (key === "b" && !event.shiftKey) {
      event.preventDefault()
      if (app.screen !== "home") requestHomeSearchFocus()
      toggleHome()
    } else if (key === "t" && event.shiftKey) {
      event.preventDefault()
      reopenClosed()
    } else if ((key === "t" || key === "n") && !event.shiftKey) {
      event.preventDefault()
      openNewTab()
    } else if (key === "w" && !event.shiftKey) {
      const current = prefs().tabs.find(isActive)
      if (!current) return
      event.preventDefault()
      closeTabRemembered(current)
    } else if (/^[1-9]$/.test(key)) {
      const tab = prefs().tabs[Number(key) - 1]
      if (!tab) return
      event.preventDefault()
      selectTab(tab)
    }
  })

  const current = () => prefs().tabs.find(isActive)
  const currentTitle = () => {
    const tab = current()
    if (!tab) return language.t("home.title")
    if (tab.draft) return language.t("session.tab.session")
    const store = workspaceByPath(tab.path)
    return sessionTabTitle(store?.state.sessions.find((s) => s.id === tab.sid)?.title, language.t("session.tab.session"))
  }

  const homeButton = () => (
    <Tooltip
      placement="bottom"
      value={
        <>
          {language.t("home.title")}
          <Keybind keys={[mod, "B"]} variant="neutral" />
        </>
      }
      class="shrink-0"
    >
      <IconButton
        type="button"
        variant="ghost-muted"
        size="large"
        class="!w-9 shrink-0"
        icon={<Icon name="grid-plus" />}
        state={app.screen === "home" ? "pressed" : undefined}
        onClick={toggleHome}
        aria-label={language.t("home.title")}
        aria-pressed={app.screen === "home"}
      />
    </Tooltip>
  )

  return (
    <header
      data-slot="titlebar-v2"
      class="shrink-0 relative flex flex-row h-9 bg-v2-background-bg-deep overflow-visible"
      style={{
        height: mobile()
          ? "calc(28px + max(8px, min(16px, env(safe-area-inset-top, 0px) * 1000)) + env(safe-area-inset-top, 0px))"
          : "calc(28px + max(8px, env(safe-area-inset-top, 0px)))",
        "padding-top": mobile() ? "calc(max(8px, min(16px, env(safe-area-inset-top, 0px) * 1000)) + env(safe-area-inset-top, 0px))" : "env(safe-area-inset-top, 0px)",
      }}
    >
      <div
        class="h-full flex-1 overflow-hidden flex flex-row items-center gap-1.5 px-2 md:pe-3"
        classList={{ "pt-[max(0px,calc(8px-env(safe-area-inset-top,0px)))]": !mobile() }}
      >
        <Show when={!mobile()}>{homeButton()}</Show>
        <Show
          when={!mobile()}
          fallback={
            <MobileDrawer
              open={mobileTabs.open}
              onOpenChange={(open) => setMobileTabs("open", open)}
              onContentPresentChange={(present) => {
                if (present || !mobileTabs.settings) return
                setMobileTabs("settings", false)
                openSettings()
              }}
            >
              <MobileDrawerTrigger
                data-slot="mobile-tabs-trigger"
                class="flex h-7 min-w-0 flex-1 items-center gap-2 rounded-[6px] px-2 text-[13px] leading-4 text-v2-text-text-base focus-visible:outline-none [app-region:no-drag]"
                aria-label={language.t("titlebar.tabs")}
              >
                <Show when={current()} fallback={<Icon name="grid-plus" class="shrink-0" />}>
                  {(tab) => (
                    <span data-slot="project-avatar-slot" class="flex size-4 shrink-0 items-center justify-center">
                      <Show when={!tab().draft} fallback={<Icon name="edit" />}>
                        <SessionTabAvatarView
                          project={workspaceByPath(tab().path) ? projectOf(workspaceByPath(tab().path)!) : undefined}
                          directory={tab().path}
                          unread={false}
                          loading={false}
                        />
                      </Show>
                    </span>
                  )}
                </Show>
                <span data-slot="mobile-tab-title" dir="auto" class="min-w-0 flex-1 truncate text-start">
                  {currentTitle()}
                </span>
                <span class="shrink-0 text-v2-text-text-muted">{prefs().tabs.length}</span>
              </MobileDrawerTrigger>
              <MobileDrawerContent>
                <MobileDrawerLabel class="sr-only">{language.t("titlebar.tabs")}</MobileDrawerLabel>
                <div data-slot="mobile-tabs-drawer" data-corvu-no-drag>
                  <div data-slot="mobile-tabs-drawer-list">
                    <TitlebarTabStrip vertical onNavigate={() => setMobileTabs("open", false)} />
                  </div>
                  <button
                    type="button"
                    data-action="mobile-tabs-new-session"
                    class="flex h-7 w-full shrink-0 items-center gap-2 rounded-[6px] px-2 text-[13px] leading-4 text-v2-text-text-base hover:bg-v2-background-bg-layer-02 focus-visible:outline-none focus-visible:bg-v2-background-bg-layer-02"
                    onClick={() => {
                      openNewTab()
                      setMobileTabs("open", false)
                    }}
                  >
                    <Icon name="plus" />
                    {language.t("command.session.new")}
                  </button>
                  <div class="flex shrink-0 flex-col gap-1 border-t border-v2-border-border-muted pt-2">
                    <button
                      type="button"
                      data-action="mobile-tabs-home"
                      data-state={app.screen === "home" ? "pressed" : undefined}
                      aria-current={app.screen === "home" ? "page" : undefined}
                      class="flex h-7 w-full items-center gap-2 rounded-[6px] px-2 text-[13px] leading-4 text-v2-text-text-faint data-[state=pressed]:text-v2-text-text-base focus-visible:outline-none"
                      onClick={() => {
                        if (app.screen !== "home") goHome()
                        setMobileTabs("open", false)
                      }}
                    >
                      <Icon name="grid-plus" />
                      {language.t("home.title")}
                    </button>
                    <div class="flex items-center gap-1">
                      <button
                        type="button"
                        data-action="mobile-tabs-settings"
                        class="flex h-7 min-w-0 flex-1 items-center gap-2 rounded-[6px] px-2 text-[13px] leading-4 text-v2-text-text-faint hover:bg-v2-background-bg-layer-02 focus-visible:outline-none focus-visible:bg-v2-background-bg-layer-02"
                        onClick={() => setMobileTabs({ open: false, settings: true })}
                      >
                        <Icon name="settings-gear" size="small" />
                        {language.t("sidebar.settings")}
                      </button>
                      <button
                        type="button"
                        data-action="mobile-tabs-help"
                        class="flex h-7 shrink-0 items-center gap-2 rounded-[6px] px-2 text-[13px] leading-4 text-v2-text-text-faint hover:bg-v2-background-bg-layer-02 focus-visible:outline-none focus-visible:bg-v2-background-bg-layer-02"
                        onClick={() => {
                          setMobileTabs("open", false)
                          setHelpDialog(true)
                        }}
                      >
                        <Icon name="help" size="small" />
                        {language.t("sidebar.help")}
                      </button>
                    </div>
                  </div>
                </div>
              </MobileDrawerContent>
            </MobileDrawer>
          }
        >
          <TitlebarTabStrip />
          <RecentlyClosedTabsMenu
            onNewTab={openNewTab}
            tooltip={
              <>
                {language.t("command.session.new")}
                <Keybind keys={[mod, "T"]} variant="neutral" />
              </>
            }
          />
        </Show>
        <Show when={!mobile()}>
          <div class="flex-1" />
        </Show>
      </div>
    </header>
  )
}
