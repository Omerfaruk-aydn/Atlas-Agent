"use strict";

// Chrome extensions in the panel browser. They live in the browser's own
// session (persist:atlas-browser) and nowhere else: the app's window, its
// session and the local gateway are out of their reach. chrome.* APIs come from
// electron-chrome-extensions, installs from the Web Store from
// electron-chrome-web-store (both MIT; pinned in package.json). An extension
// is only ever loaded after the person confirmed the permissions it asks for;
// a changed manifest asks again.

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow, dialog } = require("electron");

const POPUP_MAX = { width: 800, height: 600 };

/** What an extension asks for, from its manifest. */
function summarize(manifest) {
  const list = (value) => (Array.isArray(value) ? value.filter((x) => typeof x === "string").map((x) => x.slice(0, 200)).slice(0, 100) : []);
  const permissions = list(manifest.permissions);
  const hostPermissions = [...list(manifest.host_permissions), ...permissions.filter((p) => /^[a-z*]+:\/\//i.test(p) || p === "<all_urls>")];
  const scripts = Array.isArray(manifest.content_scripts) ? manifest.content_scripts.flatMap((c) => list(c.matches)) : [];
  return {
    name: String(manifest.name ?? "").slice(0, 200),
    version: String(manifest.version ?? "").slice(0, 50),
    description: String(manifest.description ?? "").slice(0, 500),
    permissions: permissions.filter((p) => !hostPermissions.includes(p)),
    hostPermissions: [...new Set([...hostPermissions, ...scripts])],
  };
}

/** A fingerprint of what was confirmed: change a permission and it no longer matches. */
function signature(manifest) {
  const s = summarize(manifest);
  return crypto.createHash("sha256").update(JSON.stringify([s.name, s.version, [...s.permissions].sort(), [...s.hostPermissions].sort()])).digest("hex");
}

function readManifest(dir) {
  const file = path.join(dir, "manifest.json");
  const manifest = JSON.parse(fs.readFileSync(file, "utf8").replace(/^﻿/, ""));
  if (typeof manifest !== "object" || manifest === null || !manifest.name || !manifest.version) throw new Error("This folder has no valid extension manifest.");
  if (Number(manifest.manifest_version) < 3) throw new Error("Only Manifest V3 extensions are supported.");
  return manifest;
}

function iconOf(dir, manifest) {
  const icons = { ...(manifest.icons ?? {}), ...(manifest.action?.default_icon && typeof manifest.action.default_icon === "object" ? manifest.action.default_icon : {}) };
  const sizes = Object.keys(icons).map(Number).filter((n) => n > 0).sort((a, b) => a - b);
  const wanted = sizes.find((n) => n >= 32) ?? sizes.at(-1);
  const rel = wanted ? icons[String(wanted)] : typeof manifest.action?.default_icon === "string" ? manifest.action.default_icon : "";
  if (typeof rel !== "string" || !rel) return "";
  const full = path.resolve(dir, rel);
  if (!full.startsWith(path.resolve(dir) + path.sep)) return "";
  try {
    const type = /\.png$/i.test(full) ? "image/png" : /\.(jpe?g)$/i.test(full) ? "image/jpeg" : /\.svg$/i.test(full) ? "image/svg+xml" : "";
    const data = type ? fs.readFileSync(full) : null;
    return data && data.length < 256 * 1024 ? `data:${type};base64,${data.toString("base64")}` : "";
  } catch {
    return "";
  }
}

class ExtensionManager {
  /**
   * @param {{ses: import("electron").Session, file: string, storePath: string, lang: () => string, openUrl: (url: string) => string, onChange: () => void}} opts
   */
  constructor(opts) {
    this.opts = opts;
    this.ses = opts.ses;
    this.file = opts.file;
    /** @type {{path: string, enabled: boolean, hidden: boolean, sig: string}[]} */
    this.items = [];
    this.lib = null;
    this.popup = null;
    this.ready = false;
  }

  loadState() {
    try {
      const parsed = JSON.parse(fs.readFileSync(this.file, "utf8"));
      this.items = Array.isArray(parsed.items) ? parsed.items.filter((i) => i && typeof i.path === "string") : [];
      this.hiddenIds = new Set(Array.isArray(parsed.hiddenIds) ? parsed.hiddenIds : []);
    } catch {
      this.items = [];
      this.hiddenIds = new Set();
    }
    this.hiddenIds ??= new Set();
  }

  saveState() {
    fs.mkdirSync(path.dirname(this.file), { recursive: true });
    fs.writeFileSync(this.file, JSON.stringify({ items: this.items, hiddenIds: [...this.hiddenIds] }));
  }

  /** Starts the extension APIs and loads what was enabled and confirmed before. */
  async init() {
    this.loadState();
    const { ElectronChromeExtensions } = require("electron-chrome-extensions");
    this.lib = new ElectronChromeExtensions({
      session: this.ses,
      createTab: async (details) => {
        const created = await this.opts.openUrl(details.url ?? "");
        if (!created) throw new Error("No tab could be opened.");
        return created;
      },
    });
    try {
      const { installChromeWebStore } = require("electron-chrome-web-store");
      await installChromeWebStore({
        session: this.ses,
        extensionsPath: this.opts.storePath,
        autoUpdate: false,
        beforeInstall: (details) => this.confirmStoreInstall(details),
      });
      this.storeReady = true;
    } catch (err) {
      this.storeReady = false;
      this.storeError = err instanceof Error ? err.message : String(err);
    }
    for (const item of this.items) {
      if (!item.enabled) continue;
      try {
        const manifest = readManifest(item.path);
        if (signature(manifest) !== item.sig) {
          item.enabled = false;
          continue;
        }
        await this.ses.extensions.loadExtension(item.path, { allowFileAccess: false });
      } catch {
        item.enabled = false;
      }
    }
    this.saveState();
    this.ready = true;
  }

  /** The permission prompt of a Web Store install: native, so no page can fake it. */
  async confirmStoreInstall(details) {
    const forced = !app.isPackaged && process.env.ATLAS_APP_TEST_STORE_CONFIRM;
    if (forced) return { action: forced === "yes" ? "allow" : "deny" };
    const tr = this.opts.lang() === "tr";
    const s = summarize(details.manifest ?? {});
    const asked = [...s.permissions, ...s.hostPermissions];
    const win = details.browserWindow ?? BrowserWindow.getFocusedWindow() ?? undefined;
    const answer = await dialog.showMessageBox(win, {
      type: "question",
      buttons: tr ? ["Ekle", "Vazgeç"] : ["Add", "Cancel"],
      defaultId: 1,
      cancelId: 1,
      message: tr ? `"${details.localizedName}" eklensin mi?` : `Add "${details.localizedName}"?`,
      detail: asked.length ? (tr ? "İstediği izinler:\n" : "It asks for:\n") + asked.slice(0, 30).map((p) => `  - ${p}`).join("\n") : tr ? "Özel izin istemiyor." : "It asks for no special permissions.",
    });
    return { action: answer.response === 0 ? "allow" : "deny" };
  }

  inspect(dir) {
    const manifest = readManifest(dir);
    return { path: dir, ...summarize(manifest), sig: signature(manifest), icon: iconOf(dir, manifest) };
  }

  /** Loads an unpacked extension the person confirmed. `sig` is what the confirmation was shown for. */
  async loadFolder(dir, sig) {
    const manifest = readManifest(dir);
    if (signature(manifest) !== sig) throw new Error("The extension changed since it was shown; open it again.");
    const loaded = await this.ses.extensions.loadExtension(dir, { allowFileAccess: false });
    const existing = this.items.find((i) => i.path === dir);
    if (existing) Object.assign(existing, { enabled: true, sig });
    else this.items.push({ path: dir, enabled: true, hidden: false, sig });
    this.saveState();
    this.opts.onChange();
    return loaded.id;
  }

  byId(id) {
    return this.ses.extensions.getAllExtensions().find((e) => e.id === id);
  }

  /** Everything installed: what is loaded now and what is switched off. */
  list() {
    const loaded = this.ses.extensions.getAllExtensions();
    const out = loaded.map((e) => {
      const item = this.items.find((i) => path.resolve(i.path) === path.resolve(e.path));
      const popup = e.manifest.action?.default_popup ?? e.manifest.browser_action?.default_popup ?? "";
      return {
        id: e.id,
        ...summarize(e.manifest),
        enabled: true,
        hidden: this.hiddenIds.has(e.id),
        source: item ? "folder" : "store",
        path: item ? e.path : "",
        icon: iconOf(e.path, e.manifest),
        action: e.manifest.action || e.manifest.browser_action ? { title: String(e.manifest.action?.default_title ?? e.manifest.name).slice(0, 200), popup: !!popup } : null,
      };
    });
    for (const item of this.items.filter((i) => !i.enabled)) {
      try {
        const manifest = readManifest(item.path);
        out.push({ id: `off:${item.path}`, ...summarize(manifest), enabled: false, hidden: false, source: "folder", path: item.path, icon: iconOf(item.path, manifest), action: null, sig: signature(manifest) });
      } catch {
        // The folder is gone; the entry stays until it is removed.
        out.push({ id: `off:${item.path}`, name: path.basename(item.path), version: "", description: "", permissions: [], hostPermissions: [], enabled: false, hidden: false, source: "folder", path: item.path, icon: "", action: null, missing: true });
      }
    }
    return out;
  }

  async disable(id) {
    const ext = this.byId(id);
    const item = ext && this.items.find((i) => path.resolve(i.path) === path.resolve(ext.path));
    if (!ext || !item) throw new Error("Only extensions loaded from a folder can be switched off here.");
    await this.ses.extensions.removeExtension(id);
    item.enabled = false;
    this.saveState();
    this.opts.onChange();
  }

  /** Switches one back on. It asks again, with the manifest as it is now. */
  async enable(dir, sig) {
    return this.loadFolder(dir, sig);
  }

  async remove(id) {
    if (id.startsWith("off:")) {
      this.items = this.items.filter((i) => `off:${i.path}` !== id);
    } else {
      const ext = this.byId(id);
      if (!ext) return false;
      const item = this.items.find((i) => path.resolve(i.path) === path.resolve(ext.path));
      await this.ses.extensions.removeExtension(id);
      if (item) this.items = this.items.filter((i) => i !== item);
      else {
        try {
          const { uninstallExtension } = require("electron-chrome-web-store");
          await uninstallExtension(id, { session: this.ses, extensionsPath: this.opts.storePath });
        } catch {
          // Removed from the session; the copy stays until the next start.
        }
      }
      this.hiddenIds.delete(id);
    }
    this.saveState();
    this.opts.onChange();
    return true;
  }

  setHidden(id, hidden) {
    if (hidden) this.hiddenIds.add(id);
    else this.hiddenIds.delete(id);
    this.saveState();
    this.opts.onChange();
  }

  addTab(contents, win) {
    try {
      this.lib?.addTab(contents, win);
    } catch {
      // A tab that is gone is not tracked.
    }
  }

  removeTab(contents) {
    try {
      this.lib?.removeTab(contents);
    } catch {
      // Not tracked.
    }
  }

  selectTab(contents) {
    try {
      this.lib?.selectTab(contents);
    } catch {
      // Not tracked.
    }
  }

  /**
   * The toolbar button of an extension that has no popup: fires its
   * chrome.action.onClicked in the page the person is looking at, as Chrome
   * does. False when the extension has no such button or no page is in front.
   */
  click(id, contents) {
    const ext = this.byId(id);
    if (!ext || !this.lib || !contents || contents.isDestroyed()) return false;
    const action = ext.manifest.action ?? ext.manifest.browser_action;
    if (!action || action.default_popup) return false;
    try {
      this.lib.api.browserAction.activateClick({ extensionId: id, tabId: contents.id, anchorRect: { x: 0, y: 0, width: 0, height: 0 }, alignment: "" });
      return true;
    } catch {
      return false;
    }
  }

  /** Opens an extension's popup in a small window of its own, next to the button it came from. */
  openPopup(id, anchor, parent) {
    const ext = this.byId(id);
    const popup = ext?.manifest.action?.default_popup ?? ext?.manifest.browser_action?.default_popup;
    if (!ext || !popup || typeof popup !== "string") return false;
    this.popup?.close();
    const bounds = parent.getContentBounds();
    const win = new BrowserWindow({
      parent,
      width: 380,
      height: 520,
      maxWidth: POPUP_MAX.width,
      maxHeight: POPUP_MAX.height,
      x: Math.round(bounds.x + (anchor?.x ?? bounds.width - 400)),
      y: Math.round(bounds.y + (anchor?.y ?? 48)),
      frame: false,
      resizable: false,
      skipTaskbar: true,
      show: false,
      webPreferences: { session: this.ses, sandbox: true, contextIsolation: true, nodeIntegration: false, webviewTag: false },
    });
    const origin = `chrome-extension://${ext.id}/`;
    const stay = (event, url) => {
      if (!url.startsWith(origin)) event.preventDefault();
    };
    win.webContents.on("will-navigate", stay);
    win.webContents.setWindowOpenHandler(({ url }) => {
      if (/^https?:\/\//i.test(url)) this.opts.openUrl(url);
      return { action: "deny" };
    });
    // It fades in, and out: a window may not appear or vanish in a single frame.
    const fade = (from, to, ms, done) => {
      const steps = 8;
      let n = 0;
      const timer = setInterval(() => {
        n++;
        if (win.isDestroyed()) return clearInterval(timer);
        win.setOpacity(from + ((to - from) * n) / steps);
        if (n >= steps) {
          clearInterval(timer);
          done?.();
        }
      }, ms / steps);
    };
    win.setOpacity(0);
    win.once("ready-to-show", () => {
      win.show();
      fade(0, 1, 140);
    });
    let leaving = false;
    win.on("close", (event) => {
      if (leaving || win.isDestroyed()) return;
      event.preventDefault();
      leaving = true;
      fade(1, 0, 100, () => !win.isDestroyed() && win.destroy());
    });
    win.on("blur", () => win.close());
    win.on("closed", () => {
      if (this.popup === win) this.popup = null;
    });
    this.popup = win;
    win.loadURL(origin + popup.replace(/^\//, "")).catch(() => win.close());
    return true;
  }

  dispose() {
    this.popup?.close();
  }
}

module.exports = { ExtensionManager, summarize, signature, readManifest, iconOf };
