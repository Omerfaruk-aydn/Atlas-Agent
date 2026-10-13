"use strict";
// Run with node --test after installing Playwright and its WebKit browser.
const { test, before, after } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { fixture: makeFixture, themeQuestion } = require("./fixture.cjs");
const fixture = (t, options) => makeFixture(browser, t, options);
const { webkit } = require("playwright");

let browser;
before(async () => { browser = await webkit.launch(); });
after(async () => { if (browser) await browser.close(); });

test("iPhone Safari shows install guidance and fits a narrow viewport", async (t) => {
  const { page, url } = await fixture(t);
  await page.goto(url);
  await page.locator(".task-row").first().waitFor();
  await page.locator("#settings-btn").click();
  await page.locator("#install-details summary").click();
  await page.locator("#iphone-install").waitFor();
  assert.match(await page.locator("#iphone-install").innerText(), /Ana Ekrana Ekle/);
  assert.ok(await page.locator("#android-install").isHidden());
  assert.ok(await page.locator("#notify").isEnabled(), "in-app alerts work without iOS notification permission");
  await page.locator("#notify").check();
  assert.equal(await page.evaluate(() => localStorage.getItem("atlas.notify")), "1");
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
  assert.equal(await page.locator('link[rel="apple-touch-icon"]').getAttribute("href"), "/apple-touch-icon.png");
  if (process.env.ATLAS_MOBILE_SCREENSHOT_DIR) {
    await fs.mkdir(process.env.ATLAS_MOBILE_SCREENSHOT_DIR, { recursive: true });
    await page.screenshot({ path: path.join(process.env.ATLAS_MOBILE_SCREENSHOT_DIR, "iphone-settings.png"), fullPage: true });
  }
  await page.setViewportSize({ width: 320, height: 568 });
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
});

test("installed mode hides install instructions; returning refreshes auth and preserves the task, prompt and answer draft", async (t) => {
  const { page, state, url } = await fixture(t, { standalone: true, setup: (s, h) => { s.details.s1.questions = [h.themeQuestion()]; } });
  await page.goto(url);
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator("#pending-pane form").waitFor();
  await page.locator('#pending-pane input[value="dark"]').check();
  await page.locator("#pending-pane .back-to-chat button").click();
  await page.locator("#prompt").fill("Henüz gönderilmemiş taslak");
  state.csrf = "csrf-after-resume";
  const reads = state.meReads;
  await page.evaluate(() => { window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true })); document.dispatchEvent(new Event("visibilitychange")); });
  await page.waitForFunction(() => window.testStreams.length === 2 && !window.testStreams[1].closed);
  assert.equal(state.meReads, reads + 1, "concurrent resume events must share one refresh");
  assert.equal(state.posts.length, 0, "resume must not repeat a user action");
  assert.ok(await page.locator("#view-task").isVisible());
  assert.equal(await page.locator("#prompt").inputValue(), "Henüz gönderilmemiş taslak");
  await page.locator("#waiting-banner").click();
  assert.ok(await page.locator('#pending-pane input[value="dark"]').isChecked(), "returning must preserve an unanswered question draft with the same identity");
  await page.locator("#pending-pane button[type=submit]").click();
  await page.locator("#pending-pane").waitFor({ state: "hidden" });
  assert.equal(state.posts.length, 1);
  assert.equal(state.posts[0].path, "/api/questions/q1");
  assert.deepEqual(state.posts[0].body.answers[0].selected_ids, ["dark"]);
  assert.equal(state.posts[0].body.answers[0].notes, undefined, "an empty note is not sent");
});

test("expired iPhone session closes its stream and returns to pairing without replaying actions", async (t) => {
  const { page, state, url } = await fixture(t);
  await page.goto(url);
  await page.locator(".task-row").first().waitFor();
  state.authenticated = false;
  await page.evaluate(() => window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true })));
  await page.locator("#view-pair").waitFor();
  assert.match(await page.locator("#pair-invalid").innerText(), /Eşleştirme sona erdi/);
  assert.ok(await page.evaluate(() => window.testStreams.every((s) => s.closed)));
  assert.equal(state.posts.length, 0);
});

test("Safari with unavailable storage can still show pairing", async (t) => {
  const { page, state, url } = await fixture(t, { deniedStorage: true });
  state.authenticated = false;
  await page.goto(url + "/#pair=fixture-only");
  await page.locator("#pair-go").waitFor();
  assert.ok(await page.locator("#pair-name").isVisible());
});

test("a replacement question does not inherit an earlier answer draft", async (t) => {
  const { page, state, url } = await fixture(t, { setup: (s) => { s.details.s1.questions = [themeQuestion("q1")]; } });
  await page.goto(url);
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator('#pending-pane input[value="dark"]').check();
  state.details.s1.questions = [themeQuestion("q2")];
  await page.evaluate(() => window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true })));
  await page.waitForFunction(() => window.testStreams.length === 2);
  await page.locator("#pending-pane form").waitFor();
  assert.equal(await page.locator('#pending-pane input[value="dark"]').isChecked(), false);
  assert.equal(state.posts.length, 0);
});

test("iPhone chat keeps the composer reachable when the keyboard shrinks the viewport", async (t) => {
  const { page, url } = await fixture(t);
  await page.goto(url);
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator("#messages .msg").first().waitFor();
  await page.locator("#prompt").focus();
  // Emulate an on-screen keyboard taking 45% of the screen.
  await page.setViewportSize({ width: 390, height: 464 });
  await page.waitForTimeout(100);
  const box = await page.locator("#send").boundingBox();
  assert.ok(box && box.y + box.height <= 464, "send button must stay inside the visible viewport");
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
});
