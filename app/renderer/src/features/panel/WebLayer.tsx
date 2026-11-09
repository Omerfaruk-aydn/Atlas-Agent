// The root layer of the panel browser: one <webview> per open web tab (see
// webviews.ts for why they live here and not in the panel). It sits above the
// app's content and below its menus, popovers and dialogs, so those draw over
// a page at full quality.

import { useEffect, useLayoutEffect, useRef, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { usePanelState } from "../../stores/panel";
import { dropGuest, ensureGuest, guestIds, hasBeenShown, hasGuest, setLayer, shownVersion, subscribeShown } from "./webviews";

export function WebLayer() {
  const panel = usePanelState();
  const ref = useRef<HTMLDivElement>(null);
  // A tab shown for the first time (the panel mounts after the app is ready) is a reason to look again.
  const shown = useSyncExternalStore(subscribeShown, shownVersion);
  useLayoutEffect(() => {
    setLayer(ref.current);
    return () => setLayer(null);
  }, []);

  // A tab that is closed loses its tag, and with it its page; a tab with an
  // address gets one once it has been shown (a tab opened for an extension is
  // wanted at once, because the extension is waiting for it).
  useEffect(() => {
    const wanted = new Set(panel.tabs.filter((t) => t.kind === "web").map((t) => t.id));
    for (const id of guestIds()) if (!wanted.has(id)) dropGuest(id);
    for (const tab of panel.tabs) {
      if (tab.kind !== "web" || !tab.url || hasGuest(tab.id)) continue;
      if (hasBeenShown(tab.id) || tab.id.startsWith("x")) void ensureGuest(tab.id, tab.url);
    }
  }, [panel.tabs, panel.active, panel.open, shown]);

  return createPortal(<div className="web-layer" ref={ref} />, document.body);
}
