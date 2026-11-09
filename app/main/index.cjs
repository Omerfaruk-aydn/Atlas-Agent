"use strict";

// Entry point of the Atlas desktop app. `atlas-agent app [path]` starts this
// with --project and --atlas-bin. This process owns the gateway child and all
// windows; the command that started it has long since returned.

const fs = require("node:fs");
const path = require("node:path");
const { app, dialog, Menu, nativeTheme } = require("electron");
const { Gateway } = require("./gateway.cjs");
const ipc = require("./ipc.cjs");
const prefs = require("./prefs.cjs");
const { hardenBrowserSession, hardenSession } = require("./security.cjs");
const { pickCore } = require("./core-pick.cjs");
const { watchDownloads } = require("./browser.cjs");
const windows = require("./windows.cjs");
const { LaunchQueue } = require("./launch-queue.cjs");
const { record } = require("./startup-log.cjs");

// This 2D desktop UI uses software rendering on Windows to avoid driver
// failures that can leave a live renderer in an unpainted gray window.
if (process.platform === "win32") app.disableHardwareAcceleration();

function argValue(argv, name) {
  const prefix = `--${name}=`;
  const hit = argv.find((a) => a.startsWith(prefix));
  return hit ? hit.slice(prefix.length) : "";
}

/** The CLI sits one folder above the packaged app: <bin>/app/Atlas.exe. */
function coreNextToApp() {
  return pickCore(path.join(path.dirname(process.execPath), ".."));
}

/** The core binary: the one that launched us, or the CLI the app came with. */
function coreBinary() {
  const exe = process.platform === "win32" ? "atlas-agent.exe" : "atlas-agent";
  const candidates = [argValue(process.argv, "atlas-bin"), process.env.ATLAS_APP_BIN, app.isPackaged ? coreNextToApp() : path.join(__dirname, "..", "..", exe)];
  return candidates.find((c) => c && fs.existsSync(c)) || exe;
}

function rendererDir() {
  const packaged = path.join(process.resourcesPath, "renderer");
  return app.isPackaged ? packaged : path.join(__dirname, "..", "renderer", "dist");
}

let gateway = null;
let restarted = false;
let quitting = false;

// The longest a quit may take before it is forced.
const QUIT_LIMIT_MS = 7000;

const origin = () => gateway?.origin ?? "";

/** The page for a project; the first window also signs in with the launch link. */
function pageURL(project, restore = false) {
  const page = project ? `/?project=${encodeURIComponent(project)}${restore ? "&restore=1" : ""}` : "/";
  const link = gateway.takeLink();
  return link ? `${link}&next=${encodeURIComponent(page)}` : origin() + page;
}

const windowCtx = { origin, url: pageURL };
const openProject = (project) => windows.open(project, windowCtx);
const moveToProject = (win, project) => windows.moveTo(win, project, windowCtx);
const launches = new LaunchQueue(argValue(process.argv, "project"), openProject, windows.focusAny);

function fatal(err) {
  record("fatal", { message: String(err.message).slice(0, 300) });
  dialog.showErrorBox("Atlas could not start", err.message);
  app.exit(1);
}

/**
 * Development only: an already running gateway instead of a child of our own.
 * The end-to-end tests point this at their fake gateway.
 */
function devGateway() {
  const origin = app.isPackaged ? "" : process.env.ATLAS_APP_DEV_GATEWAY;
  const delay = Math.min(5000, Math.max(0, Number(process.env.ATLAS_APP_DEV_START_DELAY_MS) || 0));
  return origin ? { origin, version: "dev", takeLink: () => "", stop: async () => {}, start: () => new Promise((resolve) => setTimeout(resolve, delay)) } : null;
}

