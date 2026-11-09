"use strict";

// The renderer may only talk to the local gateway. Everything else is cut off
// here as well as by the gateway's CSP.

const net = require("node:net");
const { net: electronNet, session, shell } = require("electron");

const PARTITION = "atlas-app";
// The built-in browser keeps its own profile: cookies and storage of the
// sites a person visits never meet the app's own session.
const BROWSER_PARTITION = "persist:atlas-browser";
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

/** A page address the built-in browser may load: http and https only. */
function webUrl(raw) {
  try {
    const url = new URL(raw);
    return (url.protocol === "https:" || url.protocol === "http:") && url.hostname !== "" ? url.toString() : "";
  } catch {
    return "";
  }
}

/** True for an address on the public internet: not loopback, private, link-local or reserved. */
function isPublicAddress(address) {
  const family = net.isIP(address);
  if (family === 4) {
    const [a, b] = address.split(".").map(Number);
    if (a === 0 || a === 10 || a === 127 || a >= 224) return false;
    if (a === 169 && b === 254) return false;
    if (a === 172 && b >= 16 && b <= 31) return false;
    if (a === 192 && b === 168) return false;
    if (a === 100 && b >= 64 && b <= 127) return false;
    if (a === 192 && b === 0) return false;
    if (a === 198 && (b === 18 || b === 19)) return false;
    return true;
  }
  if (family === 6) {
    const lower = address.toLowerCase();
    const mapped = /^::ffff:(\d+\.\d+\.\d+\.\d+)$/.exec(lower);
    if (mapped) return isPublicAddress(mapped[1]);
    if (lower === "::" || lower === "::1") return false;
    if (/^f[cd]/.test(lower) || /^fe[89ab]/.test(lower) || lower.startsWith("ff")) return false;
    return true;
  }
  return false;
}

/** Whether a host name resolves only to public addresses. The app's own fetches of web content go through this. */
async function publicHost(hostname) {
  const host = hostname.replace(/^\[|\]$/g, "");
  if (!host || host === "localhost" || host.endsWith(".localhost") || host.endsWith(".local") || host.endsWith(".internal")) return false;
  if (net.isIP(host)) return isPublicAddress(host);
  try {
    // Chromium's resolver, not the thread pool: a lookup that hangs ends with the app.
    const found = await Promise.race([electronNet.resolveHost(host), new Promise((_, reject) => setTimeout(() => reject(new Error("timeout")), 3000).unref?.())]);
    const addresses = found.endpoints.map((e) => e.address);
    return addresses.length > 0 && addresses.every(isPublicAddress);
  } catch {
    return false;
  }
}

/** Visited sites get no device permissions at all; downloads are handled in browser.cjs. */
function hardenBrowserSession() {
  const ses = session.fromPartition(BROWSER_PARTITION);
  // Sites should see a browser, not the name of the app around it.
  ses.setUserAgent(ses.getUserAgent().replace(/\s(?:Electron|Atlas|atlas-app)\/\S+/gi, ""));
  ses.setPermissionRequestHandler((_wc, _permission, callback) => callback(false));
  ses.setPermissionCheckHandler(() => false);
  ses.setDevicePermissionHandler(() => false);
  return ses;
}

/** @param {() => string} origin returns the current gateway origin */
function hardenSession(origin) {
  const ses = session.fromPartition(PARTITION);
  ses.webRequest.onBeforeRequest((details, callback) => {
    const { url } = details;
    const local = url.startsWith(`${origin()}/`) || url.startsWith("data:") || url.startsWith("blob:") || url.startsWith("devtools:");
    callback({ cancel: !local });
  });
  const localPage = (contents) => {
    try { return new URL(contents?.getURL()).origin === origin(); } catch { return false; }
  };
  ses.setPermissionRequestHandler((wc, permission, callback, details) => {
    const microphone = permission === "media" && details.mediaTypes?.length > 0 && details.mediaTypes.every((type) => type === "audio");
    callback(localPage(wc) && (ALLOWED_PERMISSIONS.has(permission) || microphone));
  });
  ses.setPermissionCheckHandler((wc, permission, _requestingOrigin, details) =>
    localPage(wc) && (ALLOWED_PERMISSIONS.has(permission) || (permission === "media" && details.mediaType === "audio")));
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
  // A <webview> is attached only through the gate (webview-gate.cjs), which the app's own windows install.
}

module.exports = { PARTITION, BROWSER_PARTITION, openExternal, webUrl, isPublicAddress, publicHost, hardenSession, hardenBrowserSession, hardenContents };
