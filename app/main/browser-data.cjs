"use strict";

// What the panel browser remembers, kept in the main process, one profile:
// the pages visited, the notes written on pages, the zoom of each site, and
// the device presets of the device toolbar. Files are small JSON in userData.

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");

const MAX_HISTORY = 5000;
const MAX_NOTES_PER_URL = 100;
const MAX_NOTE_LENGTH = 8000;
const SAVE_DELAY_MS = 400;

/** A small JSON file that saves a moment after the last change. */
class JsonFile {
  constructor(file, fallback) {
    this.file = file;
    this.fallback = fallback;
    this.value = null;
    this.timer = null;
  }

  get() {
    if (this.value) return this.value;
    try {
      this.value = JSON.parse(fs.readFileSync(this.file, "utf8"));
    } catch {
      this.value = this.fallback();
    }
    return this.value;
  }

  touch() {
    if (this.timer) return;
    this.timer = setTimeout(() => this.flush(), SAVE_DELAY_MS);
    this.timer.unref?.();
  }

  /** For what the person chose to change (a zoom, a note, a deletion): written now, so a crash or a kill cannot lose it. */
  save() {
    this.flush();
  }

  flush() {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    if (!this.value) return;
    fs.mkdirSync(path.dirname(this.file), { recursive: true });
    const tmp = `${this.file}.tmp`;
    fs.writeFileSync(tmp, JSON.stringify(this.value));
    fs.renameSync(tmp, this.file);
  }
}

/** A page address without its fragment, so notes and history follow the page, not the anchor. */
function pageKey(raw) {
  try {
    const url = new URL(raw);
    if (url.protocol !== "http:" && url.protocol !== "https:") return "";
    url.hash = "";
    return url.toString();
  } catch {
    return "";
  }
}

class HistoryStore {
  constructor(file) {
    this.store = new JsonFile(file, () => ({ entries: [] }));
  }

  entries() {
    const data = this.store.get();
    if (!Array.isArray(data.entries)) data.entries = [];
    return data.entries;
  }

  /** Remembers a page that loaded. Failed loads never reach here. */
  record(url, title, at = Date.now()) {
    const key = pageKey(url);
    if (!key) return null;
    const entries = this.entries();
    const last = entries[0];
    if (last && last.url === key && at - last.at < 2000) {
      if (title) last.title = String(title).slice(0, 300);
      this.store.touch();
      return last;
    }
    const entry = { id: crypto.randomUUID(), url: key, title: String(title || "").slice(0, 300), at };
    entries.unshift(entry);
    if (entries.length > MAX_HISTORY) entries.length = MAX_HISTORY;
    this.store.touch();
    return entry;
  }

  /** The title often arrives after the load; the page just recorded takes it. */
  retitle(url, title) {
    const key = pageKey(url);
    const last = this.entries()[0];
    if (last && last.url === key && title) {
      last.title = String(title).slice(0, 300);
      this.store.touch();
    }
  }

  search(query = "", limit = 500) {
    const q = String(query).trim().toLowerCase();
    return this.entries()
      .filter((e) => !q || e.url.toLowerCase().includes(q) || e.title.toLowerCase().includes(q))
      .slice(0, Math.min(1000, Math.max(1, limit)));
  }

  remove(id) {
    const entries = this.entries();
    const next = entries.filter((e) => e.id !== id);
    if (next.length === entries.length) return false;
    this.store.get().entries = next;
    this.store.save();
    return true;
  }

  /** Forgets pages, all of them or those newer than `since` (a time in ms). */
  clear(since = 0) {
    const data = this.store.get();
    const before = data.entries.length;
    data.entries = since > 0 ? data.entries.filter((e) => e.at < since) : [];
    this.store.save();
    return before - data.entries.length;
  }

  flush() {
    this.store.flush();
  }
}

class NotesStore {
  constructor(file) {
    this.store = new JsonFile(file, () => ({ notes: {} }));
  }

  notes() {
    const data = this.store.get();
    if (typeof data.notes !== "object" || data.notes === null) data.notes = {};
    return data.notes;
  }

  list(url) {
    const key = pageKey(url);
    return key ? (this.notes()[key] ?? []) : [];
  }

  /** Every note, newest first, with the page it belongs to. */
  all() {
    return Object.entries(this.notes())
      .flatMap(([url, list]) => list.map((n) => ({ ...n, url })))
      .sort((a, b) => b.updated - a.updated);
  }

