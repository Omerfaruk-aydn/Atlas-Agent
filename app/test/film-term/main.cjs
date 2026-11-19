"use strict";
// A bare terminal window for README clips: xterm.js drawing a real
// pseudo-terminal that runs the real atlas-agent. Nothing else is on screen.
const { app, BrowserWindow } = require("electron");
const path = require("node:path");

app.disableHardwareAcceleration();
let win = null;
let proc = null;

// Called by the recorder through app.evaluate.
global.__film = {
  start(file, args, cwd) {
    const pty = require("node-pty");
    proc = pty.spawn(file, args, { name: "xterm-256color", cols: 142, rows: 36, cwd, env: { ...process.env, PATH: (process.env.FILM_EXTRA_PATH ? process.env.FILM_EXTRA_PATH + ";" : "") + (process.env.PATH || process.env.Path || ""), ATLAS_AGENT_GLOBAL_DATA: process.env.FILM_CFG || "", ATLAS_AGENT_SKILLS_DIR: process.env.FILM_SKILLS || "", TERM: "xterm-256color", COLORTERM: "truecolor" }, useConpty: true });
    proc.onData((d) => win?.webContents.send("data", d));
    proc.onExit(() => win?.webContents.send("exit"));
  },
  send(text) { proc?.write(text); },
  kill() { try { proc?.kill(); } catch {} },
};

app.whenReady().then(() => {
  win = new BrowserWindow({ width: 1280, height: 760, useContentSize: true, resizable: false, backgroundColor: "#0d1117", webPreferences: { nodeIntegration: true, contextIsolation: false } });
  win.setMenuBarVisibility(false);
  win.loadFile(path.join(__dirname, "term.html"));
});
app.on("window-all-closed", () => app.quit());