function startGateway() {
  const dev = devGateway();
  if (dev) {
    gateway = dev;
    return dev.start();
  }
  gateway = new Gateway({
    bin: coreBinary(),
    renderer: rendererDir(),
    onCrash: restartGateway,
    // An isolated run (a test, a trial build) names its own server, so it never attaches to the one the person is using.
    host: process.env.ATLAS_APP_HOST || "",
    timeout: Number(process.env.ATLAS_APP_READY_TIMEOUT_MS) || undefined,
  });
  return gateway.start();
}

/** A crashed core gets one restart; a second crash ends the app. */
async function restartGateway(err) {
  if (quitting) return;
  if (restarted) {
    fatal(err);
    return;
  }
  restarted = true;
  launches.pause();
  try {
    await startGateway();
    windows.reloadAll(pageURL);
    launches.ready();
  } catch (startErr) {
    fatal(startErr);
  }
}

if (!app.isPackaged && process.env.ATLAS_APP_USER_DATA) app.setPath("userData", process.env.ATLAS_APP_USER_DATA);

// A preview has its own profile and instance lock, so it can run beside Atlas.
const preview = process.argv.includes("--preview") || app.getVersion().includes("preview");
if (preview) app.setPath("userData", path.join(app.getPath("appData"), "Atlas Preview"));

// Windows names and groups the app by this id, notifications included.
app.setName(preview ? "Atlas Preview" : "Atlas");
app.setAppUserModelId(preview ? "dev.atlas.agent.preview" : "dev.atlas.agent");

// A stray error in a callback that runs late (a timer that outlives its window,
// say) is written to the log. Electron's default is a modal error box, which
// freezes the main process and keeps the app from closing.
process.on("uncaughtException", (err) => {
  try {
    record("uncaught-exception", { message: String(err?.stack ?? err).slice(0, 600) });
  } catch {
    // Nothing more can be done.
  }
});

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", (_event, argv) => {
    if (!quitting) launches.request(argValue(argv, "project"));
  });

  app.whenReady().then(async () => {
    record("graphics-mode", { mode: process.platform === "win32" ? "software" : "default" });
    Menu.setApplicationMenu(null);
    nativeTheme.themeSource = prefs.load().theme;
    nativeTheme.on("updated", windows.syncTheme);
    hardenSession(origin);
    watchDownloads(hardenBrowserSession());
    ipc.register({ origin, version: () => gateway?.version ?? "", moveToProject, rendererReady: windows.rendererReady });
    try {
      await startGateway();
    } catch (err) {
      fatal(err);
      return;
    }
    record("gateway-ready");
    launches.ready();
  });

  // A quit that has not finished by itself is ended: what the app keeps is
  // already written by now, and a process left behind with no window would
  // hold the single-instance lock and block the next launch. Long enough for
  // the gateway to stop cleanly (see Gateway.stop).
  let watchdog = null;
  const armWatchdog = () => {
    if (watchdog) return;
    watchdog = setTimeout(() => {
      record("quit-forced");
      app.exit(0);
    }, QUIT_LIMIT_MS);
  };
  app.on("window-all-closed", () => {
    armWatchdog();
    app.quit();
  });
  app.on("child-process-gone", (_event, details) => {
    if (details.type === "GPU") record("gpu-exit", { reason: details.reason, exitCode: details.exitCode });
  });

  // Waits for the gateway so its claims on open workspaces are released.
  // What the browser keeps is written before anything else closes, on the way
  // out by either event, and again after the windows are gone.
  const saveAll = () => {
    try {
      ipc.shutdown();
    } catch (err) {
      record("shutdown-failed", { message: String(err?.message ?? err).slice(0, 200) });
    }
  };
  app.on("before-quit", () => {
    saveAll();
    armWatchdog();
  });
  app.on("will-quit", (event) => {
    saveAll();
    if (quitting || !gateway) return;
    quitting = true;
    event.preventDefault();
    // Quit from a fresh tick: a quit issued while this event is still running
    // is dropped, which happens when the gateway has nothing left to wait for.
    gateway.stop().finally(() => setImmediate(() => app.quit()));
  });
}
