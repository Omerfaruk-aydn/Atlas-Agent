"use strict";

// The password vault of the panel browser. Every password is encrypted with
// Electron's safeStorage (DPAPI on Windows) before it touches the disk; if
// that is not available, nothing is saved and the reason is said. Secrets leave
// this module only through reveal() and fill(), one entry at a time, and never
// through a list, a log or an error message.

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { sameOrigin, originOf } = require("./origin.cjs");

const MAX_ENTRIES = 5000;
const MAX_FIELD = 2048;

class VaultError extends Error {
  /** @param {"unavailable"|"invalid"|"missing"|"limit"} code */
  constructor(code, message) {
    super(message);
    this.code = code;
  }
}

class Vault {
  /**
   * @param {string} file where the encrypted vault lives
   * @param {{isEncryptionAvailable(): boolean, encryptString(s: string): Buffer, decryptString(b: Buffer): string}} crypt Electron's safeStorage, or a stand-in
   */
  constructor(file, crypt) {
    this.file = file;
    this.crypt = crypt;
    this.data = null;
  }

  available() {
    try {
      return this.crypt.isEncryptionAvailable();
    } catch {
      return false;
    }
  }

  load() {
    if (this.data) return this.data;
    let parsed = null;
    try {
      parsed = JSON.parse(fs.readFileSync(this.file, "utf8"));
    } catch {
      parsed = null;
    }
    const entries = Array.isArray(parsed?.entries) ? parsed.entries.filter((e) => e && typeof e.id === "string" && typeof e.origin === "string" && typeof e.secret === "string") : [];
    const never = Array.isArray(parsed?.never) ? parsed.never.filter((x) => typeof x === "string") : [];
    this.data = { entries, never };
    return this.data;
  }

  save() {
    fs.mkdirSync(path.dirname(this.file), { recursive: true });
    const tmp = `${this.file}.tmp`;
    fs.writeFileSync(tmp, JSON.stringify(this.load()), { mode: 0o600 });
    fs.renameSync(tmp, this.file);
  }

  requireEncryption() {
    if (!this.available()) throw new VaultError("unavailable", "Passwords cannot be saved: this system offers no safe storage for them.");
  }

  /** Entries without their secrets, newest first; an optional filter on site or user name. */
  list(query = "") {
    const q = String(query).trim().toLowerCase();
    return this.load()
      .entries.filter((e) => !q || e.origin.toLowerCase().includes(q) || e.username.toLowerCase().includes(q))
      .map(({ secret: _secret, ...meta }) => meta)
      .sort((a, b) => b.updated - a.updated);
  }

  /** The logins saved for exactly this origin, without their secrets. */
  forOrigin(url) {
    return this.list().filter((e) => sameOrigin(e.origin, url));
  }

  /** Saves a login, or updates the password of the same user on the same origin. */
  add(origin, username, password) {
    const result = this.put(origin, username, password);
    this.save();
    return result;
  }

  /** Puts a login in memory; the caller writes the file once it is done. */
  put(origin, username, password) {
    this.requireEncryption();
    const site = originOf(origin);
    if (!site || typeof username !== "string" || typeof password !== "string" || !password || username.length > MAX_FIELD || password.length > MAX_FIELD) {
      throw new VaultError("invalid", "That login cannot be saved.");
    }
    const data = this.load();
    const secret = this.crypt.encryptString(password).toString("base64");
    const now = Date.now();
    const existing = data.entries.find((e) => e.origin === site && e.username === username);
    if (existing) {
      existing.secret = secret;
      existing.updated = now;
      return { id: existing.id, updated: true };
    }
    if (data.entries.length >= MAX_ENTRIES) throw new VaultError("limit", "The vault is full.");
    const entry = { id: crypto.randomUUID(), origin: site, username, secret, created: now, updated: now };
    data.entries.push(entry);
    return { id: entry.id, updated: false };
  }

  /** Several logins at once, written to disk once; returns how many were saved, replaced and refused. */
  addMany(items) {
    this.requireEncryption();
    let saved = 0;
    let replaced = 0;
    let refused = 0;
    for (const item of items) {
      try {
        const result = this.put(item.origin, item.username, item.password);
        if (result.updated) replaced++;
        else saved++;
      } catch (err) {
        if (err instanceof VaultError && err.code === "unavailable") throw err;
        refused++;
      }
    }
    if (saved + replaced > 0) this.save();
    return { saved, replaced, refused };
  }

  /** Changes the user name and, when given, the password of one entry. */
  update(id, { username, password }) {
    const entry = this.load().entries.find((e) => e.id === id);
    if (!entry) throw new VaultError("missing", "That login no longer exists.");
    if (typeof username === "string") {
      if (username.length > MAX_FIELD) throw new VaultError("invalid", "That login cannot be saved.");
      entry.username = username;
    }
    if (typeof password === "string" && password) {
      this.requireEncryption();
      if (password.length > MAX_FIELD) throw new VaultError("invalid", "That login cannot be saved.");
      entry.secret = this.crypt.encryptString(password).toString("base64");
    }
    entry.updated = Date.now();
    this.save();
  }

  remove(id) {
    const data = this.load();
    const before = data.entries.length;
    data.entries = data.entries.filter((e) => e.id !== id);
    if (data.entries.length !== before) this.save();
    return data.entries.length !== before;
  }

  /** The one secret the person asked to see. */
  reveal(id) {
    const entry = this.load().entries.find((e) => e.id === id);
    if (!entry) throw new VaultError("missing", "That login no longer exists.");
    this.requireEncryption();
    return this.crypt.decryptString(Buffer.from(entry.secret, "base64"));
  }

  /** The login to fill into a page: only when the page's origin is exactly the saved one. */
  fill(id, pageUrl) {
    const entry = this.load().entries.find((e) => e.id === id);
    if (!entry || !sameOrigin(entry.origin, pageUrl)) return null;
    this.requireEncryption();
    return { username: entry.username, password: this.crypt.decryptString(Buffer.from(entry.secret, "base64")) };
  }

  neverSave(origin) {
    const site = originOf(origin);
    return !!site && this.load().never.includes(site);
  }

  /** The sites that were told "never save here". */
  neverList() {
    return [...this.load().never];
  }

  setNeverSave(origin, never) {
    const site = originOf(origin);
    if (!site) return;
    const data = this.load();
    data.never = never ? [...new Set([...data.never, site])] : data.never.filter((x) => x !== site);
    this.save();
  }

  /** Whether this exact login is already saved with this password, so a submit changes nothing. */
  isSaved(origin, username, password) {
    const site = originOf(origin);
    const entry = this.load().entries.find((e) => e.origin === site && e.username === username);
    if (!entry || !this.available()) return false;
    try {
      return this.crypt.decryptString(Buffer.from(entry.secret, "base64")) === password;
    } catch {
      return false;
    }
  }

  clear() {
    this.data = { entries: [], never: this.load().never };
    this.save();
  }
}

module.exports = { Vault, VaultError };
