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
