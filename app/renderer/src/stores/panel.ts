// The side panel's state: which tabs a project has open, how wide the panel
// is, and what each browser tab reports. What must survive a restart goes to
// the main process under ui.panels and ui.recentSites, by project.

import { create } from "zustand";
import { ApiError, http } from "../api/http";
import type { Session } from "../api/types";
import { t } from "../lib/i18n";
import {
  emptyPanel,
  MAX_TABS,
  newTabId,
  PANEL_MIN,
  projectKey,
  rememberSite,
  sanitizePanel,
  sanitizeRecent,
  type PanelTab,
  type BrowserView,
  type PanelTool,
  type ProjectPanel,
  type RecentSite,
} from "../lib/panel";
import { morph } from "../lib/motion";
import { loadUI } from "../lib/prefs";
import { useApp } from "./app";

export type { PanelTool } from "../lib/panel";

/** What the main process reports about a browser tab. */
export interface LiveTab {
  url: string;
  title: string;
  favicon: string;
  loading: boolean;
  canGoBack: boolean;
  canGoForward: boolean;
  zoom: number;
  device: { id: string; name: string; width: number; height: number } | null;
  /** The page shows a login form. */
  login: boolean;
  /** The page shows an address form. */
  address: boolean;
}

interface PanelState {
  panels: Record<string, ProjectPanel>;
  recent: RecentSite[];
  live: Record<string, LiveTab>;
  /** A file the Files tab should open. */
  fileTarget: string;

  toggle(tool?: PanelTool): void;
  setOpen(open: boolean): void;
  setWidth(width: number): void;
  toggleExpanded(): void;
  activate(id: string): void;
  /** Names a tool tab: the shell's path, the file that is open. */
  setTabTitle(id: string, title: string): void;
  /** The chat a note or screenshot goes to: the side chat in front, or the open chat. */
  activeThread(): string | undefined;
  /** Opens the browser's history, passwords or extensions, once. */
  openView(view: BrowserView): void;
  addTab(tab?: Partial<PanelTab>, after?: string): string;
  closeTab(id: string): void;
  /** `fresh` opens another terminal instead of showing the one that is open. */
  openTool(tool: PanelTool, options?: { fresh?: boolean }): void;
  openFile(path: string): void;
  /** Shows the design tool, with a design file open when one is named. */
  openDesign(name?: string): void;
  openUrl(url: string, from?: string): void;
  openSideChat(sessionId: string): void;
  newSideChat(): Promise<Session | undefined>;
  setTabUrl(id: string, url: string): void;
  report(id: string, live: LiveTab): void;
}

const initial = loadUI();
const SIDE_KINDS: ReadonlySet<string> = new Set(["changes", "files", "jobs", "terminal", "side", "design"]);

/** The main process merges one project's panel into the saved ones, so a window never erases another's. */
function savePanel(key: string, panel: ProjectPanel) {
  void window.atlas?.setPanel(key, panel);
}

/**
 * Side chats the person has just started. Their composer takes the focus once
 * it appears, so what they type next goes into the new chat and not the main
 * thread; one restored with the panel at launch leaves the focus alone.
 */
export const focusSideChat = new Set<string>();

const key = () => projectKey(useApp.getState().workspace?.path ?? useApp.getState().project);

