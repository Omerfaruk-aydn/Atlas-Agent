"use strict";

// Cookies from another browser, read the way the current user may read them:
//  - Firefox keeps them in plain text in cookies.sqlite.
//  - Chromium browsers (Chrome, Edge) encrypt them. Values written with the
//    older, user-level scheme (a DPAPI-protected key and AES-GCM, prefix v10
//    or v11) are decrypted here with that key, asked of the current user's own
//    DPAPI. Values with the "v20" prefix are app-bound: only the browser's own
//    service may decrypt them. They are NOT bypassed, not by injecting into
//    the browser, not by its elevation service, not by impersonating it; they
//    are counted so the person can be told.
// Nothing here touches a running browser, and nothing is written back.

const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFile } = require("node:child_process");

const MAX_COOKIES = 20_000;
const WIN_EPOCH_SECONDS = 11_644_473_600;

/** The profiles of the supported browsers under the given roots. */
function listProfiles(env = process.env) {
  const out = [];
  const roaming = env.APPDATA ?? "";
  const local = env.LOCALAPPDATA ?? "";

  const iniFile = roaming ? path.join(roaming, "Mozilla", "Firefox", "profiles.ini") : "";
  if (iniFile && fs.existsSync(iniFile)) {
    const sections = fs.readFileSync(iniFile, "utf8").split(/\r?\n(?=\[)/);
    for (const section of sections) {
      const value = (key) => new RegExp(`^${key}=(.*)$`, "m").exec(section)?.[1]?.trim() ?? "";
      if (!/^\[Profile/i.test(section) || !value("Path")) continue;
      const dir = value("IsRelative") === "0" ? value("Path") : path.join(roaming, "Mozilla", "Firefox", value("Path"));
      const cookies = path.join(dir, "cookies.sqlite");
      if (fs.existsSync(cookies)) out.push({ browser: "firefox", id: `firefox:${dir}`, name: value("Name") || path.basename(dir), dir, cookies });
    }
  }

  const chromium = [
    ["chrome", "Google", "Chrome"],
    ["edge", "Microsoft", "Edge"],
  ];
  for (const [browser, vendor, product] of chromium) {
    const root = local ? path.join(local, vendor, product, "User Data") : "";
    if (!root || !fs.existsSync(path.join(root, "Local State"))) continue;
    let names = {};
    try {
      names = JSON.parse(fs.readFileSync(path.join(root, "Local State"), "utf8")).profile?.info_cache ?? {};
    } catch {
      names = {};
    }
    for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
      if (!entry.isDirectory() || !/^(Default|Profile \d+)$/.test(entry.name)) continue;
      const dir = path.join(root, entry.name);
      const cookies = [path.join(dir, "Network", "Cookies"), path.join(dir, "Cookies")].find((f) => fs.existsSync(f));
      if (cookies) out.push({ browser, id: `${browser}:${dir}`, name: names[entry.name]?.name || entry.name, dir, cookies, localState: path.join(root, "Local State") });
    }
  }
  return out;
}

/** Copies a database aside (browsers keep theirs open) and runs `read` on the copy. */
function withCopy(file, read) {
  const { DatabaseSync } = require("node:sqlite");
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-cookies-"));
  const copy = path.join(dir, "cookies.sqlite");
  try {
    fs.copyFileSync(file, copy);
    for (const ext of ["-wal", "-shm"]) if (fs.existsSync(file + ext)) fs.copyFileSync(file + ext, copy + ext);
    const db = new DatabaseSync(copy, { readOnly: true });
    try {
      return read(db);
    } finally {
      db.close();
    }
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

const sameSiteOf = { 0: "no_restriction", 1: "lax", 2: "strict" };

/** @returns {{cookies: object[], appBound: number, failed: number}} */
function readFirefoxCookies(file) {
  return withCopy(file, (db) => {
    const rows = db.prepare("SELECT name, value, host, path, expiry, isSecure, isHttpOnly, sameSite, originAttributes FROM moz_cookies LIMIT ?").all(MAX_COOKIES);
    const cookies = rows
      .filter((r) => !r.originAttributes)
      .map((r) => ({
        name: String(r.name),
        value: String(r.value),
        host: String(r.host),
        path: String(r.path || "/"),
        // Firefox counts milliseconds in newer profiles and seconds in older ones.
        expires: r.expiry > 1e11 ? Math.floor(r.expiry / 1000) : Number(r.expiry) || 0,
        secure: !!r.isSecure,
        httpOnly: !!r.isHttpOnly,
        sameSite: sameSiteOf[r.sameSite] ?? "unspecified",
      }));
    return { cookies, appBound: 0, failed: 0 };
  });
}

/** Asks the current user's own DPAPI to unwrap a blob. Resolves with a Buffer. */
function unprotectWithDpapi(blob) {
  return new Promise((resolve, reject) => {
    const script = "Add-Type -AssemblyName System.Security; [Convert]::ToBase64String([System.Security.Cryptography.ProtectedData]::Unprotect([Convert]::FromBase64String($env:ATLAS_BLOB), $null, 'CurrentUser'))";
    execFile("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command", script], { env: { ...process.env, ATLAS_BLOB: blob.toString("base64") }, windowsHide: true, timeout: 15_000 }, (err, stdout) => {
      if (err) reject(err);
      else resolve(Buffer.from(stdout.trim(), "base64"));
    });
  });
}

/** The AES key a Chromium profile's v10/v11 values use, or null when it cannot be had. */
async function chromiumKey(localState, unprotect) {
  try {
    const encoded = JSON.parse(fs.readFileSync(localState, "utf8")).os_crypt?.encrypted_key;
    if (typeof encoded !== "string") return null;
    const wrapped = Buffer.from(encoded, "base64");
    if (wrapped.subarray(0, 5).toString() !== "DPAPI") return null;
    return await unprotect(wrapped.subarray(5));
  } catch {
    return null;
  }
}

/**
 * @param {{cookies: string, localState: string}} profile
 * @param {{unprotect?: (blob: Buffer) => Promise<Buffer>}} [options]
 * @returns {Promise<{cookies: object[], appBound: number, failed: number}>}
 */
async function readChromiumCookies(profile, options = {}) {
  const unprotect = options.unprotect ?? unprotectWithDpapi;
  const key = await chromiumKey(profile.localState, unprotect);
  return withCopy(profile.cookies, (db) => {
    // Chromium counts microseconds since 1601: past what a double holds exactly.
    const query = db.prepare("SELECT host_key, name, value, encrypted_value, path, expires_utc, is_secure, is_httponly, samesite FROM cookies LIMIT ?");
    query.setReadBigInts(true);
    const rows = query.all(MAX_COOKIES);
    const cookies = [];
    let appBound = 0;
    let failed = 0;
    for (const r of rows) {
      let value = r.value ? String(r.value) : "";
      const enc = r.encrypted_value ? Buffer.from(r.encrypted_value) : Buffer.alloc(0);
      if (!value && enc.length > 0) {
        const tag = enc.subarray(0, 3).toString();
        if (tag === "v20") {
          appBound++;
          continue;
        }
        if ((tag === "v10" || tag === "v11") && key && key.length === 32 && enc.length >= 3 + 12 + 16) {
          try {
            const decipher = crypto.createDecipheriv("aes-256-gcm", key, enc.subarray(3, 15));
            decipher.setAuthTag(enc.subarray(enc.length - 16));
            value = Buffer.concat([decipher.update(enc.subarray(15, enc.length - 16)), decipher.final()]).toString("utf8");
          } catch {
            failed++;
            continue;
          }
        } else {
          failed++;
          continue;
        }
      }
      const micros = Number(r.expires_utc) || 0;
      cookies.push({
        name: String(r.name),
        value,
        host: String(r.host_key),
        path: String(r.path || "/"),
        expires: micros > 0 ? Math.max(0, Math.floor(micros / 1e6) - WIN_EPOCH_SECONDS) : 0,
        secure: Number(r.is_secure) !== 0,
        httpOnly: Number(r.is_httponly) !== 0,
        sameSite: sameSiteOf[Number(r.samesite)] ?? "unspecified",
      });
    }
    return { cookies, appBound, failed };
  });
}

/** Puts cookies into a session. Returns how many it took and how many it refused. */
async function importCookies(ses, cookies) {
  let imported = 0;
  let refused = 0;
  for (const c of cookies.slice(0, MAX_COOKIES)) {
    const bare = c.host.replace(/^\./, "");
    if (!/^[a-z0-9.-]+$/i.test(bare) || !c.name) {
      refused++;
      continue;
    }
    try {
      await ses.cookies.set({
        url: `${c.secure ? "https" : "http"}://${bare}${c.path.startsWith("/") ? c.path : `/${c.path}`}`,
        name: c.name,
        value: c.value,
        domain: c.host,
        path: c.path,
        secure: c.secure,
        httpOnly: c.httpOnly,
        expirationDate: c.expires > 0 ? c.expires : undefined,
        // SameSite=None is only valid on a Secure cookie.
        sameSite: c.sameSite === "no_restriction" && !c.secure ? "unspecified" : c.sameSite,
      });
      imported++;
    } catch {
      refused++;
    }
  }
  return { imported, refused };
}

module.exports = { listProfiles, readFirefoxCookies, readChromiumCookies, importCookies, unprotectWithDpapi };
