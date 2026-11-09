"use strict";

// The built-in browser of the side panel. Every tab is the guest of a <webview> owned
// by this process: no Node, sandboxed, a profile of its own, http and https
// only. The renderer draws the toolbar and keeps the <webview> over the panel's
// page area (features/panel/WebLayer.tsx); since a <webview> is part of the
// DOM composition, the app's menus draw over the page at full quality.

const fs = require("node:fs");
const path = require("node:path");
const { app, dialog, net, session, shell } = require("electron");
const { BROWSER_PARTITION, publicHost, webUrl } = require("./security.cjs");
const { chordOf } = require("./chords.cjs");

const MAX_TABS = 24;
const MAX_ICON_BYTES = 128 * 1024;
const ICON_TIMEOUT_MS = 3000;
// Live frames stand in for the page while a menu covers it: smaller and JPEG, so about fifteen a second stay cheap.
const MAX_DOWNLOADS = 50;
// A popup is allowed this long after a click or key press in the page.
const GESTURE_MS = 1500;
const POPUP_WINDOW_MS = 10_000;
const POPUP_LIMIT = 4;
// Files that run code are shown in their folder, never opened from here.
const RISKY_TYPES = [
  // Programs and installers.
  "exe", "com", "scr", "msi", "msp", "mst", "msix", "msixbundle", "appx", "appxbundle", "application", "appref-ms", "gadget", "pif", "cpl", "msc", "dll", "ocx", "sys", "drv", "jar", "jnlp", "cab", "diagcab", "inf", "ins", "isp",
  // Scripts and shortcuts.
  "bat", "cmd", "ps1", "ps1xml", "psm1", "psd1", "ps2", "ps2xml", "psc1", "psc2", "vbs", "vbe", "vb", "js", "jse", "wsf", "wsh", "ws", "hta", "lnk", "url", "scf", "reg", "sct", "py", "pyw", "pyz", "rb", "pl", "sh",
  // Documents that carry macros, and help files that run code.
  "docm", "dotm", "xlsm", "xltm", "xlam", "xll", "pptm", "potm", "ppam", "ppsm", "sldm", "xla", "wll", "chm", "hlp", "one",
  // Disk images, which hide what is inside from the scanner.
  "iso", "img", "vhd", "vhdx", "dmg",
  // Windows settings and library files that open programs.
  "settingcontent-ms", "library-ms", "search-ms", "searchconnector-ms", "website",
];
const RISKY = new RegExp(String.raw`\.(${RISKY_TYPES.join("|")})$`, "i");

/** Whether a file would run code, or open a program, when opened: it is shown in its folder, never opened from here. Windows ignores trailing dots and spaces in a name. */
function isRiskyFile(name) {
  return RISKY.test(String(name).replace(/[.  ]+$/, ""));
}
const ZOOMS = [0.25, 0.33, 0.5, 0.67, 0.75, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5, 3, 4, 5];

/** The favicon a page named, as a data URL. Empty when it is not a small public image. */
async function iconData(raw) {
  const url = webUrl(raw);
  if (!url) return "";
  try {
    // Pages must not make this process fetch from inside the user's network.
    if (!(await publicHost(new URL(url).hostname))) return "";
    const res = await net.fetch(url, { signal: AbortSignal.timeout(ICON_TIMEOUT_MS), credentials: "omit", redirect: "manual" });
    const type = (res.headers.get("content-type") ?? "").split(";")[0].trim();
    if (!res.ok || !/^image\/(x-icon|vnd\.microsoft\.icon|png|jpeg|gif|webp|svg\+xml)$/.test(type)) return "";
    const data = Buffer.from(await res.arrayBuffer());
    return data.length > 0 && data.length <= MAX_ICON_BYTES ? `data:${type};base64,${data.toString("base64")}` : "";
  } catch {
    return "";
  }
}

