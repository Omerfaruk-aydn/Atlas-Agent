// The panel on the right of the thread: tabs for a browser and for the
// project's tools. Width, open state and tabs are kept per project.

import { lazy, Suspense, useEffect, useRef, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from "react";
import { reducedMotion, tokenMs } from "../../lib/motion";
import { t } from "../../lib/i18n";
import type { BrowserSettings } from "../../api/types";
import { blurGuest } from "./webviews";
import { clampWidth, PANEL_DEFAULT, PANEL_MIN, setSearchEngine, type PanelTab } from "../../lib/panel";
import { allChords, useKeys } from "../../lib/shortcuts";
import { IconChat, IconClose, IconDiff, IconDriveFile, IconExpand, IconHistory, IconJobs, IconKey2, IconMapPin, IconPlus, IconPuzzle, IconGear, IconPen, IconShrink, IconTerminal } from "../../primitives/icons";
import { Tip } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { useBrowserUI } from "../../stores/browserui";
import { usePanel, usePanelState } from "../../stores/panel";
import { BrowserViews } from "./BrowserBody";
import { ClearDataDialog, CustomDeviceDialog, ImportDialog } from "./Dialogs";
import { SideChatTab } from "./SideChatTab";
import { SiteFavicon } from "./SiteIcon";
import { TerminalTab } from "./TerminalTab";
import { Jobs } from "./Tools";
import { WebTab } from "./WebTab";
import { Changes, Files } from "./Workspace";

const KEY_STEP = 24;
// The design tool loads on first use: it brings its own renderer and export.
const DesignTab = lazy(() => import("../design/DesignTab").then((m) => ({ default: m.DesignTab })));

function useTitle(tab: PanelTab): string {
  const live = usePanel((s) => s.live[tab.id]);
  switch (tab.kind) {
    case "web":
      return live?.title || tab.title || t("panel.newTab");
    case "changes":
      return t("rail.changes");
    case "files":
      return tab.title || t("work.openFile");
    case "terminal":
      return tab.title || t("panel.terminal");
    case "side":
      return t("panel.side");
    case "jobs":
      return t("rail.jobs");
    case "design":
      return tab.title || t("design.title");
    case "history":
      return t("web.history");
    case "passwords":
      return t("vault.title");
    case "extensions":
      return t("web.extensions");
    case "addresses":
      return t("addr.title");
    case "settings":
      return t("bset.title");
  }
}

function TabIcon({ tab }: { tab: PanelTab }) {
  const live = usePanel((s) => s.live[tab.id]);
  switch (tab.kind) {
    case "web":
      return <SiteFavicon src={live?.favicon} />;
    case "changes":
      return <IconDiff />;
    case "files":
      return <IconDriveFile />;
    case "jobs":
      return <IconJobs />;
    case "design":
      return <IconPen />;
    case "side":
      return <IconChat />;
    case "history":
      return <IconHistory />;
    case "passwords":
      return <IconKey2 />;
    case "extensions":
      return <IconPuzzle />;
    case "addresses":
      return <IconMapPin />;
    case "settings":
      return <IconGear />;
    default:
      return <IconTerminal />;
  }
}

function TabButton({ tab, active }: { tab: PanelTab; active: boolean }) {
  const title = useTitle(tab);
  const activate = usePanel((s) => s.activate);
  const closeTab = usePanel((s) => s.closeTab);
  return (
    <div className="ptab" style={{ ["--vt-name" as string]: `ptab-${tab.id.slice(0, 12)}` }} data-active={active || undefined} data-kind={tab.kind} onAuxClick={(e) => e.button === 1 && closeTab(tab.id)}>
      <button type="button" id={`ptab-${tab.id}`} aria-current={active ? "true" : undefined} className="ptab-main" title={title} onClick={() => activate(tab.id)}>
        <TabIcon tab={tab} />
        <span className="ptab-title">{title}</span>
      </button>
      <button type="button" className="ptab-close" aria-label={`${t("panel.closeTab")}: ${title}`} onClick={() => closeTab(tab.id)}>
        <IconClose />
      </button>
    </div>
  );
}

function Body({ tab }: { tab: PanelTab }) {
  switch (tab.kind) {
    case "web":
      return <WebTab tab={tab} active />;
    case "terminal":
      return <TerminalTab tab={tab} active />;
    case "side":
      return <SideChatTab tab={tab} />;
    case "changes":
      return <Changes />;
    case "files":
      return <Files tabId={tab.id} />;
    case "jobs":
      return <Jobs />;
    case "design":
      return (
        <Suspense fallback={<div className="loading" role="status" aria-label={t("app.loading")} />}>
          <DesignTab />
        </Suspense>
      );
    default:
      return <BrowserViews kind={tab.kind} />;
  }
}

/** The drag handle on the panel's left edge. Double click resets the width. */
function Handle({ width }: { width: number }) {
  const start = useRef<{ x: number; width: number; container: number } | null>(null);
  const last = useRef(width);
  const setWidth = usePanel((s) => s.setWidth);

  /** The room the thread and the panel share: the window's body, less the sidebar. */
  const container = (el: HTMLElement) => {
    const body = el.closest<HTMLElement>(".body");
    const sidebar = body?.querySelector<HTMLElement>(".sidebar-slot")?.getBoundingClientRect().width ?? 0;
    return (body?.clientWidth ?? window.innerWidth) - sidebar - 8;
  };
  const apply = (value: number, within: number) => {
    last.current = clampWidth(value, within);
    document.documentElement.style.setProperty("--atlas-panel-eff", `${last.current}px`);
  };

  const onDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    const panel = e.currentTarget.parentElement as HTMLElement;
    start.current = { x: e.clientX, width: panel.getBoundingClientRect().width, container: container(e.currentTarget) };
    last.current = start.current.width;
    document.documentElement.dataset.resizing = "";
  };
  const onMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (start.current) apply(start.current.width + (start.current.x - e.clientX), start.current.container);
  };
  const onUp = () => {
    if (!start.current) return;
    start.current = null;
    delete document.documentElement.dataset.resizing;
    setWidth(last.current);
    // The terminal refits once, now that the width has settled.
    window.dispatchEvent(new Event("atlas:panel-settled"));
  };
  const onKey = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    const step = e.key === "ArrowLeft" ? KEY_STEP : e.key === "ArrowRight" ? -KEY_STEP : 0;
    if (!step) return;
    e.preventDefault();
    const box = (e.currentTarget.parentElement as HTMLElement).getBoundingClientRect();
    apply(box.width + step, container(e.currentTarget));
    setWidth(last.current);
  };

  return (
    <div
      className="panel-handle"
      role="separator"
      aria-orientation="vertical"
      aria-label={t("panel.resize")}
      aria-valuemin={PANEL_MIN}
      aria-valuenow={width}
      tabIndex={0}
      onPointerDown={onDown}
      onPointerMove={onMove}
      onPointerUp={onUp}
      onPointerCancel={onUp}
      onLostPointerCapture={onUp}
      onKeyDown={onKey}
      onDoubleClick={() => {
        document.documentElement.style.removeProperty("--atlas-panel-eff");
        last.current = PANEL_DEFAULT;
        setWidth(PANEL_DEFAULT);
      }}
    />
  );
}

