// Scores a static web site in the current directory for the Atlas task bench.
//
// Usage: node judge.mjs   (run inside the workspace)
//
// The task's own checks live in .bench/checks.mjs, which the bench copies over
// the workspace after the agent has finished. That module exports
//
//   export const threshold = 85;            // pass mark, 0-100
//   export const checks = [{ name, points, run }];
//
// where run({ open, page }) resolves to true, false or a fraction from 0 to 1.
// open(options) loads the site in a fresh page; options.width defaults to
// 1280, options.colorScheme to "light".
//
// Every site is also judged on generic quality: it must load without console
// errors, make no requests outside its own origin, fit a 375px phone without
// sideways scrolling, keep tap targets usable, pass axe WCAG 2 A/AA rules and
// follow the reader's dark-mode preference. Those generic points are weighted
// by the share of task points earned. The score is the share of all points
// earned; it is printed as "BENCH_SCORE: N" and the exit status is 0 only when
// it reaches the threshold.

import { createServer } from "node:http";
import { readFile, stat } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { chromium } from "playwright-core";

const require = createRequire(import.meta.url);
const axeSource = await readFile(require.resolve("axe-core/axe.min.js"), "utf8");

const root = process.cwd();
const checkTimeoutMs = 20_000;

const types = {
  ".html": "text/html; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".webp": "image/webp",
  ".gif": "image/gif",
  ".ico": "image/x-icon",
  ".woff2": "font/woff2",
  ".woff": "font/woff",
};

// Serves the workspace, refusing dot paths (.git, .bench, .atlas*) and
// anything outside it.
function serve() {
  const server = createServer(async (req, res) => {
    try {
      const url = new URL(req.url, "http://localhost");
      let rel = decodeURIComponent(url.pathname);
      if (rel.endsWith("/")) rel += "index.html";
      const parts = rel.split("/").filter(Boolean);
      if (parts.some((p) => p.startsWith(".") || p.includes("\\"))) {
        res.writeHead(404).end();
        return;
      }
      const file = path.join(root, ...parts);
      if (!file.startsWith(root + path.sep)) {
        res.writeHead(404).end();
        return;
      }
      const info = await stat(file);
      if (!info.isFile()) {
        res.writeHead(404).end();
        return;
      }
      const type = types[path.extname(file).toLowerCase()] ?? "application/octet-stream";
      res.writeHead(200, { "content-type": type, "cache-control": "no-store" });
      res.end(await readFile(file));
    } catch {
      res.writeHead(404).end();
    }
  });
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => resolve(server));
  });
}

// Uses an installed Chrome or Edge so no browser download is needed.
async function launch() {
  const channels = process.env.BENCH_BROWSER_CHANNEL
    ? [process.env.BENCH_BROWSER_CHANNEL]
    : ["chrome", "msedge", undefined];
  let last;
  for (const channel of channels) {
    try {
      return await chromium.launch({ channel, headless: true });
    } catch (err) {
      last = err;
    }
  }
  throw last;
}

function withTimeout(promise, ms, label) {
  let timer;
  return Promise.race([
    promise,
    new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error(`${label} timed out after ${ms} ms`)), ms);
    }),
  ]).finally(() => clearTimeout(timer));
}

const server = await serve();
const baseURL = `http://127.0.0.1:${server.address().port}/`;
const browser = await launch();

const consoleErrors = [];
const externalRequests = new Set();
const openPages = [];

async function open({ width = 1280, height = 800, colorScheme = "light", reducedMotion = "reduce" } = {}) {
  const context = await browser.newContext({
    viewport: { width, height },
    colorScheme,
    reducedMotion,
    isMobile: width < 768,
    hasTouch: width < 768,
  });
  await context.route("**/*", (route) => {
    const url = route.request().url();
    if (url.startsWith(baseURL) || url.startsWith("data:") || url.startsWith("blob:")) {
      return route.continue();
    }
    externalRequests.add(url);
    return route.abort();
  });
  const page = await context.newPage();
  // A missing element should fail its check quickly, not after Playwright's
  // default 30 second wait.
  page.setDefaultTimeout(3000);
  page.on("console", (msg) => {
    if (msg.type() === "error") consoleErrors.push(msg.text());
  });
  page.on("pageerror", (err) => consoleErrors.push(String(err)));
  openPages.push(context);
  const response = await page.goto(baseURL, { waitUntil: "load" });
  if (!response || !response.ok()) {
    throw new Error(`index.html did not load (status ${response?.status()})`);
  }
  return page;
}

