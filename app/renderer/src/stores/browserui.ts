// Small pieces of browser UI state that more than one component needs: which
// of the browser's dialogs and popovers are open, the downloads, and the
// offer to save a login. Nothing here is saved.

import { create } from "zustand";
import type { BrowserSettings, DownloadItem, VaultOffer } from "../api/types";
import { morph } from "../lib/motion";

interface BrowserUI {
  downloadsOpen: boolean;
  extensionsOpen: boolean;
  importOpen: boolean;
  clearOpen: boolean;
  customDeviceOpen: boolean;
  downloads: DownloadItem[];
  /** What the person chose in the browser's settings. */
  settings: BrowserSettings;
  offers: VaultOffer[];
  /** The panel or the sidebar is moving: a terminal waits to refit until it has settled. */
  moving: boolean;
  set(change: Partial<Pick<BrowserUI, "downloadsOpen" | "extensionsOpen" | "importOpen" | "clearOpen" | "customDeviceOpen" | "settings">>): void;
  putDownload(item: DownloadItem): void;
  setDownloads(list: DownloadItem[]): void;
  addOffer(offer: VaultOffer): void;
  /** Marks a move for `ms`; marking again extends it. */
  markMoving(ms: number): void;
  dropOffer(id: string): void;
}

let movingTimer: ReturnType<typeof setTimeout> | 0 = 0;

export const useBrowserUI = create<BrowserUI>((set) => ({
  downloadsOpen: false,
  extensionsOpen: false,
  importOpen: false,
  clearOpen: false,
  customDeviceOpen: false,
  downloads: [],
  settings: { searchEngine: "duckduckgo", offerSave: true, addressAutofill: true },
  offers: [],
  moving: false,
  set: (change) => set(change),
  putDownload: (item) =>
    set((s) => {
      const at = s.downloads.findIndex((d) => d.id === item.id);
      if (at < 0) return { downloads: [item, ...s.downloads].slice(0, 50) };
      const next = s.downloads.slice();
      next[at] = item;
      return { downloads: next };
    }),
  setDownloads: (downloads) => set({ downloads }),
  addOffer: (offer) => morph(() => set((s) => ({ offers: [...s.offers.filter((o) => o.id !== offer.id), offer] }))),
  markMoving: (ms) => {
    if (movingTimer) clearTimeout(movingTimer);
    set({ moving: true });
    movingTimer = setTimeout(() => {
      movingTimer = 0;
      set({ moving: false });
    }, ms);
  },
  dropOffer: (id) => morph(() => set((s) => ({ offers: s.offers.filter((o) => o.id !== id) }))),
}));