let expanding = false;
let expandTurn = 0;

/**
 * Expands or restores the panel with the window's edge sweeping across it.
 * The content is laid out at its final size at once; only a clip grows or
 * shrinks, so nothing is re-laid-out frame by frame, and the terminal refits
 * once, at the end. A toggle while one is moving is ignored: the state can
 * never be left half-way.
 */
function animateExpand(expanded: boolean, toggle: () => void) {
  const slot = document.querySelector<HTMLElement>(".panel-slot");
  const body = slot?.parentElement;
  if (!slot || !body || reducedMotion()) return toggle();
  // A press while one is running takes over from where it stands.
  if (expanding) {
    slot.getAnimations().forEach((a) => a.cancel());
    slot.style.clipPath = "";
  }
  expanding = true;
  const turn = ++expandTurn;
  const bodyBox = body.getBoundingClientRect();
  const eff = parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--atlas-panel-eff")) || PANEL_DEFAULT;
  const ease = getComputedStyle(document.documentElement).getPropertyValue("--atlas-ease-move").trim() || "ease";
  const finish = () => {
    if (turn !== expandTurn) return;
    slot.style.clipPath = "";
    slot.getAnimations().forEach((a) => a.cancel());
    expanding = false;
    window.dispatchEvent(new Event("atlas:panel-settled"));
  };
  useBrowserUI.getState().markMoving(tokenMs("--atlas-dur-move", 240) + 120);
  if (!expanded) {
    const from = slot.getBoundingClientRect().left - bodyBox.left;
    slot.style.clipPath = `inset(0 0 0 ${from}px)`;
    toggle();
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        const a = slot.animate([{ clipPath: `inset(0 0 0 ${from}px)` }, { clipPath: "inset(0 0 0 0px)" }], { duration: tokenMs("--atlas-dur-move", 240), easing: ease, fill: "forwards" });
        a.finished.then(finish, finish);
      }),
    );
  } else {
    const to = bodyBox.width - eff;
    const a = slot.animate([{ clipPath: "inset(0 0 0 0px)" }, { clipPath: `inset(0 0 0 ${to}px)` }], { duration: tokenMs("--atlas-dur-move-out", 200), easing: ease, fill: "forwards" });
    a.finished.then(
      () => {
        if (turn !== expandTurn) return;
        slot.style.clipPath = `inset(0 0 0 ${to}px)`;
        toggle();
        requestAnimationFrame(() => requestAnimationFrame(finish));
      },
      finish,
    );
  }
}

