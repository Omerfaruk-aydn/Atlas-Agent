import { useEffect, useRef, useState } from "react";
import { Composer } from "./features/composer/Composer";
import { PermissionDock, QuestionDock } from "./features/composer/Docks";
import { Failed, Home, PickProject } from "./features/home/Screens";
import { Onboarding } from "./features/onboarding/Onboarding";
import { Palette } from "./features/palette/Palette";
import { PanelEvents, SidePanel, usePanelWidth } from "./features/panel/SidePanel";
import { WebLayer } from "./features/panel/WebLayer";
import { ThreadHeader } from "./features/thread/ThreadHeader";
import { SettingsDialog } from "./features/settings/Settings";
import { Sidebar } from "./features/sidebar/Sidebar";
import { RewindDialog, Thread } from "./features/thread/Thread";
import { t } from "./lib/i18n";
import { useKeys, useShortcuts } from "./lib/shortcuts";
import { AtlasMark, IconBranch, IconPanelRight, IconSearch, IconSidebar } from "./primitives/icons";
import { Collapse } from "./primitives/motion";
import { Kbd, Tip, TipProvider, Toasts } from "./primitives/ui";
import { tokenMs, usePresence } from "./lib/motion";
import { useApp } from "./stores/app";
import { useBrowserUI } from "./stores/browserui";
import { usePanelState } from "./stores/panel";

function TitleBar() {
  const workspace = useApp((s) => s.workspace);
  const git = useApp((s) => s.git);
  const toggleSidebar = useApp((s) => s.toggleSidebar);
  const toggleRail = useApp((s) => s.toggleRail);
  const railOpen = usePanelState().open;
  const openDialog = useApp((s) => s.openDialog);
  const sidebarKeys = useKeys("sidebar");
  const railKeys = useKeys("rail");
  const paletteKeys = useKeys("palette");
  return (
    <header className="titlebar">
      <span className="titlebar-mark">
        <AtlasMark />
      </span>
      {workspace && (
        <Tip label={t("sidebar.toggle")} keys={sidebarKeys}>
          <button type="button" className="icon-button" aria-label={t("sidebar.toggle")} onClick={toggleSidebar}>
            <IconSidebar />
          </button>
        </Tip>
      )}
      <div className="crumbs">
        <span className="crumb-project" title={workspace?.name ?? "Atlas"}>{workspace?.name ?? "Atlas"}</span>
        {git && (
          <span className="branch-chip" title={`${git.branch}${git.dirty ? ` · ${git.dirty} ${t("git.changed")}` : ""}`}>
            <IconBranch />
            <span>{git.branch}</span>
            {git.dirty > 0 && <span className="branch-dirty" aria-hidden="true" />}
          </span>
        )}
      </div>
      {workspace && (
        <div className="titlebar-end">
          <button type="button" className="palette-button" onClick={() => openDialog("palette")}>
            <IconSearch />
            <span className="palette-label">{t("palette.open")}</span>
            {paletteKeys && <Kbd keys={paletteKeys} />}
          </button>
          <Tip label={t("rail.toggle")} keys={railKeys}>
            <button type="button" className="icon-button" data-on={railOpen || undefined} aria-label={t("rail.toggle")} aria-pressed={railOpen} onClick={() => toggleRail()}>
              <IconPanelRight />
            </button>
          </Tip>
        </div>
      )}
    </header>
  );
}

/** Says the connection dropped, and leaves on its own once it is back. */
function ConnectionBanner() {
  const online = useApp((s) => s.online);
  const reconnect = useApp((s) => s.reconnect);
  const retry = useApp((s) => s.retry);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (online || !retry) return;
    const id = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(id);
  }, [online, retry]);
  const seconds = retry ? Math.max(0, Math.ceil((retry.at - now) / 1000)) : 0;
  return (
    <Collapse open={!online}>
      <div className="conn-banner" role="status">
        <span>{retry && retry.attempt > 1 && seconds > 0 ? t("conn.retrying").replace("{0}", String(seconds)).replace("{1}", String(retry.attempt)) : t("conn.offline")}</span>
        <button type="button" className="link-button" onClick={reconnect}>
          {t("conn.retry")}
        </button>
      </div>
    </Collapse>
  );
}

