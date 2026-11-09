"use strict";

// The renderer's whole view of the desktop. Network work goes straight to the
// gateway over HTTP; only what a page cannot do lives here. Every call is
// checked to come from the gateway's own page.

const { app, BrowserWindow, dialog, ipcMain, nativeImage, nativeTheme, net, Notification, shell } = require("electron");
const { execFile } = require("node:child_process");
const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const prefs = require("./prefs.cjs");
const { webviewGate } = require("./webview-gate.cjs");
const { openExternal, publicHost, webUrl } = require("./security.cjs");
const { BrowserHost } = require("./browser.cjs");
const { TerminalHost } = require("./terminal.cjs");
const browserIpc = require("./browser-ipc.cjs");
const { exportDesign } = require("./design-export.cjs");

let servicesRef = null;
const GIT_TIMEOUT_MS = 3000;
const FAVICON_TIMEOUT_MS = 3000;
const MAX_FAVICON_BYTES = 128 * 1024;
const MAX_BADGE_URL = 64 * 1024;

const isDir = (p) => typeof p === "string" && p.length < 4096 && path.isAbsolute(p) && !!fs.statSync(p, { throwIfNoEntry: false })?.isDirectory();

function git(cwd, args) {
  return new Promise((resolve) => {
    execFile("git", ["-C", cwd, "--no-optional-locks", ...args], { timeout: GIT_TIMEOUT_MS, windowsHide: true, maxBuffer: 4 << 20 }, (err, stdout) => resolve(err ? null : stdout));
  });
}

/** Branch and dirtiness, from read-only commands. Null outside a repository. */
async function gitStatus(project) {
  if (!isDir(project)) return null;
  const out = await git(project, ["status", "--porcelain=v1", "--branch"]);
  if (out === null) return null;
  const [head, ...files] = out.split("\n").filter(Boolean);
  const upstream = ((await git(project, ["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"])) ?? "").trim();
  const branch = /^## (?:No commits yet on )?([^.\s]+(?:\.[^.\s]+)*)/.exec(head ?? "")?.[1] ?? "HEAD";
  return {
    branch,
    dirty: files.length,
    ahead: Number(/ahead (\d+)/.exec(head ?? "")?.[1] ?? 0),
    behind: Number(/behind (\d+)/.exec(head ?? "")?.[1] ?? 0),
    upstream,
  };
}

const favicons = new Map();

/** Pages may not reach the web, so favicons come through here, size-limited. */
async function favicon(host) {
  if (typeof host !== "string" || !/^[a-z0-9.-]{1,253}$/i.test(host)) return "";
  if (favicons.has(host)) return favicons.get(host);
  let icon = "";
  try {
    // Never fetch from inside the user's network on a page's say-so.
    if (!(await publicHost(host))) {
      favicons.set(host, "");
      return "";
    }
    const res = await net.fetch(`https://${host}/favicon.ico`, { signal: AbortSignal.timeout(FAVICON_TIMEOUT_MS), credentials: "omit", redirect: "manual" });
    const type = (res.headers.get("content-type") ?? "").split(";")[0].trim();
    if (res.ok && /^image\/(x-icon|vnd\.microsoft\.icon|png|jpeg|gif|webp)$/.test(type)) {
      const data = Buffer.from(await res.arrayBuffer());
      if (data.length > 0 && data.length <= MAX_FAVICON_BYTES) icon = `data:${type};base64,${data.toString("base64")}`;
    }
  } catch {
    // The letter stands in.
  }
  favicons.set(host, icon);
  return icon;
}

/** @param {{origin: () => string, version: () => string, moveToProject: (win: BrowserWindow, p: string) => BrowserWindow}} ctx */
const MAX_SAVE_CHARS = 8 * 1024 * 1024;