export function SidePanel() {
  const panel = usePanelState();
  const addTab = usePanel((s) => s.addTab);
  const setOpen = usePanel((s) => s.setOpen);
  const toggleStore = usePanel((s) => s.toggleExpanded);
  const toggleExpanded = () => animateExpand(panel.expanded, toggleStore);
  const keys = useKeys("rail");
  const active = panel.tabs.find((x) => x.id === panel.active) ?? panel.tabs[0];

  const onKeys = (e: ReactKeyboardEvent<HTMLElement>) => {
    if ((e.key !== "ArrowLeft" && e.key !== "ArrowRight") || !(e.target as HTMLElement).classList.contains("ptab-main")) return;
    const at = panel.tabs.findIndex((x) => x.id === active?.id);
    const next = panel.tabs[(at + (e.key === "ArrowRight" ? 1 : -1) + panel.tabs.length) % panel.tabs.length];
    if (!next) return;
    e.preventDefault();
    usePanel.getState().activate(next.id);
    requestAnimationFrame(() => document.getElementById(`ptab-${next.id}`)?.focus());
  };

  return (
    <aside className="side-panel" aria-label={t("panel.title")} data-expanded={panel.expanded || undefined}>
      {!panel.expanded && <Handle width={panel.width} />}
      <div className="panel-head">
        <nav className="ptabs" aria-label={t("panel.tabs")} onKeyDown={onKeys}>
          {panel.tabs.map((tab) => (
            <TabButton key={tab.id} tab={tab} active={tab.id === active?.id} />
          ))}
          <Tip label={t("panel.newTab")}>
            <button type="button" className="icon-button ptab-add" aria-label={t("panel.newTab")} onClick={() => addTab()}>
              <IconPlus />
            </button>
          </Tip>
        </nav>
        <span className="panel-sep" aria-hidden="true" />
        <Tip label={panel.expanded ? t("panel.collapse") : t("panel.expand")}>
          <button type="button" className="icon-button" data-on={panel.expanded || undefined} aria-label={panel.expanded ? t("panel.collapse") : t("panel.expand")} aria-pressed={panel.expanded} onClick={toggleExpanded}>
            {panel.expanded ? <IconShrink /> : <IconExpand />}
          </button>
        </Tip>
        <Tip label={t("panel.close")} keys={keys}>
          <button type="button" className="icon-button" aria-label={t("panel.close")} onClick={() => setOpen(false)}>
            <IconClose />
          </button>
        </Tip>
      </div>
      <div className="panel-body" id="panel-body" role="region" aria-labelledby={active ? `ptab-${active.id}` : undefined}>
        {active && <Body key={active.id} tab={active} />}
      </div>
      <ClearDataDialog />
      <ImportDialog />
      <CustomDeviceDialog />
    </aside>
  );
}

