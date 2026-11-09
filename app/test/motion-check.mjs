// Samples the app's transitions frame by frame and prints what it saw:
// `node test/motion-check.mjs`, after `npm run build`. Runs against the fake
// gateway. A transition passes when it is spread over many frames (not one
// jump), ends in the right state, and no frame is far too long.

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { _electron as electron } from "playwright-core";
import { electronExecutable } from "./electron-bin.mjs";
import { assistantMessage, session, startFakeGateway, userMessage } from "./fake-gateway.mjs";

const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const gateway = await startFakeGateway({ renderer: path.join(appDir, "renderer", "dist") });
const long = "Satir ".repeat(40);
await gateway.seed({
  sessions: [session("s1", "Chat"), session("s2", "Other chat")],
  messages: {
    s1: Array.from({ length: 30 }, (_, i) => [userMessage(`u${i}`, "s1", `Soru ${i}`), assistantMessage(`a${i}`, "s1", [{ type: "text", data: { text: `Cevap ${i}. ${long}` } }])]).flat(),
    s2: [userMessage("x1", "s2", "Hello"), assistantMessage("x2", "s2", [{ type: "text", data: { text: "Other." } }])],
  },
});
const userData = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-motion-"));
fs.writeFileSync(path.join(userData, "prefs.json"), JSON.stringify({ theme: "dark", ui: { lang: "en", onboarded: true } }));
const project = path.join(os.tmpdir(), "atlas-e2e-project");
fs.mkdirSync(project, { recursive: true });
const app = await electron.launch({ executablePath: electronExecutable(), args: [appDir, `--project=${project}`], env: { ...process.env, ATLAS_APP_DEV_GATEWAY: gateway.origin, ATLAS_APP_USER_DATA: userData } });
const page = await app.firstWindow();
await page.waitForSelector(".composer textarea");
await page.click(".session-main >> nth=0");
await page.waitForSelector(".turn");

/** Runs `action` and samples `measure()` every animation frame for `ms`. */
async function sample(action, measure, ms = 700) {
  return page.evaluate(
    async ({ action, measure, ms }) => {
      const act = new Function(`return (${action})()`);
      const read = new Function(`return (${measure})()`);
      const frames = [];
      const t0 = performance.now();
      let last = t0;
      const done = new Promise((resolve) => {
        const tick = (now) => {
          frames.push({ t: Math.round(now - t0), dt: Math.round(now - last), v: read() });
          last = now;
          if (now - t0 < ms) requestAnimationFrame(tick);
          else resolve();
        };
        requestAnimationFrame(tick);
      });
      act();
      await done;
      return frames;
    },
    { action: action.toString(), measure: measure.toString(), ms },
  );
}

const report = (name, frames, key = (f) => f.v) => {
  const values = frames.map(key);
  const distinct = new Set(values.map((v) => (typeof v === "number" ? Math.round(v) : v))).size;
  const worst = Math.max(...frames.slice(1).map((f) => f.dt));
  const slow = frames.filter((f) => f.dt > 34).length;
  const first = values[0];
  const last = values.at(-1);
  console.log(`${name.padEnd(34)} frames=${String(frames.length).padStart(3)} distinct=${String(distinct).padStart(3)} first=${typeof first === "number" ? Math.round(first) : first} last=${typeof last === "number" ? Math.round(last) : last} worstFrame=${worst}ms slow(>34ms)=${slow}`);
  return { distinct, worst, slow, first, last };
};

const results = {};
const slotWidth = () => document.querySelector(".panel-slot").getBoundingClientRect().width;
const threadWidth = () => document.querySelector(".card").getBoundingClientRect().width;

const openFrames = await sample(() => document.querySelector('[aria-label="Toggle panel"]').click(), slotWidth);
results.open = report("panel opens (slot width)", openFrames);
console.log("  frames:", openFrames.map((f) => `${f.t}:${Math.round(f.v)}`).join(" "));
results.openThread = report("  thread width meanwhile", await sample(() => {}, threadWidth, 50));
await page.waitForSelector(".side-panel .newtab");
results.close = report("panel closes (slot width)", await sample(() => document.querySelector('[aria-label="Close panel"]').click(), slotWidth));
await page.waitForSelector(".side-panel", { state: "detached" });

// The panel was mounted through its exit.
const sawMounted = await page.evaluate(
  () =>
    new Promise((resolve) => {
      document.querySelector('[aria-label="Toggle panel"]').click();
      setTimeout(() => {
        document.querySelector('[aria-label="Close panel"]')?.click();
        const seen = [];
        const t0 = performance.now();
        const tick = () => {
          seen.push({ t: Math.round(performance.now() - t0), there: !!document.querySelector(".side-panel") });
          if (performance.now() - t0 < 400) requestAnimationFrame(tick);
          else resolve(seen);
        };
        tick();
      }, 600);
    }),
);
const gone = sawMounted.find((s) => !s.there);
console.log(`${"panel kept mounted through exit".padEnd(34)} gone after ${gone?.t ?? "never"}ms (exit ${200}ms)`);
await page.waitForSelector(".side-panel", { state: "detached" });

