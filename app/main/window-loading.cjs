"use strict";

// A page must commit the React shell, not merely finish downloading HTML.
function watchLoading(win, { url, record, onFailure, timeout = 15_000 }) {
  let project = "";
  let timer = null;
  let retryTimer = null;
  let attempts = 0;
  let recovering = false;
  let closing = false;

  const clear = () => { clearTimeout(timer); clearTimeout(retryTimer); };
  const navigate = (restore) => {
    if (closing || win.isDestroyed()) return;
    recovering = false;
    clear();
    record("window-load-start");
    timer = setTimeout(() => recover("renderer-start-timeout"), timeout);
    try {
      Promise.resolve(win.loadURL(url(project, restore))).catch(() => recover("navigation-rejected"));
    } catch {
      recover("invalid-navigation");
    }
  };
  const recover = (reason) => {
    if (closing || win.isDestroyed() || recovering) return;
    recovering = true;
    clear();
    record("window-recovery", { reason, attempt: attempts + 1 });
    if (attempts++ === 0) {
      retryTimer = setTimeout(() => navigate(true), 200);
    } else {
      win.show();
      Promise.resolve(onFailure(reason)).then((retry) => {
        if (retry && !closing && !win.isDestroyed()) { attempts = 0; navigate(true); }
        else if (!win.isDestroyed()) win.close();
      }).catch(() => { if (!win.isDestroyed()) win.close(); });
    }
  };
  win.webContents.on("did-fail-load", (_event, code, _description, _url, mainFrame) => {
    if (mainFrame && code !== -3) recover(`load-failed:${code}`);
  });
  win.webContents.on("render-process-gone", (_event, details) => {
    record("renderer-gone", { reason: details.reason, exitCode: details.exitCode });
    if (!closing && details.reason !== "clean-exit") recover("renderer-gone");
  });
  win.on("unresponsive", () => {
    if (closing || win.isDestroyed()) return;
    record("renderer-unresponsive");
    win.webContents.forcefullyCrashRenderer();
    recover("renderer-unresponsive");
  });
  win.on("closed", () => { closing = true; clear(); });
  win.on("close", () => { closing = true; clear(); });
  return {
    load(nextProject, restore = false) { project = nextProject; attempts = 0; navigate(restore); },
    reload() { attempts = 0; navigate(true); },
    ready() {
      if (!closing && !win.isDestroyed()) {
        clear();
        recovering = false;
        if (!win.isVisible()) win.show();
        record("renderer-ready");
      }
    },
  };
}

module.exports = { watchLoading };
