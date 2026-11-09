"use strict";

// The address book of the panel browser, on the same model as the password
// vault: every record is encrypted with Electron's safeStorage before it
// touches the disk, and nothing is saved when that is not available. A record
// leaves this module to the app's own page (the manager) and, one at a time,
// to the top frame of a page when the person pressed the fill button there.

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");

const MAX_ENTRIES = 200;
const MAX_FIELD = 300;
/** The fields of an address, in the order they are asked for. */
const FIELDS = ["name", "organization", "street", "street2", "city", "region", "postal", "country", "phone", "email"];

class AddressError extends Error {
  /** @param {"unavailable"|"invalid"|"missing"|"limit"} code */
  constructor(code, message) {
    super(message);
    this.code = code;
  }
}

/** Keeps only the known fields, as short strings. */
function clean(record) {
  const out = {};
  for (const field of FIELDS) {
    const value = record?.[field];
    out[field] = typeof value === "string" ? value.trim().slice(0, MAX_FIELD) : "";
  }
  return out;
}

class AddressBook {
  /** @param {string} file @param {{isEncryptionAvailable(): boolean, encryptString(s: string): Buffer, decryptString(b: Buffer): string}} crypt */
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
    let entries = [];
    try {
      const parsed = JSON.parse(fs.readFileSync(this.file, "utf8"));
      if (Array.isArray(parsed?.entries)) entries = parsed.entries.filter((e) => e && typeof e.id === "string" && typeof e.secret === "string");
    } catch {
      entries = [];
    }
    this.data = { entries };
    return this.data;
  }

  save() {
    fs.mkdirSync(path.dirname(this.file), { recursive: true });
    const tmp = `${this.file}.tmp`;
    fs.writeFileSync(tmp, JSON.stringify(this.load()), { mode: 0o600 });
    fs.renameSync(tmp, this.file);
  }

  require() {
    if (!this.available()) throw new AddressError("unavailable", "Addresses cannot be saved: this system offers no safe storage for them.");
  }

  decode(entry) {
    return { id: entry.id, updated: entry.updated, ...clean(JSON.parse(this.crypt.decryptString(Buffer.from(entry.secret, "base64")))) };
  }

  /** Every address, newest first. Only the app's own page asks for this. */
  list() {
    if (!this.available()) return [];
    const out = [];
    for (const entry of this.load().entries) {
      try {
        out.push(this.decode(entry));
      } catch {
        // A record that no longer decrypts is skipped.
      }
    }
    return out.sort((a, b) => b.updated - a.updated);
  }

  /** How many are saved, and a short label for each, for a list that shows no more than that. */
  labels() {
    return this.list().map((a) => ({ id: a.id, label: [a.name || a.organization, a.street, a.city].filter(Boolean).join(", ") || a.email || a.phone }));
  }

  get(id) {
    const entry = this.load().entries.find((e) => e.id === id);
    if (!entry) throw new AddressError("missing", "That address no longer exists.");
    this.require();
    return this.decode(entry);
  }

  /** Saves a new address, or changes the one with this id. */
  save_(id, record) {
    this.require();
    const fields = clean(record);
    if (!FIELDS.some((f) => fields[f])) throw new AddressError("invalid", "An address needs at least one field.");
    const data = this.load();
    const secret = this.crypt.encryptString(JSON.stringify(fields)).toString("base64");
    const now = Date.now();
    if (id) {
      const entry = data.entries.find((e) => e.id === id);
      if (!entry) throw new AddressError("missing", "That address no longer exists.");
      entry.secret = secret;
      entry.updated = now;
      this.save();
      return { id };
    }
    if (data.entries.length >= MAX_ENTRIES) throw new AddressError("limit", "The address book is full.");
    const entry = { id: crypto.randomUUID(), secret, created: now, updated: now };
    data.entries.push(entry);
    this.save();
    return { id: entry.id };
  }

  remove(id) {
    const data = this.load();
    const before = data.entries.length;
    data.entries = data.entries.filter((e) => e.id !== id);
    if (data.entries.length !== before) this.save();
    return data.entries.length !== before;
  }

  clear() {
    this.data = { entries: [] };
    this.save();
  }
}

module.exports = { AddressBook, AddressError, FIELDS };
