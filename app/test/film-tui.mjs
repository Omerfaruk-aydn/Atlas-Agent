// Records README clips of the real atlas-agent running in a terminal.
//
//   node test/film-tui.mjs <scene> <project> <out-dir> --bin <atlas-agent.exe>
//
// A bare Electron window draws a real pseudo-terminal with xterm.js, so the
// recording shows the real TUI, the real model and the real tool output.
// Frames go to <out-dir>/<scene>; docs/showcase/encode.py makes the WebP.

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { _electron as electron } from "playwright-core";
import { electronExecutable } from "./electron-bin.mjs";

const [scene, project, outRoot] = process.argv.slice(2);
const flag = (n) => { const i = process.argv.indexOf(n); return i > 0 ? process.argv[i + 1] : ""; };
const bin = flag("--bin");
if (!scene || !project || !outRoot || !bin) throw new Error("usage: film-tui.mjs <scene> <project> <out-dir> --bin <exe>");

const here = path.dirname(fileURLToPath(import.meta.url));
const frames = path.join(path.resolve(outRoot), scene);
fs.rmSync(frames, { recursive: true, force: true });
fs.mkdirSync(frames, { recursive: true });

const app = await electron.launch({ executablePath: electronExecutable(), args: [path.join(here, "film-term", "main.cjs")] });
const page = await app.firstWindow();
await page.waitForFunction(() => window.__film);

const cdp = await page.context().newCDPSession(page);
const log = [];
let n = 0;
cdp.on("Page.screencastFrame", async (f) => {
  const name = `${String(n++).padStart(5, "0")}.jpg`;
  fs.writeFileSync(path.join(frames, name), Buffer.from(f.data, "base64"));
  log.push({ name, t: f.metadata.timestamp });
  await cdp.send("Page.screencastFrameAck", { sessionId: f.sessionId }).catch(() => {});
});
const startRec = () => cdp.send("Page.startScreencast", { format: "jpeg", quality: 92, maxWidth: 1280, maxHeight: 760, everyNthFrame: 3 });
const stopRec = () => cdp.send("Page.stopScreencast");

const pause = (ms) => page.waitForTimeout(ms);
const screen = () => page.evaluate(() => window.__film.text());
async function type(text, cps = 26) {
  for (const ch of text) {
    await app.evaluate((_, c) => global.__film.send(c), ch);
    await pause(1000 / cps + Math.random() * 30);
  }
}
const enter = () => app.evaluate(() => global.__film.send(String.fromCharCode(13)));
const key = (seq) => app.evaluate((_, s) => global.__film.send(s), seq);
/** Waits until the screen has been still for `quiet` ms and `pattern` shows. */
async function settle(pattern, { quiet = 3500, timeout = 300000 } = {}) {
  const end = Date.now() + timeout;
  let last = "", since = Date.now();
  while (Date.now() < end) {
    const now = await screen();
    if (now !== last) { last = now; since = Date.now(); }
    else if (Date.now() - since >= quiet && (!pattern || new RegExp(pattern, "i").test(now))) return now;
    await pause(400);
  }
  throw new Error(`screen never settled on ${pattern}`);
}
/** True while the agent is working: the footer then offers "stop". */
const busy = async () => /ctrl\+c stop/.test(await screen());
/** Sends a prompt and waits until the agent has finished working on it. */
async function ask(prompt, { timeout = 900000 } = {}) {
  await type(prompt);
  await pause(500);
  await enter();
  const end = Date.now() + timeout;
  for (let i = 0; i < 60 && !(await busy()); i++) await pause(500);
  let idleSince = 0;
  while (Date.now() < end) {
    if (await busy()) idleSince = 0;
    else if (!idleSince) idleSince = Date.now();
    else if (Date.now() - idleSince > 3500) return;
    await pause(500);
  }
  throw new Error("the agent did not finish in time");
}
/** Runs a slash command: no model work to wait for. */
async function command(text) {
  await type(text);
  await pause(400);
  await enter();
  await pause(2500);
}
const start = (args = [], file = bin) => app.evaluate((_, [f, a, c]) => global.__film.start(f, a, c), [file, args, path.resolve(project)]);

import(pathToFileURL(path.join(here, "film-scenes", `${scene}.mjs`)).href).then(async (m) => {
  try {
    await m.default({ page, start, ask, command, type, enter, key, settle, pause, screen, startRec, stopRec, bin, project: path.resolve(project) });
  } finally {
    await stopRec().catch(() => {});
    fs.writeFileSync(path.join(frames, "frames.json"), JSON.stringify(log));
    await app.evaluate(() => global.__film.kill?.()).catch(() => {});
    await Promise.race([app.close(), new Promise((r) => setTimeout(r, 5000))]);
    console.log(`${log.length} frames in ${frames}`);
    process.exit(0);
  }
});
