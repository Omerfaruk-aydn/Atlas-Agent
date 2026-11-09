"use strict";

// The gate every <webview> of the app has to pass. The panel browser shows each
// page in a <webview> tag, so the page is part of the app's own DOM
// composition and menus draw over it at full quality. A <webview> is also a
// way for a page to get a process of its own, so the main process decides
// everything about it:
//
//   1. The app's own page says first, through `atlas:browser-expect`, that it
//      is about to attach one for a tab. The note is used once, lasts a few
//      seconds, and only a few can wait at a time. A <webview> that nobody
//      announced (markdown, model output or page content that managed to put
//      one into the renderer) is refused: `will-attach-webview` is cancelled
//      and no guest is ever made.
//   2. An announced one is attached on our terms: the partition of the
//      browser's profile only, an http(s), about:blank or empty address, our own guest
//      preload, a sandbox, no Node, no popups, no web security switches, no
//      string of web preferences from the page.
//   3. Once the guest exists the app tells us which tab it is
//      (`atlas:browser-attach`). The id is never trusted on its own: it must
//      be a guest of type "webview" whose host is the sender, and it must be
//      one this gate saw attach for exactly that announced tab.

const path = require("node:path");
const { webContents } = require("electron");
const { BROWSER_PARTITION, webUrl } = require("./security.cjs");

const EXPECT_TTL_MS = 10_000;
const MAX_PENDING = 8;

class WebviewGate {
  /** @param {{preload: string}} options */
  constructor({ preload }) {
    this.preload = preload;
    /** @type {Map<number, {pending: {tab: string, at: number, armed: boolean}[], guests: Map<number, string>}>} host web contents id -> notes */
    this.hosts = new Map();
  }

  /** @returns {{pending: {tab: string, at: number, armed: boolean}[], guests: Map<number, string>}} */
  of(hostId) {
    let entry = this.hosts.get(hostId);
    if (!entry) {
      entry = { pending: [], guests: new Map() };
      this.hosts.set(hostId, entry);
    }
    return entry;
  }

  /** The app's page says it is about to attach a <webview> for this tab. */
  expect(host, tab) {
    if (typeof tab !== "string" || tab.length === 0 || tab.length > 80) return false;
    const entry = this.of(host.id);
    const now = Date.now();
    entry.pending = entry.pending.filter((p) => now - p.at < EXPECT_TTL_MS);
    if (entry.pending.length >= MAX_PENDING || entry.pending.some((p) => p.tab === tab) || [...entry.guests.values()].includes(tab)) return false;
    entry.pending.push({ tab, at: now, armed: false });
    return true;
  }

  /** `will-attach-webview` of an app window's web contents. */
  willAttach(host, event, webPreferences, params) {
    const entry = this.of(host.id);
    const now = Date.now();
    entry.pending = entry.pending.filter((p) => now - p.at < EXPECT_TTL_MS || p.armed);
    const note = entry.pending.find((p) => !p.armed);
    const src = typeof params.src === "string" ? params.src : "";
    // A <webview> makes its guest only when it has an address, so ours opens on
    // about:blank, which has nothing in it; the page's own address is loaded
    // by the main process once the guest is attached and guarded.
    if (!note || (src !== "" && src !== "about:blank" && !webUrl(src))) {
      event.preventDefault();
      return;
    }
    note.armed = true;

    // The page's own say about its guest is dropped; ours is written over it.
    for (const key of ["allowpopups", "disablewebsecurity", "webpreferences", "preload", "nodeintegration", "nodeintegrationinsubframes", "plugins", "enableblinkfeatures", "disableblinkfeatures", "useragent", "httpreferrer"]) delete params[key];
    params.partition = BROWSER_PARTITION;
    params.src = src === "" ? "" : src === "about:blank" ? src : webUrl(src);
    // The guest's preferences are set, not wiped: Electron keeps its own
    // bookkeeping in the same object. Anything of ours below wins over
    // whatever the page's attributes asked for.
    for (const key of ["session", "preloadURL", "affinity", "additionalArguments", "enableBlinkFeatures", "disableBlinkFeatures", "defaultFontFamily"]) delete webPreferences[key];
    Object.assign(webPreferences, {
      partition: BROWSER_PARTITION,
      preload: this.preload,
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      nodeIntegrationInSubFrames: false,
      nodeIntegrationInWorker: false,
      webSecurity: true,
      allowRunningInsecureContent: false,
      plugins: false,
      webviewTag: false,
      safeDialogs: true,
      // No allowpopups: a page never gets a window of its own. Its window.open still reaches the
      // guest's window open handler (browser.cjs), which always denies and, after a real click, opens a panel tab.
      disablePopups: false,
      spellcheck: false,
      experimentalFeatures: false,
      javascript: true,
    });
  }

  /** `did-attach-webview`: pairs the new guest with the tab that was announced. */
  attached(host, guest) {
    const entry = this.of(host.id);
    const at = entry.pending.findIndex((p) => p.armed);
    if (at < 0) {
      // A guest nobody announced cannot exist (willAttach refused it); be sure.
      try {
        guest.close();
      } catch {
        // Gone already.
      }
      return;
    }
    const [note] = entry.pending.splice(at, 1);
    entry.guests.set(guest.id, note.tab);
    guest.once("destroyed", () => entry.guests.delete(guest.id));
  }

  /**
   * The app says guest `id` is the page of `tab`. Returns the guest's web
   * contents when that is true, and null otherwise.
   */
  claim(host, tab, id) {
    if (!Number.isInteger(id)) return null;
    const entry = this.hosts.get(host.id);
    if (!entry || entry.guests.get(id) !== tab) return null;
    const guest = webContents.fromId(id);
    if (!guest || guest.isDestroyed() || guest.getType() !== "webview" || guest.hostWebContents !== host) return null;
    return guest;
  }

  /** The window is gone: its notes go too. */
  forget(host) {
    this.hosts.delete(host.id);
  }
}

/** Wires a gate to an app window's web contents. */
function installWebviewGate(gate, host) {
  host.on("will-attach-webview", (event, webPreferences, params) => gate.willAttach(host, event, webPreferences, params));
  host.on("did-attach-webview", (_event, guest) => gate.attached(host, guest));
  host.once("destroyed", () => gate.forget(host));
}

let shared = null;
/** The one gate of the process; its guest preload is the autofill script. */
function webviewGate() {
  shared ??= new WebviewGate({ preload: path.join(__dirname, "autofill-preload.cjs") });
  return shared;
}

module.exports = { WebviewGate, installWebviewGate, webviewGate, EXPECT_TTL_MS, MAX_PENDING };