export const usePanel = create<PanelState>((set, get) => {
  /** Applies a change to this project's panel and saves it. */
  /**
   * Applies a change to this project's panel and saves it. A change to the
   * tabs themselves (one opens, closes, is switched to, turns into a tool) is
   * a view transition, so the strip and the body move instead of jumping;
   * a quiet change (the width, a title) is plain.
   */
  const edit = (change: (panel: ProjectPanel) => ProjectPanel | void, quiet = false) => {
    const apply = () => {
      const k = key();
      const current = get().panels[k] ?? sanitizePanel(initial.panels[k]);
      const draft = { ...current, tabs: current.tabs.slice() };
      const next = change(draft) ?? draft;
      const panels = { ...get().panels, [k]: next };
      set({ panels });
      savePanel(k, next);
    };
    if (quiet) apply();
    else morph(apply);
  };
  const current = (): ProjectPanel => {
    const k = key();
    return get().panels[k] ?? sanitizePanel(initial.panels[k]);
  };
  const blank = (panel: ProjectPanel) => panel.tabs.find((x) => x.id === panel.active && x.kind === "web" && !x.url && !get().live[x.id]?.url);

  return {
    panels: {},
    recent: sanitizeRecent(initial.recentSites),
    live: {},
    fileTarget: "",

    toggle(tool) {
      const panel = current();
      if (tool) {
        const active = panel.tabs.find((x) => x.id === panel.active);
        if (panel.open && active?.kind === tool) get().setOpen(false);
        else get().openTool(tool);
        return;
      }
      get().setOpen(!panel.open);
    },

    setOpen(open) {
      edit((panel) => {
        panel.open = open;
        if (!open) panel.expanded = false;
        if (open && panel.tabs.length === 0) {
          const id = newTabId();
          panel.tabs.push({ id, kind: "web" });
          panel.active = id;
        }
      }, true);
    },

    setWidth(width) {
      edit((panel) => {
        panel.width = Math.max(PANEL_MIN, Math.round(width));
      }, true);
    },

    toggleExpanded() {
      edit((panel) => {
        panel.expanded = !panel.expanded;
      }, true);
    },

    activeThread() {
      const panel = current();
      const tab = panel.tabs.find((x) => x.id === panel.active);
      return panel.open && tab?.kind === "side" ? tab.sessionId : (useApp.getState().currentId ?? undefined);
    },

    setTabTitle(id, title) {
      const tab = current().tabs.find((x) => x.id === id);
      if (!tab || tab.title === title) return;
      edit((panel) => {
        panel.tabs = panel.tabs.map((x) => (x.id === id ? { ...x, title: title.slice(0, 400) } : x));
      }, true);
    },

    openView(view) {
      const existing = current().tabs.find((x) => x.kind === view);
      if (existing) {
        get().activate(existing.id);
        get().setOpen(true);
        return;
      }
      get().addTab({ kind: view });
    },

    activate(id) {
      edit((panel) => {
        if (panel.tabs.some((x) => x.id === id)) panel.active = id;
      });
    },

    addTab(tab = {}, after) {
      const id = tab.id ?? newTabId();
      edit((panel) => {
        if (panel.tabs.length >= MAX_TABS) {
          useApp.getState().toast(t("panel.tooManyTabs"), "danger");
          return;
        }
        const at = after ? panel.tabs.findIndex((x) => x.id === after) : -1;
        const entry: PanelTab = { kind: "web", ...tab, id };
        panel.tabs.splice(at >= 0 ? at + 1 : panel.tabs.length, 0, entry);
        panel.active = id;
        panel.open = true;
      });
      return id;
    },

    closeTab(id) {
      const tab = current().tabs.find((x) => x.id === id);
      if (!tab) return;
      if (tab.kind === "web") void window.atlas?.browser.close(id);
      if (tab.kind === "terminal") {
        void window.atlas?.terminal.kill(id);
        // The terminal view loads on demand, so the store stays light.
        void import("../features/panel/terminals").then((m) => m.disposeTerminal(id));
      }
      set((s) => {
        const live = { ...s.live };
        delete live[id];
        return { live };
      });
      edit((panel) => {
        const at = panel.tabs.findIndex((x) => x.id === id);
        panel.tabs.splice(at, 1);
        if (panel.active === id) panel.active = (panel.tabs[Math.min(at, panel.tabs.length - 1)] ?? panel.tabs[0])?.id ?? "";
        if (panel.tabs.length === 0) {
          panel.open = false;
          panel.expanded = false;
        }
      });
    },

    openTool(tool, options) {
      if (!SIDE_KINDS.has(tool)) return;
      if (tool === "side") {
        const existing = current().tabs.find((x) => x.kind === "side");
        if (existing) {
          get().activate(existing.id);
          get().setOpen(true);
        } else {
          void get().newSideChat();
        }
        return;
      }
      const panel = current();
      const existing = options?.fresh && tool === "terminal" ? undefined : panel.tabs.find((x) => x.kind === tool);
      const placeholder = blank(panel);
      if (existing) {
        edit((p) => {
          p.active = existing.id;
          p.open = true;
          // A new tab page that led here is not needed any more.
          if (placeholder && placeholder.id !== existing.id) p.tabs = p.tabs.filter((x) => x.id !== placeholder.id);
        });
        return;
      }
      if (placeholder) {
        // The new tab page turns into the tool, in place.
        edit((p) => {
          p.tabs = p.tabs.map((x) => (x.id === placeholder.id ? { id: newTabId(), kind: tool } : x));
          p.active = p.tabs[p.tabs.findIndex((x) => x.kind === tool)]?.id ?? p.active;
          p.open = true;
        });
        return;
      }
      get().addTab({ kind: tool });
    },

    openFile(path) {
      set({ fileTarget: path });
      get().openTool("files");
    },

    openDesign(name) {
      if (name) void import("./design").then(({ useDesign }) => useDesign.setState({ target: name }));
      get().openTool("design");
    },

    openUrl(url, from) {
      get().addTab({ kind: "web", url }, from);
    },

    openSideChat(sessionId) {
      const panel = current();
      const existing = panel.tabs.find((x) => x.kind === "side" && x.sessionId === sessionId);
      if (existing) {
        get().activate(existing.id);
        get().setOpen(true);
        return;
      }
      const placeholder = blank(panel);
      const tab: PanelTab = { id: newTabId(), kind: "side", sessionId };
      edit((p) => {
        if (placeholder) p.tabs = p.tabs.map((x) => (x.id === placeholder.id ? tab : x));
        else p.tabs.push(tab);
        p.active = tab.id;
        p.open = true;
      });
    },

    async newSideChat() {
      const app = useApp.getState();
      const source = app.currentId;
      if (!source) {
        app.toast(t("panel.sideNeedsChat"));
        return undefined;
      }
      try {
        const session = await http.post<Session>(`/api/workspaces/${encodeURIComponent(app.workspace?.id ?? "")}/sessions/${encodeURIComponent(source)}/side-chat`, {});
        useApp.setState((s) => ({ sideChats: [session, ...s.sideChats.filter((x) => x.id !== session.id)] }));
        focusSideChat.add(session.id);
        get().openSideChat(session.id);
        return session;
      } catch (err) {
        const old = err instanceof ApiError && err.status === 501;
        useApp.getState().toast(old ? t("panel.sideOld") : `${t("panel.sideFailed")}: ${err instanceof Error ? err.message : String(err)}`, "danger");
        return undefined;
      }
    },

    setTabUrl(id, url) {
      const tab = current().tabs.find((x) => x.id === id);
      if (!tab || tab.url === url) return;
      edit((panel) => {
        panel.tabs = panel.tabs.map((x) => (x.id === id ? { ...x, url } : x));
      }, true);
    },

    report(id, live) {
      set((s) => ({ live: { ...s.live, [id]: live } }));
      const tab = current().tabs.find((x) => x.id === id);
      if (!tab || tab.kind !== "web" || !live.url) return;
      const settled = !live.loading;
      if (tab.url !== live.url || (settled && live.title && tab.title !== live.title)) {
        const before = get().recent;
        const recent = settled ? rememberSite(before, live.url, live.title) : before;
        const k = key();
        const panel = current();
        const next = { ...panel, tabs: panel.tabs.map((x) => (x.id === id ? { ...x, url: live.url, title: live.title || x.title } : x)) };
        set({ panels: { ...get().panels, [k]: next }, recent });
        savePanel(k, next);
        if (settled && recent !== before) void window.atlas?.setRecent(recent).then((merged) => merged && set({ recent: merged }));
      }
    },
  };
});

/** This project's panel as the components see it. */
const NONE = emptyPanel();
export function usePanelState(): ProjectPanel {
  const k = useApp((s) => projectKey(s.workspace?.path ?? s.project));
  return usePanel((s) => s.panels[k]) ?? hydrated(k);
}

const cache = new Map<string, ProjectPanel>();
function hydrated(k: string): ProjectPanel {
  if (!k) return NONE;
  let panel = cache.get(k);
  if (!panel) {
    panel = sanitizePanel(initial.panels[k]);
    cache.set(k, panel);
  }
  return panel;
}
