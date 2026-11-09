"use strict";

// The panel browser's channels beyond the basics in ipc.cjs: find, zoom,
// devices, downloads, history, notes, extensions, passwords and imports.
// Every channel is checked: the sender must be the gateway's own page (the
// `handle` wrapper), arguments are validated here, and secrets never travel to
// the renderer except the one entry the person asks to reveal.

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow, clipboard, dialog, ipcMain, safeStorage, session } = require("electron");
const prefs = require("./prefs.cjs");
const { BROWSER_PARTITION, webUrl } = require("./security.cjs");
const { HistoryStore, NotesStore, ZoomStore, SettingsStore, DEVICES } = require("./browser-data.cjs");
const { AddressBook, AddressError } = require("./addresses.cjs");
const { Vault, VaultError } = require("./vault.cjs");
const { ExtensionManager } = require("./extensions.cjs");
const { originOf } = require("./origin.cjs");
const { parsePasswordCsv } = require("./csv.cjs");
const cookies = require("./cookie-import.cjs");

const OFFER_TTL_MS = 5 * 60_000;
/** How long a copied password stays on the clipboard; a test build may shorten it. */
const clearAfterMs = () => (!app.isPackaged && Number(process.env.ATLAS_APP_CLIPBOARD_CLEAR_MS) > 0 ? Number(process.env.ATLAS_APP_CLIPBOARD_CLEAR_MS) : 30_000);
const str = (value, max = 4096) => (typeof value === "string" && value.length <= max ? value : "");

/**
 * Login offers waiting for an answer. The password is held here, in memory,
 * only until the person answers, the offer is five minutes old, or its tab
 * closes: then it is overwritten and forgotten.
 */
class OfferBook {
  constructor(ttl = OFFER_TTL_MS) {
    this.ttl = ttl;
    this.map = new Map();
  }

  add(id, offer) {
    const timer = setTimeout(() => this.drop(id), this.ttl);
    timer.unref?.();
    this.map.set(id, { ...offer, timer });
  }

  /** The offer, taken out of the book (a second answer finds nothing). */
  take(id) {
    const offer = this.map.get(id);
    if (!offer) return null;
    clearTimeout(offer.timer);
    this.map.delete(id);
    const taken = { ...offer };
    offer.password = "";
    return taken;
  }

  drop(id) {
    const offer = this.map.get(id);
    if (!offer) return;
    clearTimeout(offer.timer);
    offer.password = "";
    this.map.delete(id);
  }

  dropTab(tab) {
    for (const [id, offer] of this.map) if (offer.tab === tab) this.drop(id);
  }

  get size() {
    return this.map.size;
  }
}

/** Where the browser's data lives; tests point it elsewhere through the app's userData. */
function createServices(hooks) {
  const dir = app.getPath("userData");
  const history = new HistoryStore(path.join(dir, "browser-history.json"));
  const notes = new NotesStore(path.join(dir, "browser-notes.json"));
  const zoom = new ZoomStore(path.join(dir, "browser-zoom.json"));
  const vault = new Vault(path.join(dir, "vault.json"), safeStorage);
  const addresses = new AddressBook(path.join(dir, "addresses.json"), safeStorage);
  const settings = new SettingsStore(path.join(dir, "browser-settings.json"));
  const ses = session.fromPartition(BROWSER_PARTITION);
  const extensions = new ExtensionManager({
    ses,
    file: path.join(dir, "browser-extensions.json"),
    storePath: path.join(dir, "Extensions"),
    lang: hooks.lang,
    openUrl: hooks.openUrl,
    onChange: () => hooks.broadcast("atlas:ext-changed", null),
  });
  const offers = new OfferBook(Number(process.env.ATLAS_APP_OFFER_TTL_MS) > 0 && !app.isPackaged ? Number(process.env.ATLAS_APP_OFFER_TTL_MS) : OFFER_TTL_MS);
  return { history, notes, zoom, vault, addresses, settings, broadcast: hooks.broadcast, extensions, ses, offers, tabClosed: (tab) => offers.dropTab(tab) };
}

function flushAll(services) {
  services.history.flush();
  services.notes.flush();
  services.zoom.flush();
  services.settings.flush();
}

/**
 * @param {{handle: Function, hostsOf: (event: any) => {browser: any}, peek: (win: any) => {browser: any} | undefined, services: ReturnType<typeof createServices>, lang: () => string}} ctx
 */
