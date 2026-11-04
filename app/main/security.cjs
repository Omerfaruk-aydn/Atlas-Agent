"use strict";

// The renderer may only talk to the local gateway. Everything else is cut off
// here as well as by the gateway's CSP.

const { session, shell } = require("electron");

const PARTITION = "atlas-app";
const ALLOWED_PERMISSIONS = new Set(["clipboard-sanitized-write", "notifications"]);

/** Opens a link in the system browser, for schemes that are safe to open. */
function openExternal(raw) {
  try {
    const url = new URL(raw);
    if (url.protocol === "https:" || url.protocol === "http:" || url.protocol === "mailto:") {
      shell.openExternal(url.toString());
      return true;
    }
  } catch {
    // Ignore malformed URLs.
  }
  return false;
}

/** @param {() => string} origin returns the current gateway origin */
function hardenSession(origin) {
  const ses = session.fromPartition(PARTITION);
  ses.webRequest.onBeforeRequest((details, callback) => {
    const { url } = details;
    const local = url.startsWith(`${origin()}/`) || url.startsWith("data:") || url.startsWith("blob:") || url.startsWith("devtools:");
    callback({ cancel: !local });
  });
  ses.setPermissionRequestHandler((_wc, permission, callback) => callback(ALLOWED_PERMISSIONS.has(permission)));
  ses.setPermissionCheckHandler((_wc, permission) => ALLOWED_PERMISSIONS.has(permission));
}

/** @param {import("electron").WebContents} contents @param {() => string} origin */
function hardenContents(contents, origin) {
  contents.setWindowOpenHandler(({ url }) => {
    openExternal(url);
    return { action: "deny" };
  });
  contents.on("will-navigate", (event, url) => {
    let target = "";
    try {
      target = new URL(url).origin;
    } catch {
      // Treated as leaving the app.
    }
    if (target !== origin()) {
      event.preventDefault();
      openExternal(url);
    }
  });
  contents.on("will-attach-webview", (event) => event.preventDefault());
}

module.exports = { PARTITION, openExternal, hardenSession, hardenContents };
