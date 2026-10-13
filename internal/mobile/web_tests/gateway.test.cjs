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