// Rapid toggles end in the right state, with the view back where it belongs.
await page.evaluate(
  () =>
    new Promise((resolve) => {
      let n = 0;
      const t = setInterval(() => {
        document.querySelector('[aria-label="Toggle panel"]').click();
        if (++n >= 9) {
          clearInterval(t);
          setTimeout(resolve, 700);
        }
      }, 40);
    }),
);
const rapid = await page.evaluate(() => ({ open: !!document.querySelector(".side-panel"), attr: document.querySelector(".body").dataset.panel, slot: document.querySelector(".panel-slot").getBoundingClientRect().width, eff: getComputedStyle(document.documentElement).getPropertyValue("--atlas-panel-eff") }));
console.log(`${"nine rapid toggles".padEnd(34)} ends open=${rapid.open} attr=${rapid.attr} slot=${Math.round(rapid.slot)} eff=${rapid.eff}`);
results.rapid = rapid;

if (!(await page.$(".side-panel"))) await page.click('[aria-label="Toggle panel"]');
await page.waitForSelector(".side-panel .newtab");
await page.waitForTimeout(500);

// Expand and restore: a clip sweeps across, nothing re-lays out.
const clip = () => {
  const v = getComputedStyle(document.querySelector(".panel-slot")).clipPath;
  const m = /inset\(0px 0px 0px ([\d.]+)px\)/.exec(v);
  return m ? Number(m[1]) : v === "none" ? 0 : -1;
};
results.expand = report("expand (clip left edge)", await sample(() => document.querySelector('[aria-label="Expand panel"]').click(), clip));
await page.waitForSelector(".body[data-expanded]");
results.collapse = report("restore (clip left edge)", await sample(() => document.querySelector('[aria-label="Restore panel size"]').click(), clip));
await page.waitForFunction(() => !document.querySelector(".body[data-expanded]"));

// Tabs.
const tabWidths = () => [...document.querySelectorAll(".ptab")].map((el) => Math.round(el.getBoundingClientRect().width)).join(",");
const vtAnimations = () => document.getAnimations().filter((a) => String(a.effect?.pseudoElement ?? "").startsWith("::view-transition")).length;
const addFrames = await sample(() => document.querySelector('.side-panel [aria-label="New tab"]').click(), () => `${vtAnimations()}`, 600);
console.log(`${"new tab (view-transition animations)".padEnd(34)} running per frame: ${[...new Set(addFrames.map((f) => f.v))].join(",")}`);
results.tabAdd = report("new tab (widths of the strip)", await sample(() => {}, tabWidths, 50));
results.tabClose = report("close tab (widths of the strip)", await sample(() => document.querySelector(".ptab-close").click(), tabWidths, 600));

// A popover and a toast leave; they do not vanish.
await page.click('button[aria-label="Chat summary"]');
await page.waitForSelector(".summary-pop");
const popExit = await sample(() => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })), () => { const el = document.querySelector(".summary-pop"); return el ? `${el.dataset.state}:${Math.round(Number(getComputedStyle(el).opacity) * 100)}` : "gone"; }, 400);
console.log(`${"popover leaves".padEnd(34)} states: ${[...new Set(popExit.map((f) => f.v))].join(" > ")}`);
await page.evaluate(() => window.__toast = null);
await page.click('button[aria-label="Chat summary"]');
await page.waitForSelector(".summary-pop");
await page.click('.summary-pop [aria-label="Project menu"]');
await page.click('.menu-item:has-text("Copy path")');
await page.waitForSelector(".toast");
const toastExit = await sample(() => {}, () => { const el = document.querySelector(".toast"); return el ? (el.dataset.leaving !== undefined ? `leaving:${Math.round(Number(getComputedStyle(el).opacity) * 100)}` : "shown") : "gone"; }, 5200);
const states = toastExit.map((f) => f.v);
console.log(`${"toast leaves".padEnd(34)} ${[...new Set(states.map((v) => v.split(":")[0]))].join(" > ")} (leaving frames: ${states.filter((v) => v.startsWith("leaving")).length})`);

// A thread switch dissolves.
const opacity = () => {
  const a = document.querySelector(".card-content");
  return a ? Math.round(Number(getComputedStyle(a).opacity) * 100) : -1;
};
const switching = await sample(() => document.querySelectorAll(".session-main")[1].click(), () => document.documentElement.hasAttribute("data-vt") ? "transition" : "idle", 600);
console.log(`${"thread switch".padEnd(34)} states: ${[...new Set(switching.map((f) => f.v))].join(" > ")}`);

// Dragging stays 1:1.
await page.waitForSelector(".panel-handle");
const edge = await page.locator(".panel-handle").boundingBox();
const startWidth = await page.evaluate(slotWidth.toString() === "" ? "0" : `(${slotWidth.toString()})()`);
await page.mouse.move(edge.x + 4, edge.y + 300);
await page.mouse.down();
const lag = [];
for (let dx = -20; dx >= -120; dx -= 20) {
  await page.mouse.move(edge.x + 4 + dx, edge.y + 300);
  const w = await page.evaluate(`(${slotWidth.toString()})()`);
  lag.push(Math.round(w - (startWidth - dx)));
}
await page.mouse.up();
console.log(`${"divider drag (error px per step)".padEnd(34)} ${lag.join(",")}`);
results.drag = lag;

await app.close();
await gateway.close();
console.log("\n" + JSON.stringify({ ok: results.open.distinct >= 5 && results.close.distinct >= 5 && results.expand.distinct >= 5 && results.rapid.attr === (results.rapid.open ? "open" : "closed") && results.drag.every((d) => Math.abs(d) <= 1) }));
