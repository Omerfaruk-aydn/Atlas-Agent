"use strict";

// Preferences that must survive restarts. The gateway's port changes on every
// start, so the renderer's localStorage cannot hold them.

const fs = require("node:fs");
const path = require("node:path");
const { app } = require("electron");

const THEMES = new Set(["system", "light", "dark"]);
const MOTIONS = new Set(["system", "full", "reduce"]);
// Drafts are the largest thing kept here.
const MAX_UI_VALUE = 512 * 1024;

let cache = null;

function file() {
  return path.join(app.getPath("userData"), "prefs.json");
}

function load() {
  if (cache) return cache;
  try {
    cache = JSON.parse(fs.readFileSync(file(), "utf8"));
  } catch {
    cache = {};
  }
  if (typeof cache !== "object" || cache === null) cache = {};
  if (!THEMES.has(cache.theme)) cache.theme = "system";
  if (!MOTIONS.has(cache.motion)) cache.motion = "system";
  if (typeof cache.windows !== "object" || cache.windows === null) cache.windows = {};
  if (typeof cache.ui !== "object" || cache.ui === null) cache.ui = {};
  return cache;
}

function save() {
  const tmp = `${file()}.tmp`;
  fs.mkdirSync(path.dirname(tmp), { recursive: true });
  fs.writeFileSync(tmp, JSON.stringify(load(), null, 2));
  fs.renameSync(tmp, file());
}

function setTheme(theme) {
  if (!THEMES.has(theme)) return false;
  load().theme = theme;
  save();
  return true;
}

function setMotion(motion) {
  if (!MOTIONS.has(motion)) return false;
  load().motion = motion;
  save();
  return true;
}

function setUI(key, value) {
  if (typeof key !== "string" || key.length > 4200) return;
  if ((JSON.stringify(value)?.length ?? 0) > MAX_UI_VALUE) return;
  load().ui[key] = value;
  save();
}

function windowBounds(key) {
  return load().windows[key];
}

function setWindowBounds(key, bounds) {
  load().windows[key] = bounds;
  save();
}

module.exports = { load, setTheme, setMotion, setUI, windowBounds, setWindowBounds };