/** A file name that cannot leave the folder, and does not overwrite what is there. */
function uniquePath(dir, wanted) {
  const clean = path.basename(String(wanted ?? "")).replace(/[<>:"/\\|?*\u0000-\u001f]/g, "_").replace(/^\.+/, "").slice(0, 150) || "download";
  const ext = path.extname(clean);
  const base = clean.slice(0, clean.length - ext.length);
  let candidate = path.join(dir, clean);
  for (let n = 1; fs.existsSync(candidate) && n < 1000; n++) candidate = path.join(dir, `${base} (${n})${ext}`);
  return candidate;
}

/** Where downloads go. A test build may name another folder, so tests never write to the person's own. */
function downloadsDir() {
  const dir = !app.isPackaged && process.env.ATLAS_APP_DOWNLOADS;
  if (dir) fs.mkdirSync(dir, { recursive: true });
  return dir || app.getPath("downloads");
}

/** @type {Map<number, BrowserHost>} web contents id of a view -> its host */
const owners = new Map();
let downloadSeq = 0;

class BrowserHost {
  /** @param {import("electron").BrowserWindow} win @param {{lang: () => string}} [hooks] */
  constructor(win, hooks = { lang: () => "en" }, services = {}) {
    this.win = win;
    this.hooks = hooks;
    /** History, zoom and the extension manager, shared by every window. */
    this.services = services;
    /** @type {Map<string, {wc: import("electron").WebContents, icon: string, loading: boolean, gesture: number, popups: number[], failed: boolean, device: object | null, agent: string, login: boolean}>} */
    this.tabs = new Map();
    /** @type {Map<string, {resolve: (wc: import("electron").WebContents) => void, timer: any}>} tabs whose guest is awaited by a caller (an extension's tabs.create) */
    this.waiting = new Map();
    /** The chords of the app's own shortcuts, as the renderer sends them. */
    this.chords = new Set();
    /** @type {{id: number, name: string, host: string, state: string, received: number, total: number, path: string, item: any}[]} */
    this.downloads = [];
  }

  send(channel, value) {
    if (!this.win.isDestroyed()) this.win.webContents.send(channel, value);
  }

  setChords(list) {
    this.chords = new Set(Array.isArray(list) ? list.filter((x) => typeof x === "string").slice(0, 200) : []);
  }

  state(id) {
    const tab = this.tabs.get(id);
    if (!tab) return null;
    const wc = tab.wc;
    const history = wc.navigationHistory;
    const url = webUrl(wc.getURL());
    return {
      id,
      url,
      // A blank guest has no title, and a title is only the page's own once the address is.
      title: url && wc.getTitle() !== "about:blank" ? wc.getTitle() : "",
      favicon: tab.icon,
      loading: tab.loading,
      canGoBack: history.canGoBack(),
      canGoForward: history.canGoForward(),
      zoom: wc.getZoomFactor(),
      device: tab.device ? { id: tab.device.id, name: tab.device.name, width: tab.device.width, height: tab.device.height } : null,
      login: tab.login,
      address: tab.address,
    };
  }

  report(id) {
    const state = this.state(id);
    if (state) this.send("atlas:browser-state", state);
  }

  /**
   * A tab's page is a <webview> of the app's own page (see webview-gate.cjs);
   * its guest exists only once the app has attached it and told us which tab
   * it is. This takes hold of the guest: everything about the page is decided
   * here from then on.
   */
  adopt(id, wc) {
    if (typeof id !== "string" || id.length > 80 || this.tabs.has(id) || this.tabs.size >= MAX_TABS) return null;
    const tab = { wc, icon: "", loading: wc.isLoading(), gesture: 0, popups: [], failed: false, device: null, agent: wc.getUserAgent(), login: false, address: false };
    this.tabs.set(id, tab);
    owners.set(wc.id, this);
    wc.once("destroyed", () => {
      owners.delete(wc.id);
      if (this.tabs.get(id) === tab) {
        this.tabs.delete(id);
        this.services.tabClosed?.(id);
      }
    });
    this.services.extensions?.addTab(wc, this.win);

    // A popup is a tab of the panel, but only when the person did something
    // in the page just before: a page that opens windows on its own gets none.
    const touch = () => {
      tab.gesture = Date.now();
    };
    wc.on("input-event", (_event, input) => {
      if (input.type === "mouseDown" || input.type === "mouseUp" || input.type === "rawKeyDown" || input.type === "keyDown" || input.type === "touchStart") touch();
    });
    wc.setWindowOpenHandler(({ url }) => {
      const target = webUrl(url);
      const now = Date.now();
      tab.popups = tab.popups.filter((at) => now - at < POPUP_WINDOW_MS);
      if (target && now - tab.gesture <= GESTURE_MS && tab.popups.length < POPUP_LIMIT) {
        tab.popups.push(now);
        this.send("atlas:browser-popup", { from: id, url: target });
      }
      return { action: "deny" };
    });
    const guard = (event, url) => {
      if (webUrl(url)) return;
      event.preventDefault();
      // The address bar must show what the page is, not what it was asked to be.
      if (!wc.isLoading()) tab.loading = false;
      this.report(id);
      setTimeout(() => {
        if (this.tabs.get(id) === tab && !wc.isDestroyed() && !wc.isLoading() && tab.loading) {
          tab.loading = false;
          this.report(id);
        }
      }, 400);
    };
    wc.on("will-navigate", guard);
    wc.on("will-redirect", guard);
    wc.on("will-frame-navigate", (event) => {
      if (!webUrl(event.url)) event.preventDefault();
    });

    wc.on("did-start-loading", () => {
      tab.loading = true;
      this.report(id);
    });
    wc.on("did-stop-loading", () => {
      tab.loading = false;
      this.report(id);
    });
    wc.on("did-start-navigation", (details) => {
      if (details.isMainFrame && !details.isSameDocument) tab.failed = false;
    });
    wc.on("did-fail-load", (_event, code, _description, _url, mainFrame) => {
      if (mainFrame && code === -3 && !wc.isLoading()) {
        // An aborted navigation (a blocked redirect) leaves no load running.
        tab.loading = false;
        this.report(id);
      }
      if (mainFrame && code !== -3) {
        tab.failed = true;
        tab.loading = false;
        this.report(id);
      }
    });
    // Only a page that loaded is history; a failed load never is.
    wc.on("did-finish-load", () => {
      if (!tab.failed) this.services.history?.record(wc.getURL(), wc.getTitle());
    });
    wc.on("did-navigate", () => {
      tab.icon = "";
      tab.login = false;
      tab.address = false;
      // The zoom of a site holds in every tab and after a restart.
      const saved = this.services.zoom?.get(wc.getURL());
      if (saved && Math.abs(saved - wc.getZoomFactor()) > 0.001) wc.setZoomFactor(saved);
      this.report(id);
    });
    wc.on("did-navigate-in-page", () => this.report(id));
    wc.on("page-title-updated", (_event, title) => {
      this.services.history?.retitle(wc.getURL(), title);
      this.report(id);
    });
    wc.on("zoom-changed", () => this.report(id));
    wc.on("page-favicon-updated", async (_event, favicons) => {
      for (const href of favicons) {
        const icon = await iconData(href);
        if (icon && this.tabs.get(id) === tab) {
          tab.icon = icon;
          this.report(id);
          return;
        }
      }
    });
    wc.on("render-process-gone", () => {
      tab.loading = false;
      this.report(id);
    });
    wc.on("found-in-page", (_event, result) => {
      this.send("atlas:browser-found", { id, active: result.activeMatchOrdinal, matches: result.matches, final: result.finalUpdate });
    });
    // The shortcuts of the app keep working while the page has focus, and
    // nothing else is taken from the page: Ctrl+Arrow, Ctrl+Backspace,
    // Ctrl+Shift+Z, AltGr characters and the rest reach it untouched.
    wc.on("before-input-event", (event, input) => {
      if (input.type !== "keyDown") return;
      touch();
      // Page-level keys of the browser: find, and zoom for the tab.
      if ((input.control || input.meta) && !input.alt) {
        const key = input.key;
        if (!input.shift && key.toLowerCase() === "f") {
          event.preventDefault();
          this.send("atlas:browser-find-open", { id });
          return;
        }
        if (key === "+" || key === "=" || key === "-" || key === "_" || key === "0") {
          event.preventDefault();
          this.zoom(id, key === "0" ? "reset" : key === "-" || key === "_" ? "out" : "in");
          return;
        }
      }
      const chord = chordOf(input);
      if (!chord || !this.chords.has(chord)) return;
      event.preventDefault();
      this.send("atlas:browser-key", { key: input.key, code: input.code, ctrl: input.control || input.meta, alt: input.alt, shift: input.shift });
    });

    // The page may have started loading (its first address is the tag's own
    // src) before this was attached: what it did meanwhile is caught up on.
    if (!wc.isLoading() && wc.getURL() && !tab.failed) this.services.history?.record(wc.getURL(), wc.getTitle());
    const waiter = this.waiting.get(id);
    if (waiter) {
      clearTimeout(waiter.timer);
      this.waiting.delete(id);
      waiter.resolve(wc);
    }
    this.report(id);
    return this.state(id);
  }

  /** Resolves with the guest of a tab that is about to be attached; rejects if it does not come. */
  expectGuest(id, ms = 10_000) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.waiting.delete(id);
        reject(new Error("The page did not open."));
      }, ms);
      timer.unref?.();
      this.waiting.set(id, { resolve, timer });
    });
  }

  navigate(id, raw) {
    const tab = this.tabs.get(id);
    const url = webUrl(raw);
    if (!tab || !url) return false;
    // A page that is loaded for the person must not take the keyboard from the
    // app (a menu open, an address being typed): focus goes back unless the
    // person clicked or typed in the page since.
    const appHadFocus = this.win.webContents.isFocused();
    const started = Date.now();
    const give = () => {
      // This runs after a load, and the page, the tab or the window can be gone by then.
      if (this.win.isDestroyed() || this.win.webContents.isDestroyed() || tab.wc.isDestroyed()) return;
      if (appHadFocus && tab.gesture < started && tab.wc.isFocused()) {
        // Focus goes back to the app, and the tag lets go of it in the DOM as well.
        this.win.webContents.focus();
        this.send("atlas:browser-blur", { id });
      }
    };
    tab.wc.once("dom-ready", give);
    tab.wc.once("did-finish-load", () => {
      give();
      setTimeout(give, 250);
    });
    tab.wc.loadURL(url).catch(() => {
      // A failed load shows Chromium's own error page; an abort is a redirect.
    });
    return true;
  }

  back(id) {
    const history = this.tabs.get(id)?.wc.navigationHistory;
    if (history?.canGoBack()) history.goBack();
  }

  forward(id) {
    const history = this.tabs.get(id)?.wc.navigationHistory;
    if (history?.canGoForward()) history.goForward();
  }

  /** Marks the tab the person is looking at, for the extension APIs. */
  select(id) {
    const tab = this.tabs.get(id);
    if (tab) this.services.extensions?.selectTab(tab.wc);
  }

  /** The tab a web contents belongs to, by id; "" when it is not one of ours. */
  idOf(contents) {
    for (const [id, tab] of this.tabs) if (tab.wc === contents) return id;
    return "";
  }

  setAddressForm(id, address) {
    const tab = this.tabs.get(id);
    if (tab && tab.address !== !!address) {
      tab.address = !!address;
      this.report(id);
    }
  }

  setLogin(id, login) {
    const tab = this.tabs.get(id);
    if (tab && tab.login !== !!login) {
      tab.login = !!login;
      this.report(id);
    }
  }

  /**
   * Shows the page as a device: its viewport, pixel ratio and user agent
   * together. `null` goes back to the plain panel. Electron's
   * enableDeviceEmulation does nothing for a guest (the page keeps
   * the panel's size), so the same emulation is asked of Chromium through the
   * debugger protocol, which does apply to it.
   * @param {string} id
   * @param {{id: string, name: string, width: number, height: number, ratio: number, mobile: boolean, userAgent: string} | null} device
   */
  async setDevice(id, device) {
    const tab = this.tabs.get(id);
    if (!tab) return false;
    const wc = tab.wc;
    try {
      if (!wc.debugger.isAttached()) wc.debugger.attach("1.3");
      if (!device) {
        await wc.debugger.sendCommand("Emulation.clearDeviceMetricsOverride");
        await wc.debugger.sendCommand("Emulation.setUserAgentOverride", { userAgent: tab.agent });
        wc.debugger.detach();
        tab.device = null;
      } else {
        const width = Math.min(4000, Math.max(200, Math.round(device.width)));
        const height = Math.min(4000, Math.max(200, Math.round(device.height)));
        const ratio = Math.min(5, Math.max(1, Number(device.ratio) || 1));
        await wc.debugger.sendCommand("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: ratio, mobile: !!device.mobile, screenWidth: width, screenHeight: height });
        await wc.debugger.sendCommand("Emulation.setUserAgentOverride", { userAgent: device.userAgent || tab.agent });
        tab.device = { id: device.id, name: device.name, width, height };
      }
    } catch {
      return false;
    }
    wc.reload();
    this.report(id);
    return true;
  }

  reload(id) {
    this.tabs.get(id)?.wc.reload();
  }

  stop(id) {
    this.tabs.get(id)?.wc.stop();
  }

  find(id, text, options = {}) {
    const wc = this.tabs.get(id)?.wc;
    if (!wc || typeof text !== "string") return;
    if (!text) {
      wc.stopFindInPage("clearSelection");
      return;
    }
    wc.findInPage(text.slice(0, 500), { forward: options.forward !== false, findNext: options.findNext === true, matchCase: false });
  }

  stopFind(id) {
    this.tabs.get(id)?.wc.stopFindInPage("clearSelection");
  }

  /**
   * Opens the print dialog. Resolves {ok: true} when the job was printed or the
   * person closed the dialog, and {ok: false, error} when printing could not
   * start: "none" when the system has no printer, "failed" with the reason otherwise.
   */
  async print(id) {
    const wc = this.tabs.get(id)?.wc;
    if (!wc) return { ok: false, error: "failed", reason: "The page is gone." };
    const forced = !app.isPackaged && process.env.ATLAS_APP_TEST_PRINT;
    if (forced) return forced === "none" ? { ok: false, error: "none" } : forced === "cancel" ? { ok: true, cancelled: true } : { ok: false, error: "failed", reason: "test" };
    try {
      if ((await wc.getPrintersAsync()).length === 0) return { ok: false, error: "none" };
    } catch {
      // The list is only a hint; printing itself decides.
    }
    return new Promise((resolve) => {
      try {
        wc.print({ silent: false, printBackground: true }, (success, reason) => {
          if (success || /cancel/i.test(String(reason))) resolve({ ok: true, cancelled: !success });
          else resolve({ ok: false, error: "failed", reason: String(reason ?? "").slice(0, 200) });
        });
      } catch (err) {
        resolve({ ok: false, error: "failed", reason: String(err?.message ?? err).slice(0, 200) });
      }
    });
  }

  /** Steps the page zoom through the usual levels; "reset" returns to 100%. */
  zoom(id, action) {
    const wc = this.tabs.get(id)?.wc;
    if (!wc) return 1;
    const current = wc.getZoomFactor();
    const at = ZOOMS.reduce((best, z, i) => (Math.abs(z - current) < Math.abs(ZOOMS[best] - current) ? i : best), 0);
    const next = action === "reset" ? 1 : action === "in" ? ZOOMS[Math.min(ZOOMS.length - 1, at + 1)] : action === "out" ? ZOOMS[Math.max(0, at - 1)] : current;
    wc.setZoomFactor(next);
    this.services.zoom?.set(wc.getURL(), next);
    this.report(id);
    return next;
  }

  history(id) {
    const history = this.tabs.get(id)?.wc.navigationHistory;
    if (!history) return { entries: [], index: -1 };
    return { entries: history.getAllEntries().map((e) => ({ url: e.url, title: e.title })), index: history.getActiveIndex() };
  }

  goToIndex(id, index) {
    const history = this.tabs.get(id)?.wc.navigationHistory;
    if (history && Number.isInteger(index) && index >= 0 && index < history.length()) history.goToIndex(index);
  }

  /** The text the person has selected in the page, for notes. */
  async selection(id) {
    const wc = this.tabs.get(id)?.wc;
    if (!wc) return "";
    try {
      return String(await wc.executeJavaScript("String(window.getSelection())")).slice(0, 4000);
    } catch {
      return "";
    }
  }

  /**
   * A capture of the page. While a menu is closing the view is still out of
   * the window and gives an empty picture; it is back in a moment, so ask again.
   */
  async capture(wc) {
    for (let attempt = 0; attempt < 12; attempt++) {
      const image = await wc.capturePage();
      if (!image.isEmpty()) return image;
      await new Promise((resolve) => setTimeout(resolve, 150));
    }
    return wc.capturePage();
  }

  /** The page as a PNG, base64, for the composer. */
  async screenshotData(id) {
    const wc = this.tabs.get(id)?.wc;
    if (!wc) return null;
    const image = await this.capture(wc);
    if (image.isEmpty()) return null;
    const png = image.toPNG();
    return png.length <= 10 << 20 ? { name: `page-${Date.now()}.png`, mime: "image/png", content: png.toString("base64") } : null;
  }

  /** The page as a PNG at a path the person chose. Resolves with the path, or "" when they cancelled. */
  async screenshotSave(id) {
    const wc = this.tabs.get(id)?.wc;
    if (!wc) return "";
    const image = await this.capture(wc);
    if (image.isEmpty()) return "";
    const stamp = new Date().toISOString().replace(/[:T]/g, "-").slice(0, 19);
    const forced = !app.isPackaged && process.env.ATLAS_APP_TEST_SAVE;
    if (forced) {
      await fs.promises.writeFile(forced, image.toPNG());
      return forced;
    }
    const picked = await dialog.showSaveDialog(this.win, { defaultPath: path.join(app.getPath("pictures"), `Atlas page ${stamp}.png`), filters: [{ name: "PNG", extensions: ["png"] }] });
    if (picked.canceled || !picked.filePath) return "";
    const target = picked.filePath.toLowerCase().endsWith(".png") ? picked.filePath : `${picked.filePath}.png`;
    await fs.promises.writeFile(target, image.toPNG());
    return target;
  }

  /**
   * Forgets what the person chose. The time range applies to history, the one
   * thing that keeps a time; cookies, storage, the cache and passwords have no
   * date the browser exposes, so for them it is everything.
   * @param {{history?: boolean, cookies?: boolean, cache?: boolean, siteData?: boolean, passwords?: boolean, since?: number}} options
   */
  async clearData(options = {}) {
    const ses = session.fromPartition(BROWSER_PARTITION);
    const result = { history: 0, cookies: false, cache: false, siteData: false, passwords: false };
    if (options.cookies) {
      await ses.clearStorageData({ storages: ["cookies"] });
      result.cookies = true;
    }
    if (options.siteData) {
      await ses.clearStorageData({ storages: ["localstorage", "indexdb", "serviceworkers", "cachestorage", "filesystem", "websql", "shadercache"] });
      result.siteData = true;
    }
    if (options.cache) {
      await ses.clearCache();
      result.cache = true;
    }
    if (options.history) {
      const since = Number.isFinite(options.since) && options.since > 0 ? options.since : 0;
      result.history = this.services.history?.clear(since) ?? 0;
      if (!since) for (const tab of this.tabs.values()) tab.wc.navigationHistory.clear?.();
    }
    if (options.passwords) {
      this.services.vault?.clear();
      result.passwords = true;
    }
    for (const id of this.tabs.keys()) this.report(id);
    return result;
  }

  record(fields) {
    const entry = { id: ++downloadSeq, item: null, ...fields };
    this.downloads.unshift(entry);
    this.downloads.length = Math.min(this.downloads.length, MAX_DOWNLOADS);
    this.send("atlas:browser-download", this.publicDownload(entry));
    return this.publicDownload(entry);
  }

  publicDownload({ item: _item, ...rest }) {
    return rest;
  }

  listDownloads() {
    return this.downloads.map((d) => this.publicDownload(d));
  }

  /** Takes a download: into the Downloads folder, under a name that cannot overwrite or escape. */
  takeDownload(item) {
    const file = uniquePath(downloadsDir(), item.getFilename());
    item.setSavePath(file);
    let host = "";
    try {
      host = new URL(item.getURL()).hostname;
    } catch {
      // A blob or data URL has no host.
    }
    const entry = { id: ++downloadSeq, name: path.basename(file), host, state: "progressing", received: 0, total: item.getTotalBytes(), path: file, item };
    this.downloads.unshift(entry);
    this.downloads.length = Math.min(this.downloads.length, MAX_DOWNLOADS);
    const push = () => this.send("atlas:browser-download", this.publicDownload(entry));
    item.on("updated", (_e, state) => {
      entry.received = item.getReceivedBytes();
      entry.total = item.getTotalBytes();
      entry.state = state === "interrupted" ? "interrupted" : item.isPaused() ? "paused" : "progressing";
      push();
    });
    item.once("done", (_e, state) => {
      entry.received = item.getReceivedBytes();
      entry.state = state === "completed" ? "completed" : state === "cancelled" ? "cancelled" : "interrupted";
      entry.item = null;
      push();
    });
    push();
  }

  downloadAction(id, action) {
    const entry = this.downloads.find((d) => d.id === id);
    if (!entry) return false;
    if (action === "cancel" && entry.item) entry.item.cancel();
    else if (action === "pause" && entry.item && !entry.item.isPaused()) entry.item.pause();
    else if (action === "resume" && entry.item?.canResume()) entry.item.resume();
    else if (action === "open" && entry.state === "completed" && !isRiskyFile(entry.path)) shell.openPath(entry.path);
    else if (action === "show" && entry.state === "completed") shell.showItemInFolder(entry.path);
    else if (action === "remove" && entry.state !== "progressing") this.downloads = this.downloads.filter((d) => d.id !== id);
    else return false;
    return true;
  }

  /** Closes a tab for good: its guest is destroyed. (The app removes the <webview> too.) */
  close(id) {
    const tab = this.tabs.get(id);
    if (!tab) return;
    this.tabs.delete(id);
    this.services.tabClosed?.(id);
    this.services.extensions?.removeTab(tab.wc);
    try {
      if (!tab.wc.isDestroyed()) tab.wc.close();
    } catch {
      // Already gone.
    }
  }

  destroy() {
    for (const id of [...this.tabs.keys()]) this.close(id);
    for (const d of this.downloads) d.item?.cancel();
  }
}

/** Sends the downloads of the browser's profile to the window they came from. */
function watchDownloads(ses) {
  ses.on("will-download", (_event, item, contents) => {
    const host = owners.get(contents?.id);
    if (host) host.takeDownload(item);
    else item.cancel();
  });
}

module.exports = { BrowserHost, iconData, uniquePath, watchDownloads, GESTURE_MS, isRiskyFile };
