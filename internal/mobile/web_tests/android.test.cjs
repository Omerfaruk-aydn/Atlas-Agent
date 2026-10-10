"use strict";
const { test, before, after } = require("node:test");
const assert = require("node:assert/strict");
const { chromium } = require("playwright");
const { fixture: makeFixture } = require("./fixture.cjs");

let browser;
before(async () => { browser = await chromium.launch({ channel: process.env.ATLAS_CHROMIUM_CHANNEL || undefined }); });
after(async () => { if (browser) await browser.close(); });
const fixture = (t, options = {}) => makeFixture(browser, t, { ...options, device: "Pixel 7" });

test("Android Chrome runs the phone panel without installation and fits narrow screens", async (t) => {
  const { page, url } = await fixture(t);
  await page.goto(url);
  await page.locator("#task-list .card").waitFor();
  await page.locator("#settings-btn").click();
  assert.ok(await page.locator("#iphone-install").isHidden());
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
  await page.setViewportSize({ width: 320, height: 640 });
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
});

test("Android network return refreshes CSRF and keeps drafts without replaying commands", async (t) => {
  const { page, state, url } = await fixture(t);
  await page.goto(url);
  await page.locator("#task-list .card").click();
  await page.locator("#pending form").waitFor();
  await page.locator("#prompt").fill("Mobil internetten gönderilecek taslak");
  await page.locator('#pending input[value="true"]').check();
  const reads = state.meReads;
  state.csrf = "android-new-csrf";
  await page.evaluate(() => {
    window.dispatchEvent(new Event("offline"));
    window.dispatchEvent(new Event("online"));
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await page.waitForFunction(() => window.testStreams.length === 2 && !window.testStreams[1].closed);
  assert.equal(state.meReads, reads + 1);
  assert.equal(state.posts.length, 0);
  assert.equal(await page.locator("#prompt").inputValue(), "Mobil internetten gönderilecek taslak");
  assert.ok(await page.locator('#pending input[value="true"]').isChecked());
  await page.locator("#pending button[type=submit]").click();
  await page.locator("#pending form").waitFor({ state: "detached" });
  assert.equal(state.posts.length, 1);
  assert.equal(state.posts[0].path, "/api/questions/q1");
});

test("Android session expiry closes the stream and requires pairing again", async (t) => {
  const { page, state, url } = await fixture(t);
  await page.goto(url);
  await page.locator("#task-list .card").waitFor();
  state.authenticated = false;
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await page.locator("#view-pair").waitFor();
  assert.ok(await page.evaluate(() => window.testStreams.every((s) => s.closed)));
  assert.equal(state.posts.length, 0);
});

test("Android with unavailable storage still opens the pairing form", async (t) => {
  const { page, state, url } = await fixture(t, { deniedStorage: true });
  state.authenticated = false;
  await page.goto(url + "/#pair=fixture-only");
  await page.locator("#pair-go").waitFor();
  assert.ok(await page.locator("#pair-name").isVisible());
});

test("Android retries an unavailable relay with growing delays, then reconnects", async (t) => {
  const { page, state, url } = await fixture(t);
  await page.goto(url);
  await page.locator("#task-list .card").waitFor();
  await page.waitForFunction(() => window.testStreams.length === 1);
  state.unavailable = true;
  await page.evaluate(() => window.testStreams[0].fail());
  while (state.unavailableAt.length < 3) await new Promise((r) => setTimeout(r, 100));
  const [a, b, c] = state.unavailableAt;
  assert.ok(c - b > b - a, `delays must grow: ${b - a}ms then ${c - b}ms`);
  assert.equal(state.posts.length, 0, "a retry never replays an action");
  state.unavailable = false;
  await page.waitForFunction(() => window.testStreams.length === 2 && window.testStreams[1].readyState === 1, null, { timeout: 15000 });
});
