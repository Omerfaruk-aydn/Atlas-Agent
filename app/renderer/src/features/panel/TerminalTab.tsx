// A terminal tab: a real shell in the project folder, drawn by xterm.

import { useEffect, useLayoutEffect, useRef, useSyncExternalStore } from "react";
import { t } from "../../lib/i18n";
import type { PanelTab } from "../../lib/panel";
import { useBrowserUI } from "../../stores/browserui";
import { usePanel } from "../../stores/panel";
import { useApp } from "../../stores/app";
import { startShell, terminalFor, termTheme } from "./terminals";

export function TerminalTab({ tab, active }: { tab: PanelTab; active: boolean }) {
  const host = useRef<HTMLDivElement>(null);
  const entry = terminalFor(tab.id);
  const scheme = useApp((s) => s.scheme);
  useSyncExternalStore(
    (fn) => {
      entry.subscribers.add(fn);
      return () => entry.subscribers.delete(fn);
    },
    () => `${entry.status}:${entry.detail}`,
  );

  // The xterm instance is moved into this tab's box, and fitted to it.
  useLayoutEffect(() => {
    const el = host.current;
    if (!el) return;
    const { term, fit } = entry;
    if (!entry.opened) {
      term.open(el);
      entry.opened = true;
    } else if (term.element && term.element.parentElement !== el) {
      el.appendChild(term.element);
    }
    let frame = 0;
    const refit = () => {
      frame = 0;
      if (el.clientWidth > 0 && el.clientHeight > 0) fit.fit();
    };
    // Not while the panel is moving or being dragged: one refit when it has settled.
    const busy = () => useBrowserUI.getState().moving || document.documentElement.dataset.resizing !== undefined;
    const settled = () => {
      if (!frame) frame = requestAnimationFrame(refit);
    };
    window.addEventListener("atlas:panel-settled", settled);
    const stop = useBrowserUI.subscribe((state, before) => {
      if (before.moving && !state.moving) settled();
    });
    refit();
    if (!entry.started) {
      entry.started = true;
      // The tab is named after the shell it runs, as the reference does.
      void startShell(entry).then(() => entry.status === "running" && usePanel.getState().setTabTitle(tab.id, entry.detail));
    }
    const observer = new ResizeObserver(() => {
      if (!busy() && !frame) frame = requestAnimationFrame(refit);
    });
    observer.observe(el);
    return () => {
      window.removeEventListener("atlas:panel-settled", settled);
      stop();
      observer.disconnect();
      if (frame) cancelAnimationFrame(frame);
    };
  }, [entry]);

  useEffect(() => {
    if (active) entry.term.focus();
  }, [active, entry]);

  useEffect(() => {
    entry.term.options.theme = termTheme();
  }, [entry, scheme]);

  return (
    <div className="terminal-tab" onMouseDown={() => entry.term.focus()}>
      <div className="terminal-host" ref={host} />
      {entry.status === "failed" && (
        <div className="terminal-note" role="alert">
          <span>
            {t("panel.terminalFailed")}
            {entry.detail ? `: ${entry.detail}` : ""}
          </span>
        </div>
      )}
      {entry.status === "exited" && (
        <div className="terminal-note" role="status">
          <span>
            {t("panel.terminalExited")} {entry.detail}
          </span>
          <button
            type="button"
            className="button"
            onClick={() => {
              entry.term.reset();
              void startShell(entry).then(() => entry.status === "running" && usePanel.getState().setTabTitle(tab.id, entry.detail));
            }}
          >
            {t("panel.terminalRestart")}
          </button>
        </div>
      )}
    </div>
  );
}
