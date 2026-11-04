"use strict";

// A window shows one project and two windows never show the same one. Picking
// a folder moves the window it was picked in; `atlas-agent app <path>` opens a
// window of its own. A project that already has a window is focused instead.

const path = require("node:path");
const { BrowserWindow, nativeTheme, screen } = require("electron");
const prefs = require("./prefs.cjs");
const { PARTITION, hardenContents } = require("./security.cjs");

const TITLEBAR_HEIGHT = 40;
const COLORS = {
  light: { shell: "#f5f4f1", ink: "#5a5650" },
  dark: { shell: "#0b0b0c", ink: "#a6a6ad" },
};

/** @type {Map<string, BrowserWindow>} */
const windows = new Map();

/** Windows paths compare case-insensitively and without a trailing slash. */
function projectKey(project) {
  if (!project) return "";
  const norm = path.resolve(project);
  return process.platform === "win32" ? norm.toLowerCase() : norm;
}

function keyOf(win) {
  for (const [key, open] of windows) if (open === win) return key;
  return null;
}

function focus(win) {
  if (win.isMinimized()) win.restore();
  win.show();
  win.focus();
}

function palette() {
  return nativeTheme.shouldUseDarkColors ? COLORS.dark : COLORS.light;
}

function overlay() {
  const { shell, ink } = palette();
  return { color: shell, symbolColor: ink, height: TITLEBAR_HEIGHT };
}

/** Keeps saved bounds only when they still land on a connected display. */
function restoredBounds(key) {
  const saved = prefs.windowBounds(key);
  if (!saved) return { width: 1360, height: 860 };
  const area = screen.getDisplayMatching(saved).workArea;
  const visible = saved.x < area.x + area.width - 80 && saved.y < area.y + area.height - 80 && saved.x + saved.width > area.x + 80 && saved.y >= area.y - 8;
  return visible ? saved : { width: saved.width, height: saved.height };
}

/**
 * @param {string} project
 * @param {{origin: () => string, url: (project: string) => string}} ctx
 */
function open(project, ctx) {
  const key = projectKey(project);
  const existing = windows.get(key);
  if (existing && !existing.isDestroyed()) {
    focus(existing);
    return existing;
  }

  const win = new BrowserWindow({
    ...restoredBounds(key),
    minWidth: 760,
    minHeight: 520,
    show: false,
    title: "Atlas",
    icon: path.join(__dirname, "..", "build", process.platform === "win32" ? "icon.ico" : "icon.png"),
    backgroundColor: palette().shell,
    titleBarStyle: "hidden",
    titleBarOverlay: overlay(),
    webPreferences: {
      preload: path.join(__dirname, "..", "preload", "index.cjs"),
      partition: PARTITION,
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      webviewTag: false,
      spellcheck: false,
    },
  });
  windows.set(key, win);
  hardenContents(win.webContents, ctx.origin);
  win.once("ready-to-show", () => win.show());
  win.on("close", () => {
    const current = keyOf(win);
    if (current !== null && !win.isMaximized() && !win.isMinimized()) prefs.setWindowBounds(current, win.getNormalBounds());
  });
  win.on("closed", () => {
    const current = keyOf(win);
    if (current !== null) windows.delete(current);
  });
  const send = (channel, value) => {
    if (!win.isDestroyed()) win.webContents.send(channel, value);
  };
  win.on("maximize", () => send("atlas:maximized", true));
  win.on("unmaximize", () => send("atlas:maximized", false));
  if (process.env.ATLAS_APP_DEVTOOLS === "1") win.webContents.openDevTools({ mode: "detach" });
  win.loadURL(ctx.url(project));
  return win;
}

/**
 * Moves a window to another project. Runs in the project it leaves keep going
 * on the server. Returns the window that shows the project now.
 * @param {BrowserWindow} win
 * @param {string} project
 * @param {{url: (project: string) => string}} ctx
 */
function moveTo(win, project, ctx) {
  const key = projectKey(project);
  const other = windows.get(key);
  if (other === win) return win;
  if (other && !other.isDestroyed()) {
    focus(other);
    return other;
  }
  const current = keyOf(win);
  if (current !== null) windows.delete(current);
  windows.set(key, win);
  win.loadURL(ctx.url(project));
  return win;
}

function syncTheme() {
  for (const win of windows.values()) {
    if (win.isDestroyed()) continue;
    win.setBackgroundColor(palette().shell);
    win.setTitleBarOverlay(overlay());
    win.webContents.send("atlas:theme", nativeTheme.shouldUseDarkColors ? "dark" : "light");
  }
}

/** Reloads every window on a new gateway after a restart. */
function reloadAll(url) {
  for (const [key, win] of windows) {
    if (!win.isDestroyed()) win.loadURL(url(key === "" ? "" : projectOf(win), true));
  }
}

function projectOf(win) {
  try {
    return new URL(win.webContents.getURL()).searchParams.get("project") || "";
  } catch {
    return "";
  }
}

function focusAny() {
  const [win] = windows.values();
  if (win && !win.isDestroyed()) {
    if (win.isMinimized()) win.restore();
    win.focus();
  }
}

module.exports = { open, moveTo, syncTheme, reloadAll, focusAny, projectKey };