/** Receives what the main process says about browser tabs, downloads and logins, open or not. */
export function PanelEvents() {
  const overrides = useApp((s) => s.ui.shortcuts);
  const ui = useBrowserUI.getState;
  useEffect(() => {
    const browser = window.atlas?.browser;
    if (!browser) return;
    const offState = browser.onState((state) =>
      usePanel.getState().report(state.id, {
        url: state.url,
        title: state.title,
        favicon: state.favicon,
        loading: state.loading,
        canGoBack: state.canGoBack,
        canGoForward: state.canGoForward,
        zoom: state.zoom,
        device: state.device,
        login: state.login,
        address: state.address,
      }),
    );
    const offPopup = browser.onPopup(({ from, url, id }) => usePanel.getState().addTab({ kind: "web", url, ...(id ? { id } : {}) }, from || undefined));
    // Shortcuts pressed while a page has focus come back as key events.
    const offKey = browser.onKey((k) =>
      window.dispatchEvent(new KeyboardEvent("keydown", { key: k.key, code: k.code, ctrlKey: k.ctrl, altKey: k.alt, shiftKey: k.shift, bubbles: true, cancelable: true })),
    );
    const offBlur = browser.onBlur(({ id }) => blurGuest(id));
    const data = window.atlas?.data;
    void data?.downloads.list().then((list) => ui().setDownloads(list));
    const offDownload = data?.downloads.onChange((item) => {
      ui().putDownload(item);
      // A download that starts shows itself.
      if (item.state === "progressing" && item.received === 0) ui().set({ downloadsOpen: true });
    });
    const offOffer = data?.vault.onOffer((offer) => ui().addOffer(offer));
    // The browser's settings, kept in the main process; a search follows the engine chosen there.
    const applySettings = (next: BrowserSettings) => {
      setSearchEngine(next.searchEngine);
      ui().set({ settings: next });
    };
    void data?.settings.get().then(applySettings);
    const offSettings = data?.settings.onChange(applySettings);
    return () => {
      offSettings?.();
      offState();
      offPopup();
      offKey();
      offBlur();
      offDownload?.();
      offOffer?.();
    };
  }, [ui]);

  // The browser view lets exactly the app's own shortcuts through to the app.
  useEffect(() => {
    void window.atlas?.browser?.setChords(allChords(overrides));
  }, [overrides]);
  return null;
}

/**
 * Writes the width the panel really takes into the layout: the saved width,
 * kept within what the window leaves after the sidebar and the thread's
 * minimum. The panel's content is laid out at this width and never changes
 * size while the panel slides in or out.
 */
export function usePanelWidth() {
  const width = usePanelState().width;
  const sidebar = useApp((s) => (s.ui.sidebarOpen ? s.ui.sidebarW : 0));
  useEffect(() => {
    const apply = () => {
      // Narrow windows keep the sidebar smaller (see the 1000px rule in CSS).
      const side = window.innerWidth <= 1000 ? Math.min(sidebar, 220) : sidebar;
      document.documentElement.style.setProperty("--atlas-panel-eff", `${clampWidth(width, window.innerWidth - side - 8)}px`);
    };
    apply();
    window.addEventListener("resize", apply);
    return () => window.removeEventListener("resize", apply);
  }, [width, sidebar]);
}