function Conversation({ sessionId }: { sessionId: string }) {
  const permission = useApp((s) => s.permissions.find((p) => p.session_id === sessionId));
  const question = useApp((s) => s.questions.find((q) => q.session_id === sessionId));
  const notice = useApp((s) => s.notice);
  return (
    <div className="conversation">
      <Thread sessionId={sessionId} />
      <div className="composer-dock">
        <div className="composer-wrap">
          {notice && <div className="notice">{notice}</div>}
          {permission ? (
            <PermissionDock key={permission.id} request={permission} />
          ) : question ? (
            <QuestionDock key={question.id} request={question} />
          ) : (
            <Composer sessionId={sessionId} autoFocus />
          )}
        </div>
      </div>
    </div>
  );
}

function useLayoutVars() {
  const sidebarW = useApp((s) => s.ui.sidebarW);
  useEffect(() => {
    document.documentElement.style.setProperty("--atlas-sidebar-w", `${sidebarW}px`);
  }, [sidebarW]);
  usePanelWidth();
}

export function App() {
  const phase = useApp((s) => s.phase);
  const currentId = useApp((s) => s.currentId);
  const sidebarOpen = useApp((s) => s.ui.sidebarOpen);
  const panel = usePanelState();
  const want = panel.open && phase === "ready";
  // The panel stays mounted until it has finished leaving. While anything moves,
  // the terminals wait to refit until it has settled (markMoving).
  const presence = usePresence(want, Math.round(tokenMs("--atlas-dur-move-out", 200)) + 40);
  const railOpen = presence.mounted;
  const drawnOpen = presence.on && want;
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    useBrowserUI.getState().markMoving(Math.round(tokenMs("--atlas-dur-move", 240)) + 80);
    if (!want) document.querySelector<HTMLElement>(".panel-slot")?.getAnimations().forEach((a) => a.cancel());
  }, [want]);
  const sidebarFirst = useRef(true);
  useEffect(() => {
    if (sidebarFirst.current) {
      sidebarFirst.current = false;
      return;
    }
    useBrowserUI.getState().markMoving(Math.round(tokenMs("--atlas-dur-move", 240)) + 80);
  }, [sidebarOpen]);
  useShortcuts();
  useLayoutVars();
  useEffect(() => { window.atlas?.rendererReady?.(); }, []);

  return (
    <TipProvider delayDuration={500} skipDelayDuration={200}>
      <div className="app">
        <TitleBar />
        {phase === "ready" ? (
          <div className="body" data-sidebar={sidebarOpen ? "open" : "closed"} data-panel={drawnOpen ? "open" : "closed"} data-expanded={(drawnOpen && panel.expanded) || undefined}>
            <div className="sidebar-slot" inert={!sidebarOpen || (drawnOpen && panel.expanded)}>
              <Sidebar />
            </div>
            <main className="card" inert={drawnOpen && panel.expanded}>
              <ConnectionBanner />
              {currentId && <ThreadHeader />}
              <div className="card-content">{currentId ? <Conversation key={currentId} sessionId={currentId} /> : <Home />}</div>
            </main>
            <div className="panel-slot" inert={!drawnOpen}>
              {railOpen && <SidePanel />}
            </div>
          </div>
        ) : (
          <main className="card card-full">
            {phase === "pick" && <PickProject />}
            {phase === "failed" && <Failed />}
            {phase === "loading" && <div className="loading" role="status" aria-label={t("app.loading")} />}
          </main>
        )}
        <PanelEvents />
        <WebLayer />
        <Palette />
        <SettingsDialog />
        <RewindDialog />
        <Onboarding />
        <Toasts />
      </div>
    </TipProvider>
  );
}
