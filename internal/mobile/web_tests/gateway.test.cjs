"use strict";
// Acceptance against the real gateway: TLS, the paired cookie, CSRF,
// credential cards, returned control and the live screen stream. The Go
// side runs TestBrowserAcceptanceServer with a synthetic screen and a
// simulated computer that accepts pairing; nothing here touches a real
// account. Set ATLAS_ENGINE=webkit for WebKit with the iPhone 13 profile.
const { test, before, after } = require("node:test");
const assert = require("node:assert/strict");
const { spawn } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const pw = require("playwright");

const webkit = process.env.ATLAS_ENGINE === "webkit";
const root = path.resolve(__dirname, "..", "..", "..");
const SECRET = "Synth-Phone-Pass-1";
let server, browser, context, page, info;
const debugLog = [];

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function ctl(method, p) {
  const resp = await fetch(info.control + p, { method });
  return resp.json();
}
async function until(fn, ms = 10000, what = "condition") {
  const end = Date.now() + ms;
  for (;;) {
    const v = await fn();
    if (v) return v;
    if (Date.now() > end) throw new Error("timed out waiting for " + what);
    await sleep(100);
  }
}

before(async () => {
  const file = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "atlas-e2e-")), "info.json");
  server = spawn("go", ["test", "-p", "1", "-count=1", "-timeout", "30m", "-run", "^TestBrowserAcceptanceServer$", "./internal/mobile"], {
    cwd: root, env: { ...process.env, ATLAS_MOBILE_E2E_INFO: file }, stdio: ["ignore", "inherit", "inherit"],
  });
  info = await until(() => { try { return JSON.parse(fs.readFileSync(file, "utf8")); } catch (_) { return null; } }, 600000, "the gateway to start");
  browser = webkit ? await pw.webkit.launch() : await pw.chromium.launch({ channel: process.env.ATLAS_CHROMIUM_CHANNEL || undefined });
  const device = webkit ? pw.devices["iPhone 13"] : pw.devices["Pixel 7"];
  context = await browser.newContext({ ...device, ignoreHTTPSErrors: true, locale: "tr-TR", serviceWorkers: "block" });
  page = await context.newPage();
  page.on("console", (m) => debugLog.push(m.type() + ": " + m.text()));
  page.on("requestfailed", (r) => debugLog.push("failed " + r.url() + " " + (r.failure() || {}).errorText));
  page.on("response", (r) => { if (r.url().includes("/api/credentials")) debugLog.push("resp " + r.status()); });
});
after(async () => {
  if (browser) await browser.close();
  if (info) await ctl("POST", "/quit").catch(() => {});
  if (server) await new Promise((r) => { server.once("exit", r); setTimeout(r, 10000); });
});

async function openTask() {
  await page.goto(info.gateway + "/");
  await page.locator(".task-row").first().click();
}

test("pairs over TLS with the computer's approval", async () => {
  const { url } = await ctl("GET", "/pair");
  await page.goto(info.gateway + "/" + new URL(url).hash);
  await page.locator("#pair-name").fill("Test telefonu");
  await page.locator("#pair-go").click();
  await page.locator(".task-row").first().waitFor({ timeout: 15000 });
  assert.equal(await page.evaluate(() => location.hash), "");
  const cookies = await context.cookies();
  assert.ok(cookies.some((c) => c.name === "__Host-atlas_phone" && c.httpOnly && c.secure));
});

