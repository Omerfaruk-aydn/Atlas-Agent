// A browser tab: toolbar, address bar, and the page. The page is a <webview>
// kept in the app's root layer (webviews.ts, WebLayer.tsx); this component only
// says which box of the panel it goes in. Menus, popovers, the find bar and
// banners are ordinary DOM: they draw over or beside the page, and the page
// follows its box.

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";
import { t } from "../../lib/i18n";
import { morph } from "../../lib/motion";
import { hostOf, isWebUrl, normalizeAddress, type PanelTab } from "../../lib/panel";
import { IconArrowLeft, IconArrowRight, IconClose, IconExternal, IconRetry, IconStop } from "../../primitives/icons";
import { Tip } from "../../primitives/ui";
import { usePanel } from "../../stores/panel";
import { NewTab } from "./NewTab";
import { AddressButton, BrowserMenu, DownloadsButton, ExtensionsButton, FindBar, KeyButton, NotesButton, SaveLoginBar } from "./WebParts";
import { registerRegion } from "./webviews";

export function WebTab({ tab, active }: { tab: PanelTab; active: boolean }) {
  const live = usePanel((s) => s.live[tab.id]);
  const region = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState<string | null>(null);
  const [find, setFind] = useState(false);
  // The page is shown once it has an address, is loading one, or comes back from a saved tab.
  // A tab with an address wants its page even while its guest is still blank.
  const hasPage = !!tab.url || !!live?.url || !!live?.loading;
  const browser = window.atlas?.browser;

  // Find in page: Ctrl+F in the page (via the main process) or in the app.
  useEffect(() => {
    if (!browser || !active) return;
    const off = browser.onFindOpen((f) => f.id === tab.id && morph(() => setFind(true)));
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "f" && usePanel.getState().live[tab.id]?.url) {
        e.preventDefault();
        morph(() => setFind(true));
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      off();
      window.removeEventListener("keydown", onKey);
    };
  }, [browser, active, tab.id]);

  useEffect(() => {
    if (active && hasPage) void browser?.select(tab.id);
  }, [active, hasPage, browser, tab.id]);

  // Gives the address bar to a tab that has just been opened empty.
  useEffect(() => {
    if (active && !hasPage) input.current?.focus();
  }, [active, hasPage]);

  // The page goes in this box while the tab is in front. The box can change
  // size or place however it likes (the find bar, the panel sliding): the
  // layer follows it every frame.
  useLayoutEffect(() => {
    const el = region.current;
    if (!active || !hasPage || !el) return;
    return registerRegion(tab.id, el);
  }, [active, hasPage, tab.id]);

  const go = (raw: string) => {
    const url = normalizeAddress(raw);
    if (!url || !browser) return;
    // A tab with a page sends it there; an empty one gets its first address, and its page is made.
    if (usePanel.getState().live[tab.id]) void browser.navigate(tab.id, url);
    else usePanel.getState().setTabUrl(tab.id, url);
    setDraft(null);
    input.current?.blur();
  };

  // The guest's own address, or, while it has none (blank, or a load that was stopped), the one the tab was sent to.
  const address = live?.url || tab.url || "";
  // The pill shows the host; the whole address only while it is being edited.
  const shown = draft ?? (isWebUrl(address) ? hostOf(address) : address);
  const page = isWebUrl(address);

  return (
    <div className="web-tab">
      <div className="web-toolbar">
        <div className="bar-pill nav-pill">
          <Tip label={t("panel.back")}>
            <button type="button" className="icon-button" aria-label={t("panel.back")} disabled={!live?.canGoBack} onClick={() => void browser?.back(tab.id)}>
              <IconArrowLeft />
            </button>
          </Tip>
          <Tip label={t("panel.forward")}>
            <button type="button" className="icon-button" aria-label={t("panel.forward")} disabled={!live?.canGoForward} onClick={() => void browser?.forward(tab.id)}>
              <IconArrowRight />
            </button>
          </Tip>
          <span className="pill-sep" aria-hidden="true" />
          <Tip label={live?.loading ? t("panel.stop") : t("panel.reload")}>
            <button
              type="button"
              className="icon-button"
              aria-label={live?.loading ? t("panel.stop") : t("panel.reload")}
              disabled={!hasPage}
              onClick={() => void (live?.loading ? browser?.stop(tab.id) : browser?.reload(tab.id))}
            >
              {live?.loading ? <IconStop /> : <IconRetry />}
            </button>
          </Tip>
        </div>
        {page && <NotesButton tab={tab.id} url={address} />}
        <form
          className="address"
          onSubmit={(e) => {
            e.preventDefault();
            go(shown);
          }}
        >
          {live?.device && (
            <button type="button" className="address-chip" title={t("web.deviceOff")} onClick={() => void browser?.device(tab.id, null)}>
              {live.device.name} <IconClose />
            </button>
          )}
          <input
            ref={input}
            value={shown}
            placeholder={t("panel.address")}
            aria-label={t("panel.address")}
            spellCheck={false}
            autoCapitalize="off"
            autoComplete="off"
            onChange={(e) => setDraft(e.target.value)}
            onFocus={(e) => {
              const el = e.currentTarget;
              // The whole address is there, and selected, before anything is typed.
              flushSync(() => setDraft(address));
              el.select();
            }}
            onBlur={() => setDraft(null)}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                setDraft(null);
                e.currentTarget.blur();
              }
            }}
          />
          {live && Math.abs(live.zoom - 1) > 0.01 && (
            <button type="button" className="address-chip" title={t("web.zoomReset")} onClick={() => void browser?.zoom(tab.id, "reset")}>
              {Math.round(live.zoom * 100)}%
            </button>
          )}
          {page && (
            <Tip label={t("panel.openBrowser")}>
              <button type="button" className="icon-button address-open" aria-label={t("panel.openBrowser")} onClick={() => void window.atlas?.openExternal(address)}>
                <IconExternal />
              </button>
            </Tip>
          )}
          {live?.login && <KeyButton tab={tab.id} />}
          {live?.address && <AddressButton tab={tab.id} />}
        </form>
        <div className="bar-pill tool-pill">
          <ExtensionsButton tab={tab.id} />
          <DownloadsButton />
        </div>
        <BrowserMenu tab={tab} live={live} url={address} onFind={() => morph(() => setFind(true))} />
      </div>
      {find && page && (
        <FindBar
          tab={tab.id}
          onClose={() => {
            morph(() => setFind(false));
            void browser?.stopFind(tab.id);
          }}
        />
      )}
      <SaveLoginBar tab={tab.id} />
      <div className="web-stage">
        {hasPage ? (
          <>
            {live?.loading && <div className="web-progress" role="progressbar" aria-label={t("panel.loading")} />}
            <div className="web-view" ref={region} data-tab={tab.id} />
          </>
        ) : (
          <NewTab onOpen={go} />
        )}
      </div>
    </div>
  );
}