function register(ctx) {
  const { handle, hostsOf, services } = ctx;
  const { history, notes, vault, addresses, settings, extensions } = services;
  const browser = (event) => hostsOf(event).browser;
  const id = (value) => str(value, 80);
  /** Test builds only: a folder or file the dialog would have returned. */
  const testPick = (name) => (!app.isPackaged && process.env[name]) || "";

  handle("atlas:browser-chords", (e, list) => browser(e).setChords(list));
  handle("atlas:browser-select", (e, tab) => browser(e).select(id(tab)));
  handle("atlas:browser-find", (e, tab, text, options) => browser(e).find(id(tab), str(text, 500), typeof options === "object" && options ? { forward: options.forward !== false, findNext: options.findNext === true } : {}));
  handle("atlas:browser-find-stop", (e, tab) => browser(e).stopFind(id(tab)));
  handle("atlas:browser-print", (e, tab) => browser(e).print(id(tab)));
  handle("atlas:browser-zoom", (e, tab, action) => browser(e).zoom(id(tab), ["in", "out", "reset"].includes(action) ? action : ""));
  handle("atlas:browser-devices", () => DEVICES.map(({ userAgent: _ua, ...rest }) => rest));
  handle("atlas:browser-device", (e, tab, device) => {
    if (device === null) return browser(e).setDevice(id(tab), null);
    if (typeof device === "string") {
      const preset = DEVICES.find((d) => d.id === device);
      return preset ? browser(e).setDevice(id(tab), preset) : false;
    }
    if (typeof device === "object" && device && Number.isFinite(device.width) && Number.isFinite(device.height)) {
      return browser(e).setDevice(id(tab), { id: "custom", name: "Custom", width: device.width, height: device.height, ratio: Number(device.ratio) || 1, mobile: device.width < 800, userAgent: device.width < 800 ? DEVICES[2].userAgent : "" });
    }
    return false;
  });
  handle("atlas:browser-screenshot-data", (e, tab) => browser(e).screenshotData(id(tab)));
  handle("atlas:browser-screenshot-save", (e, tab) => browser(e).screenshotSave(id(tab)));
  handle("atlas:browser-nav-history", (e, tab) => browser(e).history(id(tab)));
  handle("atlas:browser-goto", (e, tab, index) => browser(e).goToIndex(id(tab), index));
  handle("atlas:browser-selection", (e, tab) => browser(e).selection(id(tab)));
  handle("atlas:browser-clear", (e, options) => {
    const o = typeof options === "object" && options ? options : {};
    return browser(e).clearData({ history: o.history === true, cookies: o.cookies === true, cache: o.cache === true, siteData: o.siteData === true, passwords: o.passwords === true, since: Number(o.since) || 0 });
  });

  handle("atlas:downloads-list", (e) => browser(e).listDownloads());
  handle("atlas:download-action", (e, downloadId, action) => (Number.isInteger(downloadId) && ["cancel", "pause", "resume", "open", "show", "remove"].includes(action) ? browser(e).downloadAction(downloadId, action) : false));

  handle("atlas:history-search", (_e, query, limit) => history.search(str(query, 200), Number(limit) || 500));
  handle("atlas:history-remove", (_e, entryId) => history.remove(id(entryId)));
  handle("atlas:history-clear", (_e, since) => history.clear(Number(since) || 0));

  handle("atlas:notes-list", (_e, url) => notes.list(str(url, 2048)));
  handle("atlas:notes-all", () => notes.all());
  handle("atlas:notes-add", (_e, url, text, quote) => notes.add(str(url, 2048), str(text, 8000), str(quote, 4000)));
  handle("atlas:notes-update", (_e, url, noteId, text) => notes.update(str(url, 2048), id(noteId), str(text, 8000)));
  handle("atlas:notes-remove", (_e, url, noteId) => notes.remove(str(url, 2048), id(noteId)));

  // Extensions: loaded only after the person confirmed what they ask for.
  const picked = new Set();
  handle("atlas:ext-list", () => ({ items: extensions.list(), store: !!extensions.storeReady, storeError: extensions.storeError ?? "" }));
  handle("atlas:ext-pick", async (e) => {
    const win = BrowserWindow.fromWebContents(e.sender);
    let dir = testPick("ATLAS_APP_TEST_EXTENSION_DIR");
    if (!dir) {
      const result = await dialog.showOpenDialog(win, { properties: ["openDirectory"] });
      if (result.canceled || !result.filePaths[0]) return null;
      dir = result.filePaths[0];
    }
    try {
      const info = extensions.inspect(dir);
      picked.add(path.resolve(dir));
      return { ok: true, ...info };
    } catch (err) {
      return { ok: false, error: err instanceof Error ? err.message : String(err) };
    }
  });
  const known = (dir) => typeof dir === "string" && path.isAbsolute(dir) && (picked.has(path.resolve(dir)) || extensions.items.some((i) => path.resolve(i.path) === path.resolve(dir)));
  handle("atlas:ext-load", async (_e, dir, sig) => {
    if (!known(dir) || typeof sig !== "string") return { ok: false, error: "Choose the folder again." };
    try {
      return { ok: true, id: await extensions.loadFolder(dir, sig) };
    } catch (err) {
      return { ok: false, error: err instanceof Error ? err.message : String(err) };
    }
  });
  handle("atlas:ext-inspect", (_e, dir) => {
    if (!known(dir)) return { ok: false, error: "Unknown folder." };
    try {
      return { ok: true, ...extensions.inspect(dir) };
    } catch (err) {
      return { ok: false, error: err instanceof Error ? err.message : String(err) };
    }
  });
  handle("atlas:ext-disable", async (_e, extId) => {
    try {
      await extensions.disable(id(extId));
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err instanceof Error ? err.message : String(err) };
    }
  });
  handle("atlas:ext-remove", (_e, extId) => extensions.remove(id(extId)));
  handle("atlas:ext-hide", (_e, extId, hidden) => extensions.setHidden(id(extId), hidden === true));
  handle("atlas:ext-click", (e, extId, tab) => extensions.click(id(extId), browser(e).tabs.get(id(tab))?.wc));
  handle("atlas:ext-popup", (e, extId, anchor) => {
    const win = BrowserWindow.fromWebContents(e.sender);
    const a = typeof anchor === "object" && anchor && Number.isFinite(anchor.x) && Number.isFinite(anchor.y) ? { x: anchor.x, y: anchor.y } : null;
    return win ? extensions.openPopup(id(extId), a, win) : false;
  });

  // Passwords.
  const offers = services.offers;
  handle("atlas:vault-status", () => ({ available: vault.available(), count: vault.list().length }));
  handle("atlas:vault-list", (_e, query) => vault.list(str(query, 200)));
  handle("atlas:vault-for-tab", (e, tab) => {
    const host = browser(e);
    const state = host.state(id(tab));
    return state?.url ? vault.forOrigin(state.url) : [];
  });
  handle("atlas:vault-fill", (e, tab, entryId) => {
    const host = browser(e);
    const wc = host.tabs.get(id(tab))?.wc;
    if (!wc) return { ok: false };
    // Only after this call, which is the person pressing the key button, and
    // only for the page's own exact origin.
    let login = null;
    try {
      login = vault.fill(id(entryId), wc.getURL());
    } catch (err) {
      return { ok: false, error: err instanceof VaultError ? err.message : "Could not read the login." };
    }
    if (!login) return { ok: false, error: "This login belongs to another site." };
    wc.send("atlas:vault-fill", login);
    return { ok: true };
  });
  // Showing or copying a saved password asks the person in a native window
  // the page cannot draw or click: it names the site and the login. At most
  // five asks a minute are taken, so a page-driven loop cannot wear a person
  // down. The test hook answers for a build that is not packaged.
  const asks = [];
  async function confirmSecret(event, entryId, verb) {
    const now = Date.now();
    while (asks.length && now - asks[0] > 60_000) asks.shift();
    if (asks.length >= 5) return { ok: false, error: "Too many requests. Try again in a minute." };
    asks.push(now);
    const entry = vault.list("").find((x) => x.id === entryId);
    if (!entry) return { ok: false, error: "That login is gone." };
    const forced = !app.isPackaged && process.env.ATLAS_APP_TEST_CONFIRM;
    if (forced) return forced === "yes" ? { ok: true, entry } : { ok: false, error: "Cancelled." };
    const win = BrowserWindow.fromWebContents(event.sender);
    const options = {
      type: "question",
      title: "Atlas",
      message: `${verb} the password for ${new URL(entry.origin).host}?`,
      detail: `Login: ${entry.username || "(no username)"}
Only continue if you asked for this.`,
      buttons: [verb, "Cancel"],
      defaultId: 1,
      cancelId: 1,
      noLink: true,
    };
    const answer = win && !win.isDestroyed() ? await dialog.showMessageBox(win, options) : await dialog.showMessageBox(options);
    return answer.response === 0 ? { ok: true, entry } : { ok: false, error: "Cancelled." };
  }
  handle("atlas:vault-reveal", async (e, entryId) => {
    const asked = await confirmSecret(e, id(entryId), "Show");
    if (!asked.ok) return asked;
    try {
      return { ok: true, password: vault.reveal(id(entryId)) };
    } catch (err) {
      return { ok: false, error: err instanceof VaultError ? err.message : "Could not read the login." };
    }
  });
  // The copy happens here: the password never reaches the page. The clipboard
  // is cleared after 30 seconds if it still holds it.
  let clearTimer = null;
  handle("atlas:vault-copy", async (e, entryId) => {
    const asked = await confirmSecret(e, id(entryId), "Copy");
    if (!asked.ok) return asked;
    try {
      const secret = vault.reveal(id(entryId));
      await clipboard.writeText(secret);
      clearTimeout(clearTimer);
      clearTimer = setTimeout(async () => {
        try {
          // The clipboard's methods answer with promises in this Electron.
          if ((await clipboard.readText()) === secret) await clipboard.clear();
        } catch {
          // Nothing to clear.
        }
      }, clearAfterMs());
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err instanceof VaultError ? err.message : "Could not read the login." };
    }
  });
  handle("atlas:vault-add", (_e, origin, username, password) => {
    try {
      return { ok: true, ...vault.add(str(origin, 2048), str(username, 2048), str(password, 2048)) };
    } catch (err) {
      return { ok: false, error: err instanceof VaultError ? err.message : "That login cannot be saved.", code: err instanceof VaultError ? err.code : "invalid" };
    }
  });
  handle("atlas:vault-update", (_e, entryId, change) => {
    try {
      vault.update(id(entryId), { username: typeof change?.username === "string" ? str(change.username, 2048) : undefined, password: typeof change?.password === "string" ? str(change.password, 2048) : undefined });
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err instanceof VaultError ? err.message : "That login cannot be saved." };
    }
  });
  handle("atlas:vault-delete", (_e, entryId) => vault.remove(id(entryId)));
  handle("atlas:vault-offer-answer", (e, offerId, answer) => {
    const offer = offers.take(id(offerId));
    if (!offer) return { ok: false };
    if (answer === "never") {
      vault.setNeverSave(offer.origin, true);
      return { ok: true };
    }
    if (answer !== "save") return { ok: true };
    try {
      return { ok: true, ...vault.add(offer.origin, offer.username, offer.password) };
    } catch (err) {
      return { ok: false, error: err instanceof VaultError ? err.message : "That login cannot be saved." };
    }
  });

  // From the page's preload: not trusted like the app's own page, so every
  // call is tied to one of our views and to its top frame.
  const fromView = (event) => {
    if (event.senderFrame !== event.sender.mainFrame) return null;
    for (const w of BrowserWindow.getAllWindows()) {
      const host = ctx.peek(w)?.browser;
      const tab = host?.idOf(event.sender);
      if (tab) return { host, tab, win: w };
    }
    return null;
  };
  ipcMain.on("atlas:vault-form", (event, payload) => {
    const view = fromView(event);
    if (view) view.host.setLogin(view.tab, !!payload?.login);
  });
  ipcMain.on("atlas:addr-form", (event, payload) => {
    const view = fromView(event);
    if (view) view.host.setAddressForm(view.tab, !!payload?.address && services.settings.get().addressAutofill);
  });
  ipcMain.on("atlas:vault-submit", (event, payload) => {
    const view = fromView(event);
    if (!view || typeof payload?.password !== "string" || !payload.password || payload.password.length > 2048) return;
    const origin = originOf(event.sender.getURL());
    const username = typeof payload.username === "string" ? payload.username.slice(0, 2048) : "";
    if (!origin || vault.neverSave(origin) || !settings.get().offerSave) return;
    if (vault.available() && vault.isSaved(origin, username, payload.password)) return;
    const offerId = crypto.randomUUID();
    offers.add(offerId, { origin, username, password: payload.password, tab: view.tab });
    const update = vault.list().some((e) => e.origin === origin && e.username === username);
    // The password stays here; the app is told who and where, not what.
    view.win.webContents.send("atlas:vault-offer", { id: offerId, tab: view.tab, host: new URL(origin).host, username, update, unavailable: !vault.available() });
  });

  // Settings of the browser, and the sites that were told "never save".
  handle("atlas:browser-settings", () => settings.get());
  handle("atlas:browser-settings-set", (_e, key, value) => {
    const ok = settings.set(str(key, 40), value);
    if (ok) services.broadcast?.("atlas:browser-settings-changed", settings.get());
    return ok ? settings.get() : null;
  });
  handle("atlas:vault-never-list", () => vault.neverList());
  handle("atlas:vault-never-remove", (_e, origin) => {
    vault.setNeverSave(str(origin, 2048), false);
    return vault.neverList();
  });

  // Addresses: the same model as the vault. The manager (the app's own page)
  // reads and edits them; a page only ever receives the one the person
  // chose, in its top frame, after pressing the fill button.
  const addrError = (err) => ({ ok: false, error: err instanceof AddressError ? err.message : "That address cannot be saved.", code: err instanceof AddressError ? err.code : "invalid" });
  handle("atlas:addr-status", () => ({ available: addresses.available(), count: addresses.available() ? addresses.list().length : 0 }));
  handle("atlas:addr-list", () => addresses.list());
  handle("atlas:addr-labels", () => addresses.labels());
  handle("atlas:addr-save", (_e, entryId, record) => {
    try {
      return { ok: true, ...addresses.save_(entryId ? id(entryId) : "", record && typeof record === "object" ? record : {}) };
    } catch (err) {
      return addrError(err);
    }
  });
  handle("atlas:addr-delete", (_e, entryId) => addresses.remove(id(entryId)));
  handle("atlas:addr-fill", (e, tab, entryId) => {
    if (!settings.get().addressAutofill) return { ok: false, error: "Address autofill is off in the browser's settings." };
    const wc = browser(e).tabs.get(id(tab))?.wc;
    if (!wc) return { ok: false };
    // Only after this call, which is the person pressing the fill button.
    try {
      wc.send("atlas:addr-fill", addresses.get(id(entryId)));
      return { ok: true };
    } catch (err) {
      return addrError(err);
    }
  });

  // Imports. Both start from a button the person pressed.
  // Test builds look for other browsers under a fixture folder (or nowhere), never in a real profile.
  const profileEnv = () => {
    if (app.isPackaged) return process.env;
    if (process.env.ATLAS_APP_TEST_PROFILE_ROOT) return { APPDATA: path.join(process.env.ATLAS_APP_TEST_PROFILE_ROOT, "Roaming"), LOCALAPPDATA: path.join(process.env.ATLAS_APP_TEST_PROFILE_ROOT, "Local") };
    return process.env.ATLAS_APP_TEST_NO_PROFILES ? {} : process.env;
  };
  const testKey = () => (!app.isPackaged && process.env.ATLAS_APP_TEST_DPAPI_KEY ? Buffer.from(process.env.ATLAS_APP_TEST_DPAPI_KEY, "hex") : null);
  handle("atlas:import-profiles", () => cookies.listProfiles(profileEnv()).map(({ id: pid, browser: b, name }) => ({ id: pid, browser: b, name })));
  handle("atlas:import-cookies", async (_e, profileId) => {
    const profile = cookies.listProfiles(profileEnv()).find((p) => p.id === profileId);
    if (!profile) return { ok: false, error: "That profile is gone." };
    try {
      const read = profile.browser === "firefox" ? cookies.readFirefoxCookies(profile.cookies) : await cookies.readChromiumCookies(profile, testKey() ? { unprotect: async () => testKey() } : undefined);
      const put = await cookies.importCookies(services.ses, read.cookies);
      return { ok: true, imported: put.imported, refused: put.refused, appBound: read.appBound, failed: read.failed };
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      return { ok: false, error: /EBUSY|EPERM|locked|being used/i.test(message) ? "locked" : message.slice(0, 200) };
    }
  });
  handle("atlas:import-passwords", async (e) => {
    const win = BrowserWindow.fromWebContents(e.sender);
    let file = testPick("ATLAS_APP_TEST_CSV");
    if (!file) {
      const result = await dialog.showOpenDialog(win, { properties: ["openFile"], filters: [{ name: "CSV", extensions: ["csv"] }] });
      if (result.canceled || !result.filePaths[0]) return null;
      file = result.filePaths[0];
    }
    if (!vault.available()) return { ok: false, unavailable: true };
    try {
      const stat = fs.statSync(file);
      if (stat.size > 20 << 20) return { ok: false, error: "too-big" };
      const parsed = parsePasswordCsv(fs.readFileSync(file, "utf8"));
      if (!parsed.format) return { ok: false, error: "format" };
      const done = vault.addMany(parsed.entries);
      return { ok: true, format: parsed.format, ...done, skipped: parsed.skipped, file: path.basename(file) };
    } catch (err) {
      if (err instanceof VaultError) return { ok: false, unavailable: err.code === "unavailable" };
      return { ok: false, error: "read" };
    }
  });
}

module.exports = { createServices, flushAll, register, OfferBook };
