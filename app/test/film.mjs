// Records README clips from the real desktop app: the real Atlas core, the
// real configured logins and a real model. Frames come from the Chromium
// screencast; docs/showcase/encode.py turns them into an animated WebP.
//
//   node test/film.mjs <scene> <project> <out-dir> [--bin path/to/atlas-agent.exe]
//
// The app runs with a throwaway profile (English, dark theme, onboarded), so
// nothing personal from the everyday profile is on screen.

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { _electron as electron } from "playwright-core";
import { electronExecutable } from "./electron-bin.mjs";

const [scene, project, outRoot] = process.argv.slice(2);
const binFlag = process.argv.indexOf("--bin");
const bin = binFlag > 0 ? process.argv[binFlag + 1] : "";
if (!scene || !project || !outRoot) throw new Error("usage: film.mjs <scene> <project> <out-dir> [--bin exe]");

const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const frames = path.join(path.resolve(outRoot), scene);
fs.rmSync(frames, { recursive: true, force: true });
fs.mkdirSync(frames, { recursive: true });

const userData = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-film-"));
fs.writeFileSync(path.join(userData, "prefs.json"), JSON.stringify({ theme: "dark", ui: { lang: "en", onboarded: true } }));
const args = [appDir, `--project=${path.resolve(project)}`];
if (bin) args.push(`--atlas-bin=${bin}`);
const app = await electron.launch({ executablePath: electronExecutable(), args, env: { ...process.env, ATLAS_APP_USER_DATA: userData } });
const page = await app.firstWindow();
await app.evaluate(({ BrowserWindow }) => {
  const w = BrowserWindow.getAllWindows()[0];
  w.setContentSize(1440, 900);
  w.center();
});
await page.waitForSelector(".composer textarea", { timeout: 120000 });

// Screencast frames with their capture times.
const cdp = await page.context().newCDPSession(page);
const log = [];
let n = 0;
cdp.on("Page.screencastFrame", async (f) => {
  const name = `${String(n++).padStart(5, "0")}.jpg`;
  fs.writeFileSync(path.join(frames, name), Buffer.from(f.data, "base64"));
  log.push({ name, t: f.metadata.timestamp });
  await cdp.send("Page.screencastFrameAck", { sessionId: f.sessionId }).catch(() => {});
});
const startRec = () => cdp.send("Page.startScreencast", { format: "jpeg", quality: 92, maxWidth: 1440, maxHeight: 900, everyNthFrame: 1 });
const stopRec = () => cdp.send("Page.stopScreencast");

const pause = (ms) => page.waitForTimeout(ms);
async function type(text, cps = 28) {
  for (const ch of text) {
    await page.keyboard.type(ch);
    await pause(1000 / cps + Math.random() * 25);
  }
}
async function pickModel(query) {
  await page.click(".model-trigger");
  await page.waitForSelector(".model-menu");
  await pause(500);
  await type(query, 18);
  await pause(600);
  await page.keyboard.press("Enter");
  await page.waitForSelector(".model-menu", { state: "detached" });
}
async function permissionMode(label) {
  await page.click(".permission-chip");
  await page.click(`[role="menuitemradio"]:has-text("${label}"), [role="menuitem"]:has-text("${label}"), [role="option"]:has-text("${label}")`);
  await pause(300);
}
// The turn is over once the working indicator has been gone for a while;
// tool calls make it flicker between steps.
async function untilIdle(timeout = 600000) {
  await page.waitForSelector(".working", { timeout: 60000 });
  const end = Date.now() + timeout;
  let quietSince = 0;
  while (Date.now() < end) {
    const busy = (await page.locator(".working").count()) > 0 || (await page.locator('[aria-label="Stop"]').count()) > 0;
    if (busy) quietSince = 0;
    else if (!quietSince) quietSince = Date.now();
    else if (Date.now() - quietSince > 4000) return;
    await pause(500);
  }
  throw new Error("the turn did not finish in time");
}

const scenes = {
  // 00 · The hero: a failing test found, fixed and verified.
  async hero() {
    await pickModel("sonnet 5.5");
    await permissionMode("Full access");
    await startRec();
    await pause(900);
    await page.click(".composer textarea");
    await type("TestParseConfig fails. Find out why, fix it and run the tests.");
    await pause(500);
    await page.keyboard.press("Enter");
    await untilIdle();
    await pause(2500);
  },
};

try {
  if (!scenes[scene]) throw new Error(`unknown scene ${scene}`);
  await scenes[scene]();
} finally {
  await stopRec().catch(() => {});
  fs.writeFileSync(path.join(frames, "frames.json"), JSON.stringify(log));
  await app.close();
}
console.log(`${log.length} frames in ${frames}`);
