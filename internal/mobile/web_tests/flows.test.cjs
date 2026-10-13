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