test("credential card: show/hide, one delivery on double tap, value cleared and never stored", async () => {
  const { id } = await ctl("POST", "/card");
  await openTask();
  const form = page.locator(`form.cred-form[data-credential="${id}"]`);
  await form.waitFor();
  const input = form.locator("input.input");
  assert.equal(await input.getAttribute("type"), "password");
  assert.match(await form.innerText(), /login\.example\.test/);
  await form.locator(".secret-row button").click();
  assert.equal(await input.getAttribute("type"), "text");
  await form.locator(".secret-row button").click();
  assert.equal(await input.getAttribute("type"), "password");

  await input.fill(SECRET);
  // A double tap: two clicks in the same task, before any response.
  await form.locator('button[data-role="use"]').evaluate((b) => { b.click(); b.click(); });
  try { await form.waitFor({ state: "detached", timeout: 8000 }); } catch (e) {
    console.log("# DEBUG", await form.innerText(), JSON.stringify(await ctl("GET", "/card/" + id)), JSON.stringify(debugLog));
    throw e;
  }
  const card = await ctl("GET", "/card/" + id);
  assert.equal(card.state, "provided");
  assert.equal(card.persist, false, "temporary unless the user opts in");
  const check = await ctl("POST", "/card/" + id + "/check?expect=" + encodeURIComponent(SECRET));
  assert.equal(check.match, true, "exactly what was typed arrived");
  const again = await ctl("POST", "/card/" + id + "/check?expect=" + encodeURIComponent(SECRET));
  assert.notEqual(again.error, "", "a credential is used once");

  const leaked = await page.evaluate((s) => {
    const all = document.documentElement.outerHTML + JSON.stringify(localStorage) + JSON.stringify(sessionStorage) + location.href;
    return all.includes(s);
  }, SECRET);
  assert.equal(leaked, false);
});

test("credential card cancel withdraws the request", async () => {
  const { id } = await ctl("POST", "/card");
  await openTask();
  const form = page.locator(`form.cred-form[data-credential="${id}"]`);
  await form.waitFor();
  await form.locator("input.input").fill("Synth-Not-Sent");
  await form.locator("button.btn.secondary").click();
  await form.waitFor({ state: "detached" });
  const card = await ctl("GET", "/card/" + id);
  assert.equal(card.state, "canceled");
});

test("return control resumes once through the real control path", async () => {
  const before = await ctl("GET", "/state");
  await ctl("POST", "/pause?reason=" + encodeURIComponent("Doğrulama kodu"));
  await openTask();
  const b = page.locator("#return-control");
  await b.waitFor();
  assert.match(await page.locator(".control-card").innerText(), /Doğrulama kodu/);
  await b.click();
  const st = await until(async () => { const s = await ctl("GET", "/state"); return s.runs > before.runs ? s : null; }, 10000, "the resumed run");
  assert.ok(st.controls.includes("interaction_resume"));
  assert.equal(st.paused, false);
  assert.ok(!st.prompts.some((p) => p.includes("/resume-use")), "the command text never reaches the model");
  await page.locator("#return-control").waitFor({ state: "detached" });
  await sleep(500);
  assert.equal((await ctl("GET", "/state")).runs, st.runs, "no duplicate run");
});

test("a canceled task is not revived by returning control", async () => {
  const before = await ctl("GET", "/state");
  await ctl("POST", "/pause?canceled=1");
  await openTask();
  await page.locator("#return-control").click();
  await until(async () => (await ctl("GET", "/state")).paused === false, 10000, "the control to be returned");
  await sleep(500);
  assert.equal((await ctl("GET", "/state")).runs, before.runs);
});