const generic = [
  {
    name: "index.html loads",
    points: 3,
    run: async ({ open }) => {
      const page = await open();
      return (await page.locator("body").innerText()).trim().length > 0;
    },
  },
  {
    name: "fits a 375px phone without sideways scrolling",
    points: 8,
    run: async ({ open }) => {
      const page = await open({ width: 375, height: 812 });
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      );
      if (overflow > 1) return { score: 0, note: `page is ${overflow}px wider than the screen` };
      return true;
    },
  },
  {
    name: "tap targets on a phone are at least 24px",
    points: 4,
    run: async ({ open }) => {
      const page = await open({ width: 375, height: 812 });
      const small = await page.evaluate(() => {
        const found = [];
        for (const el of document.querySelectorAll("a[href], button, input, select, textarea, [role=tab]")) {
          const box = el.getBoundingClientRect();
          const style = getComputedStyle(el);
          if (box.width === 0 || box.height === 0 || style.visibility === "hidden") continue;
          if (el.matches("input[type=checkbox], input[type=radio]")) continue;
          // Links inside running text are exempt, as in WCAG 2.5.8.
          if (el.matches("a") && getComputedStyle(el).display === "inline" && el.closest("p, li") && el.parentElement.textContent.trim() !== el.textContent.trim()) continue;
          if (box.height < 24 || box.width < 24) found.push((el.textContent || el.getAttribute("aria-label") || el.tagName).trim().slice(0, 30));
        }
        return found;
      });
      if (small.length === 0) return true;
      return { score: Math.max(0, 1 - small.length / 5), note: `too small: ${small.slice(0, 5).join(", ")}` };
    },
  },
  {
    name: "no axe WCAG 2 A/AA violations (light and dark)",
    points: 15,
    run: async ({ open }) => {
      const found = new Map();
      for (const colorScheme of ["light", "dark"]) {
        for (const width of [1280, 375]) {
          const page = await open({ width, colorScheme });
          await page.addScriptTag({ content: axeSource });
          const result = await page.evaluate(async () => {
            const r = await window.axe.run(document, { runOnly: ["wcag2a", "wcag2aa"] });
            return r.violations.map((v) => ({ id: v.id, impact: v.impact, nodes: v.nodes.length }));
          });
          for (const v of result) found.set(v.id, `${v.id} (${v.impact}, ${v.nodes} nodes, ${colorScheme} ${width}px)`);
        }
      }
      if (found.size === 0) return true;
      return { score: Math.max(0, 1 - found.size / 4), note: [...found.values()].join("; ") };
    },
  },
  {
    name: "follows the reader's dark-mode preference",
    points: 3,
    run: async ({ open }) => {
      const background = async (colorScheme) => {
        const page = await open({ colorScheme });
        return page.evaluate(() => {
          const body = getComputedStyle(document.body).backgroundColor;
          const html = getComputedStyle(document.documentElement).backgroundColor;
          return body === "rgba(0, 0, 0, 0)" ? html : body;
        });
      };
      const light = await background("light");
      const dark = await background("dark");
      return light !== dark ? true : { score: 0, note: `background is ${light} in both` };
    },
  },
];

// Visual design, rated by a vision model. It costs tokens and is not exactly
// repeatable, so it runs only when asked for (BENCH_DESIGN=1).
if (process.env.BENCH_DESIGN === "1") {
  const { rate } = await import("./design.mjs");
  generic.push({
    name: "visual design (vision rubric, 3 = generic template)",
    points: 25,
    timeoutMs: 360_000,
    run: async ({ open }) => rate(open),
  });
}

// Judged after every other check, since they are the ones that exercise the
// page and could surface an error.
const finalChecks = [
  {
    name: "no console errors or uncaught exceptions",
    points: 4,
    run: async () => (consoleErrors.length === 0 ? true : { score: 0, note: [...new Set(consoleErrors)].slice(0, 3).join(" | ") }),
  },
  {
    name: "no requests outside the site (CDNs, web fonts, trackers)",
    points: 3,
    run: async () => (externalRequests.size === 0 ? true : { score: 0, note: [...externalRequests].slice(0, 3).join(", ") }),
  },
];

let task = { threshold: 85, checks: [] };
const checksFile = path.join(root, ".bench", "checks.mjs");
try {
  task = await import(pathToFileURL(checksFile).href);
} catch (err) {
  console.log(`could not load the task checks ${checksFile}: ${err.message}`);
  console.log("BENCH_SCORE: 0");
  await browser.close();
  server.close();
  process.exit(1);
}

const lines = [];
const tally = { task: { earned: 0, total: 0 }, generic: { earned: 0, total: 0 } };
const all = [
  ...task.checks.map((check) => ({ ...check, group: "task" })),
  ...[...generic, ...finalChecks].map((check) => ({ ...check, group: "generic" })),
];
for (const check of all) {
  tally[check.group].total += check.points;
  let score = 0;
  let note = "";
  try {
    const result = await withTimeout(check.run({ open }), check.timeoutMs ?? checkTimeoutMs, check.name);
    if (typeof result === "object" && result !== null) {
      score = result.score ?? 0;
      note = result.note ?? "";
    } else if (typeof result === "number") {
      score = result;
    } else {
      score = result ? 1 : 0;
    }
  } catch (err) {
    note = String(err.message ?? err).split("\n")[0];
  } finally {
    for (const context of openPages.splice(0)) await context.close().catch(() => {});
  }
  score = Math.min(1, Math.max(0, score));
  tally[check.group].earned += score * check.points;
  const mark = score === 1 ? "ok  " : "FAIL";
  lines.push(`${mark} ${(score * check.points).toFixed(1).padStart(4)}/${check.points}  ${check.name}${note ? ` - ${note}` : ""}`);
}

await browser.close();
server.close();

// Generic quality counts only as far as the task is done: an empty page has
// no accessibility errors, but that is not a well-built site.
const taskShare = tally.task.total === 0 ? 1 : tally.task.earned / tally.task.total;
const earned = tally.task.earned + tally.generic.earned * taskShare;
const total = tally.task.total + tally.generic.total;
const final = total === 0 ? 0 : Math.round((earned / total) * 1000) / 10;
console.log(`task checks ${tally.task.earned.toFixed(1)}/${tally.task.total}, generic ${tally.generic.earned.toFixed(1)}/${tally.generic.total} weighted by task share ${taskShare.toFixed(2)}`);
// Failures last, so the tail of the output the bench keeps shows them.
for (const line of [...lines.filter((l) => l.startsWith("ok")), ...lines.filter((l) => !l.startsWith("ok"))]) console.log(line);
console.log(`threshold ${task.threshold}`);
console.log(`BENCH_SCORE: ${final}`);
process.exit(final >= task.threshold ? 0 : 1);
