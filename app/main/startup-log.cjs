"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { app } = require("electron");

// Keep local lifecycle diagnostics, never launch links, credentials or chats.
function record(event, details = {}) {
  try {
    const dir = app.getPath("userData");
    const file = path.join(dir, "startup.log");
    fs.mkdirSync(dir, { recursive: true });
    if ((fs.statSync(file, { throwIfNoEntry: false })?.size ?? 0) > 256 * 1024) fs.writeFileSync(file, "");
    fs.appendFileSync(file, JSON.stringify({ at: new Date().toISOString(), event, ...details }) + "\n");
  } catch {
    // Diagnostics must not prevent startup on a read-only or full disk.
  }
}

module.exports = { record };
