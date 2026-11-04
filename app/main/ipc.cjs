"use strict";

// The renderer's whole view of the desktop. Network work goes straight to the
// gateway over HTTP; only what a page cannot do lives here. Every call is
// checked to come from the gateway's own page.

const { app, BrowserWindow, dialog, ipcMain, nativeImage, nativeTheme, net, Notification, shell } = require("electron");
const { execFile } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const prefs = require("./prefs.cjs");
const { openExternal } = require("./security.cjs");

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
  const branch = /^## (?:No commits yet on )?([^.\s]+(?:\.[^.\s]+)*)/.exec(head ?? "")?.[1] ?? "HEAD";
  return {
    branch,
    dirty: files.length,
    ahead: Number(/ahead (\d+)/.exec(head ?? "")?.[1] ?? 0),
    behind: Number(/behind (\d+)/.exec(head ?? "")?.[1] ?? 0),
  };
}

const favicons = new Map();

/** Pages may not reach the web, so favicons come through here, size-limited. */
async function favicon(host) {
  if (typeof host !== "string" || !/^[a-z0-9.-]{1,253}$/i.test(host)) return "";
  if (favicons.has(host)) return favicons.get(host);
  let icon = "";
  try {
    const res = await net.fetch(`https://${host}/favicon.ico`, { signal: AbortSignal.timeout(FAVICON_TIMEOUT_MS), credentials: "omit" });
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
  handle("atlas:open-external", (_e, url) => openExternal(String(url)));
  handle("atlas:reveal", (_e, target) => {
    if (typeof target === "string" && path.isAbsolute(target)) shell.showItemInFolder(target);
  });
}

module.exports = { register };