  add(url, text, quote = "") {
    const key = pageKey(url);
    const body = String(text ?? "").trim().slice(0, MAX_NOTE_LENGTH);
    if (!key || !body) return null;
    const list = (this.notes()[key] ??= []);
    if (list.length >= MAX_NOTES_PER_URL) return null;
    const now = Date.now();
    const note = { id: crypto.randomUUID(), text: body, quote: String(quote ?? "").slice(0, 4000), created: now, updated: now };
    list.unshift(note);
    this.store.save();
    return note;
  }

  update(url, id, text) {
    const note = this.list(url).find((n) => n.id === id);
    const body = String(text ?? "").trim().slice(0, MAX_NOTE_LENGTH);
    if (!note || !body) return null;
    note.text = body;
    note.updated = Date.now();
    this.store.save();
    return note;
  }

  remove(url, id) {
    const key = pageKey(url);
    const notes = this.notes();
    const list = notes[key];
    if (!list) return false;
    const next = list.filter((n) => n.id !== id);
    if (next.length === list.length) return false;
    if (next.length) notes[key] = next;
    else delete notes[key];
    this.store.save();
    return true;
  }

  flush() {
    this.store.flush();
  }
}

/** The zoom a person chose for a site, so it holds across tabs and restarts. */
class ZoomStore {
  constructor(file) {
    this.store = new JsonFile(file, () => ({ zoom: {} }));
  }

  hosts() {
    const data = this.store.get();
    if (typeof data.zoom !== "object" || data.zoom === null) data.zoom = {};
    return data.zoom;
  }

  get(url) {
    const host = hostOf(url);
    return host ? (this.hosts()[host] ?? 1) : 1;
  }

  set(url, factor) {
    const host = hostOf(url);
    if (!host || !Number.isFinite(factor)) return;
    if (Math.abs(factor - 1) < 0.001) delete this.hosts()[host];
    else this.hosts()[host] = factor;
    this.store.save();
  }

  clear() {
    this.store.get().zoom = {};
    this.store.save();
  }

  flush() {
    this.store.flush();
  }
}

const SEARCH_ENGINES = ["duckduckgo", "google", "bing"];

/** What the person chose in the browser's settings. Every value is checked here, whoever asks. */
class SettingsStore {
  constructor(file) {
    this.store = new JsonFile(file, () => ({}));
  }

  get() {
    const raw = this.store.get();
    return {
      searchEngine: SEARCH_ENGINES.includes(raw.searchEngine) ? raw.searchEngine : "duckduckgo",
      offerSave: raw.offerSave !== false,
      addressAutofill: raw.addressAutofill !== false,
    };
  }

  /** @returns {boolean} whether the change was a valid one */
  set(key, value) {
    const raw = this.store.get();
    if (key === "searchEngine" && SEARCH_ENGINES.includes(value)) raw.searchEngine = value;
    else if ((key === "offerSave" || key === "addressAutofill") && typeof value === "boolean") raw[key] = value;
    else return false;
    this.store.save();
    return true;
  }

  flush() {
    this.store.flush();
  }
}

function hostOf(raw) {
  try {
    const url = new URL(raw);
    return url.protocol === "http:" || url.protocol === "https:" ? url.host : "";
  } catch {
    return "";
  }
}

const MOBILE_UA = (platform, model) => `Mozilla/5.0 (${platform}) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1 ${model}`.trim();

/** Presets of the device toolbar. A device changes the viewport, the pixel ratio and the user agent together. */
const DEVICES = [
  { id: "iphone-15", name: "iPhone 15", width: 393, height: 852, ratio: 3, mobile: true, userAgent: MOBILE_UA("iPhone; CPU iPhone OS 17_4 like Mac OS X", "") },
  { id: "iphone-se", name: "iPhone SE", width: 375, height: 667, ratio: 2, mobile: true, userAgent: MOBILE_UA("iPhone; CPU iPhone OS 17_4 like Mac OS X", "") },
  { id: "pixel-8", name: "Pixel 8", width: 412, height: 915, ratio: 2.625, mobile: true, userAgent: "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36" },
  { id: "galaxy-s23", name: "Galaxy S23", width: 360, height: 780, ratio: 3, mobile: true, userAgent: "Mozilla/5.0 (Linux; Android 14; SM-S911B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36" },
  { id: "ipad", name: "iPad Air", width: 820, height: 1180, ratio: 2, mobile: true, userAgent: MOBILE_UA("iPad; CPU OS 17_4 like Mac OS X", "") },
  { id: "laptop", name: "Laptop 1280", width: 1280, height: 800, ratio: 1, mobile: false, userAgent: "" },
];

module.exports = {
  SettingsStore, JsonFile, HistoryStore, NotesStore, ZoomStore, DEVICES, pageKey, hostOf };
