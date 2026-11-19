"use strict";
// Records one real desktop window (a Chrome or Notepad window the agent is
// driving) as screencast-style frames, so a README clip can show what the
// agent does outside the app. Only the chosen window is captured.
//
//   electron main.cjs --title=<substring> --out=<dir> --stop=<file>
//
// It waits for a window whose title contains the substring, then writes
// JPEG frames and frames.json (same shape as film.mjs) until the stop file
// appears.
const { app, BrowserWindow, desktopCapturer, ipcMain } = require("electron");
const fs = require("node:fs");
const path = require("node:path");

const arg = (n) => (process.argv.find((a) => a.startsWith(`--${n}=`)) || "").slice(n.length + 3);
const title = arg("title").toLowerCase();
const out = path.resolve(arg("out"));
const stopFile = path.resolve(arg("stop"));
fs.mkdirSync(out, { recursive: true });
app.disableHardwareAcceleration();

const log = [];
let n = 0;
ipcMain.on("frame", (_e, dataUrl, t) => {
  const name = `${String(n++).padStart(5, "0")}.jpg`;
  fs.writeFileSync(path.join(out, name), Buffer.from(dataUrl.split(",")[1], "base64"));
  log.push({ name, t });
});

app.whenReady().then(async () => {
  let id = "";
  for (let i = 0; i < 900 && !id; i++) {
    const sources = await desktopCapturer.getSources({ types: ["window"], thumbnailSize: { width: 1, height: 1 } });
    id = sources.find((s) => s.name.toLowerCase().includes(title) && !s.name.toLowerCase().includes("film"))?.id ?? "";
    if (!id) await new Promise((r) => setTimeout(r, 400));
  }
  if (!id) { console.log("window never appeared"); app.exit(2); return; }
  const win = new BrowserWindow({ show: false, webPreferences: { nodeIntegration: true, contextIsolation: false, backgroundThrottling: false } });
  await win.loadFile(path.join(__dirname, "capture.html"), { query: { id } });
  const timer = setInterval(() => {
    if (fs.existsSync(stopFile)) {
      clearInterval(timer);
      fs.writeFileSync(path.join(out, "frames.json"), JSON.stringify(log));
      console.log(`${log.length} frames in ${out}`);
      app.exit(0);
    }
  }, 400);
});