test("live screen streams binary frames, follows orientation, and stops", async () => {
  await ctl("POST", "/previews?on=1");
  await openTask();
  await page.locator("#screen-btn").click();
  await until(() => page.evaluate(() => window.__atlasScreen.metrics.frames >= 5), 10000, "frames");
  const start = await page.evaluate(() => ({ f: window.__atlasScreen.metrics.frames, b: window.__atlasScreen.metrics.bytes, t: Date.now() }));
  await sleep(5000);
  const end = await page.evaluate(() => ({ f: window.__atlasScreen.metrics.frames, b: window.__atlasScreen.metrics.bytes, t: Date.now(), w: document.getElementById("screen-canvas").width, h: document.getElementById("screen-canvas").height, lat: window.__atlasScreen.metrics.latency.slice() }));
  const fps = (end.f - start.f) / ((end.t - start.t) / 1000);
  const lat = end.lat.sort((a, b) => a - b);
  console.log(`# ${webkit ? "webkit" : "chromium"} rendered ${fps.toFixed(1)} fps, ${Math.round((end.b - start.b) / 5 / 1024)} KiB/s, ${end.w}x${end.h}, p50 ${lat[Math.floor(lat.length / 2)]} ms p95 ${lat[Math.floor(lat.length * 0.95)]} ms`);
  assert.ok(fps > 5, "frames keep arriving: " + fps);
  assert.ok(end.w > end.h);
  assert.equal((await ctl("GET", "/state")).screen_viewers, 1);

  async function assertFrameFit() {
    await until(() => page.evaluate(() => {
      const vp = document.getElementById("screen-viewport"), c = document.getElementById("screen-canvas");
      return Math.abs(vp.clientHeight - c.getBoundingClientRect().height) < 2;
    }), 5000, "the frame to fit without an empty strip");
    const sizes = await page.evaluate(() => {
      const vp = document.getElementById("screen-viewport"), c = document.getElementById("screen-canvas");
      const tools = document.querySelector(".screen-tools").getBoundingClientRect();
      return { width: vp.clientWidth, height: vp.clientHeight, ratio: c.width / c.height, toolsBottom: tools.bottom, windowHeight: innerHeight };
    });
    assert.ok(Math.abs(sizes.width / sizes.height - sizes.ratio) < 0.03, "the viewport preserves the frame aspect ratio");
    assert.ok(sizes.toolsBottom <= sizes.windowHeight + 1, "controls remain on screen");
  }
  await assertFrameFit();
  const fittedHeight = await page.locator("#screen-viewport").evaluate(el => el.clientHeight);
  await page.locator("#screen-zoom-in").click();
  assert.equal(await page.locator("#screen-viewport").evaluate(el => el.clientHeight), fittedHeight, "zoom does not stretch the screen box");
  assert.ok(await page.locator("#screen-viewport").evaluate(el => el.scrollWidth > el.clientWidth && el.scrollHeight > el.clientHeight), "zoomed frames can be panned");
  await page.locator("#screen-fit").click();
  await page.locator("#screen-full").click();
  await assertFrameFit();
  await page.locator("#screen-full").click();
  await assertFrameFit();

  await ctl("POST", "/screen?mode=portrait");
  await until(() => page.evaluate(() => { const c = document.getElementById("screen-canvas"); return c.height > c.width; }), 5000, "portrait frames");
  await assertFrameFit();
  await page.screenshot({ path: path.join(root, "dist", "dev", "mobile-screen-portrait.png") });
  await ctl("POST", "/screen?mode=black");
  await until(() => page.evaluate(() => !document.getElementById("screen-overlay").classList.contains("hidden")), 5000, "the blank screen notice");
  await ctl("POST", "/screen?mode=landscape");
  await until(() => page.evaluate(() => { const c = document.getElementById("screen-canvas"); return c.width > c.height; }), 5000, "landscape frames");
  await assertFrameFit();
  await page.screenshot({ path: path.join(root, "dist", "dev", "mobile-screen-landscape.png") });

  await page.locator("#screen-stop").click();
  await until(async () => (await ctl("GET", "/state")).screen_viewers === 0, 5000, "the stream to close");
  await until(async () => (await ctl("GET", "/state")).screen_running === false, 5000, "capture to stop");
});

test("turning previews off ends a running stream", async () => {
  await page.locator("#screen-btn").click();
  await until(async () => (await ctl("GET", "/state")).screen_viewers === 1, 5000, "a viewer");
  await ctl("POST", "/previews?on=0");
  await until(async () => (await ctl("GET", "/state")).screen_viewers === 0, 5000, "the stream to end");
});

test("revoking the phone on the computer returns it to pairing", async () => {
  await ctl("POST", "/previews?on=1");
  await openTask();
  await page.locator("#screen-btn").click();
  await until(async () => (await ctl("GET", "/state")).screen_viewers === 1, 5000, "a viewer");
  await ctl("POST", "/revoke");
  await until(async () => (await ctl("GET", "/state")).screen_viewers === 0, 5000, "the stream to end");
  await page.locator("#view-pair").waitFor({ timeout: 10000 });
});
