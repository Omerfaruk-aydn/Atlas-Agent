import { useEffect } from "react";
import { Composer } from "./features/composer/Composer";
import { PermissionDock, QuestionDock } from "./features/composer/Docks";
import { Failed, Home, PickProject } from "./features/home/Screens";
import { ModelPicker } from "./features/models/ModelPicker";
import { Onboarding } from "./features/onboarding/Onboarding";
import { Palette } from "./features/palette/Palette";
import { Rail } from "./features/rail/Rail";
import { SettingsDialog } from "./features/settings/Settings";
import { Sidebar } from "./features/sidebar/Sidebar";
import { RewindDialog, Thread } from "./features/thread/Thread";
import { t } from "./lib/i18n";
import { useKeys, useShortcuts } from "./lib/shortcuts";
import { AtlasMark, IconBranch, IconPanelRight, IconSearch, IconSidebar } from "./primitives/icons";
import { Collapse } from "./primitives/motion";
import { Kbd, Tip, TipProvider, Toasts } from "./primitives/ui";
import { useApp } from "./stores/app";

function TitleBar() {
  const workspace = useApp((s) => s.workspace);
  const title = useApp((s) => s.sessions.find((x) => x.id === s.currentId)?.title);
  const git = useApp((s) => s.git);
  const toggleSidebar = useApp((s) => s.toggleSidebar);
  const toggleRail = useApp((s) => s.toggleRail);
  const railOpen = useApp((s) => s.ui.railOpen);
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
        <span className="crumb-project">{workspace?.name ?? "Atlas"}</span>
        {git && (
          <span className="branch-chip" title={`${git.branch}${git.dirty ? ` · ${git.dirty} ${t("git.changed")}` : ""}`}>
            <IconBranch />
            <span>{git.branch}</span>
            {git.dirty > 0 && <span className="branch-dirty" aria-hidden="true" />}
          </span>
        )}
        {title && <span className="crumb-sep">/</span>}
        {title && <span className="crumb-title">{title}</span>}
      </div>
      {workspace && (
        <div className="titlebar-end">
          <button type="button" className="palette-button" onClick={() => openDialog("palette")}>
            <IconSearch />
            <span>{t("palette.open")}</span>
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
  return (
    <Collapse open={!online}>
      <div className="conn-banner" role="status">
        <span>{t("conn.offline")}</span>
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
  const railW = useApp((s) => s.ui.railW);
  useEffect(() => {
    const root = document.documentElement.style;
    root.setProperty("--atlas-sidebar-w", `${sidebarW}px`);
    root.setProperty("--atlas-rail-w", `${railW}px`);
  }, [sidebarW, railW]);
}

export function App() {
  const phase = useApp((s) => s.phase);
  const currentId = useApp((s) => s.currentId);
  const sidebarOpen = useApp((s) => s.ui.sidebarOpen);
  const railOpen = useApp((s) => s.ui.railOpen);
  useShortcuts();
  useLayoutVars();

  return (
    <TipProvider delayDuration={500} skipDelayDuration={200}>
      <div className="app">
        <TitleBar />
        {phase === "ready" ? (
          <div className="body" data-sidebar={sidebarOpen ? "open" : "closed"}>
            <div className="sidebar-slot" inert={!sidebarOpen}>
              <Sidebar />
            </div>
            <main className="card" data-rail={railOpen ? "open" : "closed"}>
              <ConnectionBanner />
              <div className="card-main">
                <div className="card-content">{currentId ? <Conversation key={currentId} sessionId={currentId} /> : <Home />}</div>
                <div className="rail-slot" inert={!railOpen}>
                  {railOpen && <Rail />}
                </div>
              </div>
            </main>
          </div>
        ) : (
          <main className="card card-full">
            {phase === "pick" && <PickProject />}
            {phase === "failed" && <Failed />}
            {phase === "loading" && <div className="loading" role="status" aria-label={t("app.loading")} />}
          </main>
        )}
        <Palette />
        <ModelPicker />
        <SettingsDialog />
        <RewindDialog />
        <Onboarding />
        <Toasts />
      </div>
    </TipProvider>
  );
}