function register(ctx) {
  const trusted = (event) => {
    try {
      return new URL(event.senderFrame.url).origin === ctx.origin();
    } catch {
      return false;
    }
  };
  const handle = (channel, fn) => {
    ipcMain.handle(channel, (event, ...args) => {
      if (!trusted(event)) throw new Error("refused");
      return fn(event, ...args);
    });
  };
  ipcMain.on("atlas:renderer-ready", (event) => {
    if (!trusted(event)) return;
    const win = BrowserWindow.fromWebContents(event.sender);
    if (win) ctx.rendererReady?.(win);
  });

  // Read before first paint, so the page never flashes the wrong theme.
  ipcMain.on("atlas:boot", (event) => {
    if (!trusted(event)) {
      event.returnValue = null;
      return;
    }
    const p = prefs.load();
    event.returnValue = { theme: p.theme, dark: nativeTheme.shouldUseDarkColors, motion: p.motion, ui: p.ui };
  });

  handle("atlas:app-info", () => ({
    version: app.getVersion(),
    core: ctx.version(),
    platform: process.platform,
    packaged: app.isPackaged,
    electron: process.versions.electron,
  }));
  handle("atlas:theme-set", (_e, theme) => {
    if (prefs.setTheme(theme)) nativeTheme.themeSource = theme;
  });
  handle("atlas:motion-set", (_e, motion) => {
    prefs.setMotion(motion);
  });
  handle("atlas:ui-set", (_e, key, value) => prefs.setUI(key, value));
  handle("atlas:git-status", (_e, project) => gitStatus(project));
  handle("atlas:favicon", (_e, host) => favicon(host));
  handle("atlas:notify", (event, note) => {
    if (!Notification.isSupported() || typeof note !== "object" || note === null) return;
    const win = BrowserWindow.fromWebContents(event.sender);
    const shown = new Notification({ title: String(note.title).slice(0, 120), body: String(note.body).slice(0, 240), silent: false });
    shown.on("click", () => {
      if (!win || win.isDestroyed()) return;
      if (win.isMinimized()) win.restore();
      win.show();
      win.focus();
      win.webContents.send("atlas:open-session", String(note.session));
    });
    shown.show();
  });
  handle("atlas:badge", (event, count, image) => {
    const win = BrowserWindow.fromWebContents(event.sender);
    if (!win) return;
    const valid = count > 0 && typeof image === "string" && image.length < MAX_BADGE_URL && image.startsWith("data:image/png;base64,");
    win.setOverlayIcon(valid ? nativeImage.createFromDataURL(image) : null, valid ? `${count}` : "");
    if (valid && !win.isFocused()) win.flashFrame(true);
  });
  handle("atlas:pick-folder", async (event) => {
    const win = BrowserWindow.fromWebContents(event.sender);
    const result = await dialog.showOpenDialog(win, { properties: ["openDirectory", "createDirectory"] });
    return result.canceled ? "" : (result.filePaths[0] ?? "");
  });
  handle("atlas:open-project", (event, project) => {
    if (!isDir(project)) return false;
    const sender = BrowserWindow.fromWebContents(event.sender);
    if (!sender) return false;
    const shown = ctx.moveToProject(sender, project);
    // A window with no project hands over to the one already showing it.
    if (shown !== sender && !new URL(event.senderFrame.url).searchParams.get("project")) sender.close();
    return true;
  });
  // One browser and one set of terminals per window, made on first use and
  // closed with the window.
  const hosts = new WeakMap();
  const lang = () => (prefs.load().ui.lang === "tr" || (prefs.load().ui.lang !== "en" && app.getLocale().startsWith("tr")) ? "tr" : "en");
  const broadcast = (channel, payload) => {
    for (const w of BrowserWindow.getAllWindows()) if (!w.isDestroyed()) w.webContents.send(channel, payload);
  };
  /** A tab opened for the extension APIs: made here, shown by the window that was in front. */
  const openUrl = (url) => {
    const win = BrowserWindow.getFocusedWindow() ?? BrowserWindow.getAllWindows()[0];
    const host = win && hosts.get(win)?.browser;
    if (!win || !host || !webUrl(url)) return null;
    // The renderer makes the tab and its <webview>; the guest comes back through the gate.
    const tab = `x${crypto.randomUUID()}`;
    const guest = host.expectGuest(tab);
    win.webContents.send("atlas:browser-popup", { from: "", url, id: tab });
    return guest.then((wc) => [wc, win]);
  };
  const services = browserIpc.createServices({ lang, openUrl, broadcast });
  servicesRef = services;
  const hostForWindow = (win) => {
    let host = hosts.get(win);
    if (!host) {
      host = { browser: new BrowserHost(win, { lang }, services), terminal: new TerminalHost(win) };
      hosts.set(win, host);
      wireHost(win, host);
    }
    return host;
  };
  const wireHost = (win, host) => {
    {
      hosts.set(win, host);
      const reset = () => {
        host.browser.destroy();
        host.terminal.destroy();
      };
      win.once("closed", reset);
      // A new page (another project, a reload) must not keep guests of the old.
      win.webContents.on("did-start-navigation", (details) => {
        if (details.isMainFrame && !details.isSameDocument) {
          reset();
          webviewGate().forget(win.webContents);
        }
      });
    }
  };
  const hostsOf = (event) => {
    const win = BrowserWindow.fromWebContents(event.sender);
    if (!win) throw new Error("no window");
    return hostForWindow(win);
  };
  browserIpc.register({ handle, hostsOf, peek: (win) => hosts.get(win), services, lang });
  services.extensions.init().catch(() => {});
  const id = (value) => (typeof value === "string" ? value : "");
  handle("atlas:panel-set", (_e, key, value) => prefs.setPanel(key, value));
  handle("atlas:recent-set", (_e, list) => prefs.mergeRecent(list));
  // The page of a tab is a <webview> of the app's own page. The app announces
  // it, then reports which guest it became; webview-gate.cjs decides both.
  const appWindow = (e) => e.sender.getType() === "window" && BrowserWindow.fromWebContents(e.sender) !== null;
  handle("atlas:browser-expect", (e, tab) => appWindow(e) && webviewGate().expect(e.sender, id(tab)));
  handle("atlas:browser-attach", (e, tab, contentsId, url) => {
    if (!appWindow(e)) return null;
    const guest = webviewGate().claim(e.sender, id(tab), contentsId);
    const host = hostsOf(e).browser;
    const state = guest ? host.adopt(id(tab), guest) : null;
    // The page's first address is loaded here, after every guard is in place.
    if (state && typeof url === "string" && url) host.navigate(id(tab), url);
    return state ? host.state(id(tab)) : null;
  });
  handle("atlas:browser-navigate", (e, tab, url) => hostsOf(e).browser.navigate(id(tab), String(url ?? "")));
  handle("atlas:browser-back", (e, tab) => hostsOf(e).browser.back(id(tab)));
  handle("atlas:browser-forward", (e, tab) => hostsOf(e).browser.forward(id(tab)));
  handle("atlas:browser-reload", (e, tab) => hostsOf(e).browser.reload(id(tab)));
  handle("atlas:browser-stop", (e, tab) => hostsOf(e).browser.stop(id(tab)));
  handle("atlas:browser-close", (e, tab) => hostsOf(e).browser.close(id(tab)));
  handle("atlas:browser-url", (e, tab) => hostsOf(e).browser.state(id(tab)));
  handle("atlas:terminal-create", (e, opts) => {
    if (typeof opts !== "object" || opts === null || !isDir(opts.cwd)) return { ok: false, error: "bad folder" };
    return hostsOf(e).terminal.create(opts);
  });
  handle("atlas:terminal-write", (e, tab, data) => hostsOf(e).terminal.write(id(tab), data));
  handle("atlas:terminal-resize", (e, tab, cols, rows) => hostsOf(e).terminal.resize(id(tab), cols, rows));
  handle("atlas:terminal-kill", (e, tab) => hostsOf(e).terminal.kill(id(tab)));
  handle("atlas:open-external", (_e, url) => openExternal(String(url)));
  // Saves a piece of text the person asked to keep (a code block). The person
  // picks the place in a save dialog; nothing is written without that.
  handle("atlas:save-text", async (event, name, text) => {
    if (typeof text !== "string" || text.length > MAX_SAVE_CHARS) return "";
    const win = BrowserWindow.fromWebContents(event.sender);
    const file = path.basename(String(name || "snippet.txt")).replace(/[<>:"/\\|?*\u0000-\u001f]/g, "_").slice(0, 120) || "snippet.txt";
    const result = await dialog.showSaveDialog(win, { defaultPath: path.join(app.getPath("downloads"), file) });
    if (result.canceled || !result.filePath) return "";
    await fs.promises.writeFile(result.filePath, text, "utf8");
    return result.filePath;
  });
  // Saves an export of a design (design-export.cjs); the person picks the place.
  handle("atlas:design-export", (event, name, format, payload) => exportDesign(BrowserWindow.fromWebContents(event.sender), name, format, payload));
  handle("atlas:reveal", (_e, target) => {
    if (typeof target === "string" && path.isAbsolute(target)) shell.showItemInFolder(target);
  });
}

function shutdown() {
  if (servicesRef) browserIpc.flushAll(servicesRef);
}
module.exports = { register, shutdown };
