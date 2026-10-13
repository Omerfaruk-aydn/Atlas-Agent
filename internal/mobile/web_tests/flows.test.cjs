  await page.locator(".task-row").first().waitFor();
  state.unavailable = true;
  await page.evaluate(() => window.testStreams.at(-1).fail());
  await new Promise((r) => setTimeout(r, 1500));
  const n = state.unavailableAt.length;
  await new Promise((r) => setTimeout(r, 800));
  assert.equal(state.unavailableAt.length, n, "no endless loop");
  assert.ok(n <= 6, `at most 6 automatic attempts, saw ${n}`);
});

// ---------- Settings ----------
test("theme persists and follows the system; preview permission matches the device; unpair is confirmed", async (t) => {
  const { page, state, url } = await fixture(t, { colorScheme: "dark", setup: (s) => { s.previews = true; } });
  await page.goto(url);
  await page.locator("#settings-btn").click();
  assert.ok(await page.locator('input[name="theme"][value="system"]').isChecked());
  assert.equal(await page.evaluate(() => getComputedStyle(document.body).backgroundColor), "rgb(20, 21, 25)", "system dark applies");
  await page.locator('label:has(input[value="light"])').click();
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), "light");
  assert.equal(await page.evaluate(() => localStorage.getItem("atlas.theme")), "light");
  await page.reload();
  await page.locator("#settings-btn").click();
  assert.ok(await page.locator('input[name="theme"][value="light"]').isChecked());
  assert.match(await page.locator("#set-previews").innerText(), /açık/);
  assert.match(await page.locator("#set-fp").textContent(), /AB:CD:EF/);
  assert.match(await page.locator("#set-computer").innerText(), /ATLAS-PC/);
  await page.locator("#settings-back").click();
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator("#menu-btn").click();
  await page.locator("#m-preview").click();
  await page.locator("#preview-img:not(.hidden)").waitFor();
  await page.locator("#back").click();
  await page.locator("#settings-btn").click();
  await page.locator("#logout").click();
  assert.equal(state.posts.length, 0, "the first tap only asks");
  await page.locator("#logout-yes").click();
  await page.locator("#view-pair").waitFor();
  assert.deepEqual(state.posts.map((p) => p.path), ["/api/logout"]);
  assert.match(await page.locator("#pair-invalid").innerText(), /eşleştirmesi kaldırıldı/);
});

test("over Tailscale Funnel settings explain the trusted certificate instead of a local fingerprint", async (t) => {
  const { page, url } = await fixture(t, { setup: (s) => { s.tailscale = true; } });
  await page.goto(url);
  await page.locator("#settings-btn").click();
  assert.equal((await page.locator("#set-fp").textContent()).trim(), "");
  assert.match(await page.locator("#set-cert-note").textContent(), /Tailscale Funnel/);
  assert.match(await page.locator("#set-cert-note").textContent(), /açık ve uyanık/);
});

test("preview control is hidden when the phone lacks permission", async (t) => {
  const { page, url } = await fixture(t);
  await page.goto(url);
  await page.locator("#settings-btn").click();
  assert.match(await page.locator("#set-previews").innerText(), /kapalı/);
  assert.match(await page.locator("#set-previews-note").innerText(), /\/mobile/);
  await page.locator("#settings-back").click();
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator("#menu-btn").click();
  assert.ok(await page.locator("#m-preview").isHidden());
});

test("notification toggle enables in-app alerts even if browser notifications are denied", async (t) => {
  const { page, url } = await fixture(t, { device: "Pixel 7" });
  await page.context().addInitScript(() => { Object.defineProperty(window, "Notification", { value: { permission: "denied" }, configurable: true }); });
  await page.goto(url);
  await page.locator("#settings-btn").click();
  assert.equal(await page.locator("#notify").isChecked(), false);
  await page.locator("#notify").check();
  await page.waitForFunction(() => localStorage.getItem("atlas.notify") === "1", null, { timeout: 3000 });
  assert.ok(await page.locator("#notify").isChecked());
  assert.match(await page.locator("#notify-note").innerText(), /push/);
  await stream(page, () => window.emit("update", { seq: 999, type: "question", session_id: "s2", data: { questions: [{ question: "Devam edelim mi?" }] } }));
  await page.locator("#toast:not(.hidden)").waitFor();
  assert.match(await page.locator("#toast").innerText(), /Devam edelim mi/);
  await page.locator("#notify").uncheck();
  await page.evaluate(() => document.getElementById("toast").classList.add("hidden"));
  await stream(page, () => window.emit("update", { seq: 1000, type: "question", session_id: "s2", data: { questions: [{ question: "Kapalı bildirim" }] } }));
  assert.ok(await page.locator("#toast").isHidden());
});

// ---------- Responsive ----------
test("agent tool images render in chat and external image URLs are ignored", async (t) => {
  const { page, url } = await fixture(t, { setup: (s) => {
    s.details.s1.messages.push({ id: "image-tool", role: "tool", text: "", created_at: 1, images: [
      { url: "/api/sessions/s1/messages/image-tool/images/0", mime_type: "image/png" },
      { url: "https://example.org/private.png", mime_type: "image/png" }
    ] });
  } });
  await page.route("**/api/sessions/s1/messages/image-tool/images/0", (route) => route.fulfill({ contentType: "image/png", body: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=", "base64") }));
  await page.goto(url);
  await page.locator('.task-row[data-id="s1"]').click();
  const image = page.locator(".chat-image img");
  await image.waitFor();
  await page.waitForFunction(() => document.querySelector(".chat-image img")?.naturalWidth > 0);
  assert.equal(await image.count(), 1);
  assert.equal(await page.locator(".chat-image").getAttribute("target"), "_blank");
  assert.ok(await noOverflow(page));
});

for (const width of [320, 360, 390, 430]) {
  test(`no horizontal overflow at ${width}px in every screen`, async (t) => {
    const { page, url } = await fixture(t, { viewport: { width, height: 800 } });
    await page.goto(url);
    await page.locator(".task-row").first().waitFor();
    assert.ok(await noOverflow(page), "list");
    await page.locator('.task-row[data-id="s2"]').click();
    await page.locator("#pending-pane button[data-decision]").first().waitFor();
    assert.ok(await noOverflow(page), "permission");
    await page.locator("#pending-pane .back-to-chat button").click();
    assert.ok(await noOverflow(page), "chat");
    for (const b of await page.locator("button:visible").all()) {
      const box = await b.boundingBox();
      if (box) assert.ok(box.height >= 32 && box.width >= 32, "touch target too small: " + (await b.innerText()));
    }
    await page.locator("#back").click();
    await page.locator("#settings-btn").click();
    assert.ok(await noOverflow(page), "settings");
  });
}
