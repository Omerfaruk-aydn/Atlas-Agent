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
const { hardenSession } = require("./security.cjs");
const windows = require("./windows.cjs");

function argValue(argv, name) {
  const prefix = `--${name}=`;
  const hit = argv.find((a) => a.startsWith(prefix));
  return hit ? hit.slice(prefix.length) : "";
}

/** The CLI sits one folder above the packaged app: <bin>/app/Atlas.exe. */
function coreNextToApp() {
  const dir = path.join(path.dirname(process.execPath), "..");
  const name = fs.existsSync(dir) ? fs.readdirSync(dir).find((f) => /^atlas-agent.*\.exe$/i.test(f)) : undefined;
  return name ? path.join(dir, name) : "";
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

function fatal(err) {
  dialog.showErrorBox("Atlas could not start", err.message);
  app.exit(1);
}

/**
 * Development only: an already running gateway instead of a child of our own.
 * The end-to-end tests point this at their fake gateway.
 */
function devGateway() {
  const origin = app.isPackaged ? "" : process.env.ATLAS_APP_DEV_GATEWAY;
  return origin ? { origin, version: "dev", takeLink: () => "", stop: async () => {}, start: async () => {} } : null;
}

function startGateway() {
  const dev = devGateway();
  if (dev) {
    gateway = dev;
    return dev.start();
  }
  gateway = new Gateway({ bin: coreBinary(), renderer: rendererDir(), onCrash: restartGateway });
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
  try {
    await startGateway();
    windows.reloadAll(pageURL);
  } catch (startErr) {
    fatal(startErr);
  }
}

if (!app.isPackaged && process.env.ATLAS_APP_USER_DATA) app.setPath("userData", process.env.ATLAS_APP_USER_DATA);

// Windows names and groups the app by this id, notifications included.
app.setName("Atlas");
app.setAppUserModelId("dev.atlas.agent");

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", (_event, argv) => {
    const project = argValue(argv, "project");
    if (project && gateway) openProject(project);
    else windows.focusAny();
  });

  app.whenReady().then(async () => {
    Menu.setApplicationMenu(null);
    nativeTheme.themeSource = prefs.load().theme;
    nativeTheme.on("updated", windows.syncTheme);
    hardenSession(origin);
    ipc.register({ origin, version: () => gateway?.version ?? "", moveToProject });
    try {
      await startGateway();
    } catch (err) {
      fatal(err);
      return;
    }
    openProject(argValue(process.argv, "project"));
  });

  app.on("window-all-closed", () => app.quit());

  // Waits for the gateway so its claims on open workspaces are released.
  app.on("will-quit", (event) => {
    if (quitting || !gateway) return;
    quitting = true;
    event.preventDefault();
    // Quit from a fresh tick: a quit issued while this event is still running
    // is dropped, which happens when the gateway has nothing left to wait for.
    gateway.stop().finally(() => setImmediate(() => app.quit()));
  });
}
