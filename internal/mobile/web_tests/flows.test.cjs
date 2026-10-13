"use strict";
// Acceptance flows for the seven phone screens, driven against fixture
// data in a real browser engine. Set ATLAS_ENGINE=webkit to run them in
// WebKit instead of Chromium.
const { test, before, after } = require("node:test");
const assert = require("node:assert/strict");
const pw = require("playwright");
const { fixture: makeFixture, themeQuestion } = require("./fixture.cjs");

let browser;
before(async () => {
  browser = process.env.ATLAS_ENGINE === "webkit" ? await pw.webkit.launch() : await pw.chromium.launch({ channel: process.env.ATLAS_CHROMIUM_CHANNEL || undefined });
});
after(async () => { if (browser) await browser.close(); });
const fixture = (t, options = {}) => makeFixture(browser, t, options);
const noOverflow = (page) => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);
const stream = (page, fn, arg) => page.evaluate(fn, arg);

// ---------- Pairing ----------
test("name → pairing request → computer code → owner approval → chat list", async (t) => {
  const { page, state, url } = await fixture(t, { setup: (s) => { s.authenticated = false; s.pairCode = "914027"; } });
  await page.goto(url + "/#pair=fixture-only");
  await page.locator("#pair-name").fill("Ayşe'nin telefonu");
  await page.locator("#pair-go").click();
  await page.locator("#pair-wait").waitFor();
  assert.equal(await page.locator("#pair-code").innerText(), "914 027", "the code comes from the API");
  assert.equal(await page.locator("#pair-sent-name").inputValue(), "Ayşe'nin telefonu");
  assert.ok(await page.locator("#pair-sent-name").getAttribute("readonly") !== null, "the sent name cannot change while waiting");
  assert.equal(state.pairName, "Ayşe'nin telefonu");
  assert.equal(await page.evaluate(() => location.hash), "", "the nonce leaves the URL once used");
  assert.match(await page.locator("#pair-status").innerText(), /onay bekleniyor/);
  state.pairState = "accepted";
  await page.locator(".task-row").first().waitFor();
  assert.ok(await page.locator("#view-list").isVisible());
});

test("pairing declined on the computer, and an invalid QR, each explain what to do", async (t) => {
  const { page, state, url } = await fixture(t, { setup: (s) => { s.authenticated = false; } });
  await page.goto(url + "/#pair=fixture-only");
  await page.locator("#pair-go").click();
  await page.locator("#pair-wait").waitFor();
  state.pairState = "rejected";
  await page.locator("#pair-done").waitFor();
  assert.match(await page.locator("#pair-done").innerText(), /reddedildi/);
  await page.goto(url + "/#pair=used-or-expired");
  await page.reload();
  await page.locator("#pair-go").click();
  await page.locator("#pair-invalid").waitFor();
  assert.match(await page.locator("#pair-invalid").innerText(), /yeni bir QR kodu/);
  assert.ok(await page.locator("#pair-none").isVisible(), "steps to get a new QR are shown");
});

test("a pending pairing expires on time, and Cancel really withdraws it", async (t) => {
  const { page, state, url } = await fixture(t, { setup: (s) => { s.authenticated = false; s.pairTTL = 1500; } });
  await page.goto(url + "/#pair=fixture-only");
  await page.locator("#pair-go").click();
  await page.locator("#pair-done").waitFor({ timeout: 5000 });
  assert.match(await page.locator("#pair-done").innerText(), /süresi doldu/);

  state.pairTTL = 300000;
  state.pairState = "pending";
  await page.goto(url + "/#pair=fixture-only");
  await page.reload();
  await page.locator("#pair-go").click();
  await page.locator("#pair-wait").waitFor();
  await page.locator("#pair-cancel").click();
  await page.locator("#pair-done").waitFor();
  assert.ok(state.pairCancelled, "the computer-side request was withdrawn");
  assert.match(await page.locator("#pair-done").innerText(), /geri çekildi/);
});

// ---------- List ----------
test("list groups by local day, searches Turkish titles and opens the right chat", async (t) => {
  const { page, url } = await fixture(t, { setup: (s) => {
    s.sessions.push({ id: "s6", title: "IŞIK ayarları", busy: false, created_at: s.sessions[0].created_at, updated_at: s.sessions[0].updated_at - 30, todos_done: 0, todos_total: 0, goal: "" });
    s.details.s6 = { session: s.sessions[5], workflow: { available: false }, todos: [], messages: [], permissions: [], questions: [] };
  } });
  await page.goto(url);
  await page.locator(".task-row").first().waitFor();
  assert.deepEqual(await page.locator("#task-list h2").allInnerTexts(), ["Bugün", "Dün"]);
  assert.match(await page.locator('.task-row[data-id="s1"]').innerText(), /Çalışıyor/);
  assert.match(await page.locator('.task-row[data-id="s2"]').innerText(), /İzin bekliyor/);
  assert.match(await page.locator('.task-row[data-id="s3"]').innerText(), /Tamamlandı/);
  assert.doesNotMatch(await page.locator('.task-row[data-id="s5"]').innerText(), /Tamamlandı/, "idle is not reported as done");
  await page.locator("#search").fill("ışık");
  assert.deepEqual(await page.locator(".task-row").evaluateAll((n) => n.map((x) => x.dataset.id)), ["s6"]);
  await page.locator("#search").fill("zzz");
  assert.match(await page.locator("#list-state").innerText(), /eşleşen sohbet yok/);
  await page.locator("#search").fill("remote");
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator("#messages .msg").first().waitFor();
  assert.equal(await page.locator("#task-h").innerText(), "Remote arayüzü");
  await page.locator("#back").click();
  await page.locator("#view-list").waitFor();
  assert.equal(await page.locator("#search").inputValue(), "remote", "search survives a round trip");
});

test("empty list and failed load each show a clear state", async (t) => {
  const { page, state, url } = await fixture(t, { setup: (s) => { s.sessions = []; } });
  await page.goto(url);
  await page.locator("#list-state").waitFor();
  assert.match(await page.locator("#list-state").innerText(), /Henüz sohbet yok/);
  state.unavailable = true;
  await page.reload();
  await page.locator("#list-state button").waitFor();
  assert.match(await page.locator("#list-state").innerText(), /yüklenemedi/);
});

// ---------- Chat ----------
test("message with attachment uses one idempotent request and clears the draft", async (t) => {
  const { page, state, url } = await fixture(t);
  await page.goto(url);
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator("#messages .msg").first().waitFor();
  assert.equal(await page.locator("#model-name").innerText(), "claude-haiku-4-5");
  assert.ok(await page.locator("#send").isDisabled(), "nothing to send yet");
  await page.locator("#attach").setInputFiles({ name: "notlar.txt", mimeType: "text/plain", buffer: Buffer.from("merhaba") });
  await page.waitForFunction(() => document.getElementById("attach-state").textContent.includes("KB"));
  assert.equal(await page.locator("#attach-name").innerText(), "notlar.txt");
  await page.locator("#attach-remove").click();
  assert.ok(await page.locator("#attach-chip").isHidden());
  await page.locator("#attach").setInputFiles({ name: "ekran.txt", mimeType: "text/plain", buffer: Buffer.from("ikinci") });
  await page.waitForFunction(() => document.getElementById("attach-state").textContent.includes("KB"));
  await page.locator("#prompt").fill("Bu dosyaya bak");
  await page.locator("#send").click();
  await page.waitForFunction(() => document.getElementById("composer-note").textContent === "Gönderildi");
  const prompts = state.posts.filter((p) => p.path.endsWith("/prompt"));
  assert.equal(prompts.length, 1);
  assert.deepEqual(prompts[0].body, { text: "Bu dosyaya bak", attachment_ids: ["up-2"] });
  assert.equal(await page.locator("#prompt").inputValue(), "");
  assert.ok(await page.locator("#attach-chip").isHidden());
});

test("streamed messages append once; a reader scrolled up is not pulled down", async (t) => {
  const { page, url } = await fixture(t);
  await page.goto(url);
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator("#messages .msg").first().waitFor();
  await page.waitForFunction(() => window.testStreams.length > 0 && window.testStreams.at(-1).readyState === 1);
  const long = Array.from({ length: 40 }, (_, i) => "Satır " + (i + 1)).join("\n");
  const at = Math.floor(Date.now() / 1000);
  await stream(page, (a) => window.emit("update", { seq: 5, type: "message", session_id: "s1", data: { id: "m9", role: "assistant", text: a.long, tools: [], tool_calls: [], created_at: a.at } }), { long, at });
  await page.waitForFunction(() => document.querySelector('#messages [data-id="m9"]'));
  await page.locator("#scroll").evaluate((n) => { n.scrollTop = 0; });
  await stream(page, (a) => window.emit("update", { seq: 6, type: "message", session_id: "s1", data: { id: "m10", role: "assistant", text: "Yeni gelen", tools: [], tool_calls: [], created_at: a.at } }), { at });
  await page.locator("#new-msgs").waitFor();
  assert.equal(await page.locator("#scroll").evaluate((n) => n.scrollTop), 0, "position is kept");
  // A replayed event (same or older sequence) must not duplicate anything.
  await stream(page, (a) => window.emit("update", { seq: 6, type: "message", session_id: "s1", data: { id: "m11", role: "assistant", text: "Tekrar", tools: [], tool_calls: [], created_at: a.at } }), { at });
  assert.equal(await page.locator('#messages [data-id="m11"]').count(), 0);
  await page.locator("#new-msgs").click();
  assert.ok(await page.locator("#new-msgs").isHidden());
});

test("stop asks once and sends the real control with the current revision", async (t) => {
  const { page, state, url } = await fixture(t);
  await page.goto(url);
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator("#stop-btn").waitFor();
  page.once("dialog", (d) => d.accept());
  await page.locator("#stop-btn").click();
  while (!state.posts.some((p) => p.path.endsWith("/control"))) await new Promise((r) => setTimeout(r, 50));
  const controls = state.posts.filter((p) => p.path.endsWith("/control"));
  assert.equal(controls.length, 1);
  assert.deepEqual(controls[0].body, { action: "stop", revision: "r1" });
});

test("hostile text, long paths, big code blocks and empty chats render safely", async (t) => {
  const evil = "<img src=x onerror=\"window.pwned=1\"> [tıkla](javascript:alert(1)) " + "C:/Users/" + "çok-uzun-klasör-adı/".repeat(12) + "dosya.go\n```\n" + "x".repeat(400) + "\n" + "satır\n".repeat(80) + "```";
  const { page, url } = await fixture(t, { viewport: { width: 360, height: 800 }, setup: (s) => {
    s.details.s1.messages.push({ id: "evil", role: "assistant", text: evil, tools: [], tool_calls: [{ name: "edit", path: "C:/" + "derin/".repeat(30) + "a.go", finished: false }], created_at: Math.floor(Date.now() / 1000) });
    s.details.s1.session.title = "Çok uzun bir Türkçe başlık: kullanıcı arayüzündeki düzenlemelerin ayrıntılı gözden geçirilmesi";
  } });
  await page.goto(url);
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator('#messages [data-id="evil"]').waitFor();
  assert.equal(await page.locator("#messages img").count(), 0);
  assert.equal(await page.evaluate(() => window.pwned), undefined);
  assert.equal(await page.locator('#messages a[href^="javascript"]').count(), 0);
  assert.ok(await noOverflow(page));
  const pre = await page.locator('#messages [data-id="evil"] pre').evaluate((n) => ({ sw: n.scrollWidth, cw: n.clientWidth }));
  assert.ok(pre.sw > pre.cw, "the code block scrolls inside its own box");
  await page.locator("#back").click();
  await page.locator('.task-row[data-id="s5"]').click();
  await page.locator("#view-task").waitFor();
  assert.equal(await page.locator("#messages .msg").count(), 0);
  assert.ok(await noOverflow(page));
});

// ---------- Permission ----------
for (const decision of ["allow", "allow_session", "deny"]) {
  test(`permission "${decision}" posts the exact pending request once`, async (t) => {
    const { page, state, url } = await fixture(t);
    await page.goto(url);
    await page.locator('.task-row[data-id="s2"]').click();
    const b = page.locator(`#pending-pane button[data-decision="${decision}"]`);
    await b.waitFor();
    assert.match(await page.locator("#pending-pane").innerText(), /Dosya değişikliğine izin ver/);
    assert.match(await page.locator("#pending-pane").innerText(), /internal\/mobile\/web\/app\.css/);
    await b.dblclick();
    await page.locator("#pending-pane").waitFor({ state: "hidden" });
    const posts = state.posts.filter((p) => p.path.startsWith("/api/permissions/"));
    assert.equal(posts.length, 1, "a double tap sends one answer");
    assert.deepEqual(posts[0].body, { decision, session_id: "s2", tool_call_id: "tc1" });
  });
}

test("permission diff is real, returning to chat is not an answer, and a stale request reloads", async (t) => {
  const { page, state, url } = await fixture(t);
  await page.goto(url);
  await page.locator('.task-row[data-id="s2"]').click();
  await page.locator("#pending-pane .detail-card .link-btn").click();
  assert.match(await page.locator("#perm-diff").innerText(), /\+  padding: 8px 16px;/);
  await page.locator("#pending-pane .back-to-chat button").click();
  assert.equal(state.posts.length, 0, "going back to the chat does not answer");
  await page.locator("#waiting-banner").waitFor();
  await page.locator("#waiting-banner").click();
  state.staleNext = true;
  await page.locator('#pending-pane button[data-decision="allow"]').click();
  await page.locator("#pending-pane").waitFor({ state: "hidden" });
  assert.match(await page.locator("#toast").innerText(), /artık beklemiyor/);
  assert.ok(await page.locator("#waiting-banner").isHidden());
});

test("a non-file permission does not claim to change a file", async (t) => {
  const { page, url } = await fixture(t, { setup: (s) => {
    s.details.s2.permissions = [{ id: "perm2", session_id: "s2", tool_call_id: "tc2", tool: "bash", action: "execute", description: "Testleri çalıştırır.", path: "D:/proje", command: "go test ./..." }];
  } });
  await page.goto(url);
  await page.locator('.task-row[data-id="s2"]').click();
  await page.locator("#pending-pane button[data-decision]").first().waitFor();
  const text = await page.locator("#pending-pane").innerText();
  assert.match(text, /Komut çalıştırmaya izin ver/);
  assert.match(text, /go test/);
  assert.doesNotMatch(text, /Değiştirilecek dosya|Değişiklikleri göster/);
});

// ---------- Question ----------
test("all question types: required answers, notes, drafts across events, and submit", async (t) => {
  const q = {
    id: "qa", session_id: "s1", tool_call_id: "tq", confirm_title: "Onay", confirm_description: "Atlas birkaç yanıt bekliyor.",
    questions: [
      { id: "go", type: "yes_no", question: "Devam edelim mi?", description: "Değişiklikler uygulanacak." },
      { id: "theme", type: "single_choice", question: "Hangi tema?", description: "Varsayılan görünüm.", choices: [{ id: "light", label: "Açık" }, { id: "dark", label: "Koyu" }] },
      { id: "parts", type: "multi_choice", question: "Hangi ekranlar?", description: "Birden fazla seçebilirsin.", choices: [{ id: "list", label: "Liste" }, { id: "chat", label: "Sohbet" }, { id: "settings", label: "Ayarlar" }] },
      { id: "why", type: "free_text", question: "Neden?", description: "Kısa bir açıklama." },
    ],
  };
  const { page, state, url } = await fixture(t, { setup: (s) => { s.details.s1.questions = [q]; } });
  await page.goto(url);
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator("#pending-pane form").waitFor();
  assert.equal(await page.locator("#pending-pane input:checked").count(), 0, "nothing is preselected");
  await page.locator('#pending-pane input[value="true"]').check();
  await page.locator("#pending-pane button[type=submit]").click();
  assert.equal(state.posts.length, 0, "missing answers are never sent");
  assert.match(await page.locator("#pending-pane").innerText(), /Bazı sorular hâlâ yanıt bekliyor/);
  assert.equal(await page.locator('#pending-pane fieldset[data-question="theme"] [aria-invalid="true"]').count(), 2);
  await page.locator('#pending-pane input[value="dark"]').check();
  await page.locator('#pending-pane input[value="list"]').check();
  await page.locator('#pending-pane input[value="chat"]').check();
  await page.locator("#q-3-text").fill("Daha okunaklı");
  await page.locator("#q-1-note").fill("Kontrastı da ölç");
  // An unrelated event must not wipe the draft.
  await stream(page, () => window.emit("update", { seq: 9, type: "busy", session_id: "s1", data: { busy: false } }));
  await new Promise((r) => setTimeout(r, 300));
  assert.equal(await page.locator("#q-3-text").inputValue(), "Daha okunaklı");
  assert.ok(await page.locator('#pending-pane input[value="dark"]').isChecked());
  await page.locator("#pending-pane button[type=submit]").click();
  await page.locator("#pending-pane").waitFor({ state: "hidden" });
  const post = state.posts.find((p) => p.path === "/api/questions/qa");
  assert.deepEqual(post.body, { session_id: "s1", answers: [
    { question_id: "go", yes: true },
    { question_id: "theme", selected_ids: ["dark"], notes: { _question: "Kontrastı da ölç" } },
    { question_id: "parts", selected_ids: ["list", "chat"] },
    { question_id: "why", fill_in_text: "Daha okunaklı" },
  ] });
  assert.ok(await page.locator("#view-task").isVisible(), "the same chat stays open after answering");
});

test("cancelling a question uses the real endpoint", async (t) => {
  const { page, state, url } = await fixture(t, { setup: (s) => { s.details.s1.questions = [themeQuestion()]; } });
  await page.goto(url);
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator("#pending-pane form").waitFor();
  await page.locator("#pending-pane .actions .btn.secondary").click();
  await page.locator("#pending-pane").waitFor({ state: "hidden" });
  assert.deepEqual(state.posts[0].body, { session_id: "s1", cancel: true });
});

// ---------- Offline ----------
test("disconnect keeps the chat and draft, blocks sending and answers, then reconnects without duplicates", async (t) => {
  const { page, state, url } = await fixture(t, { setup: (s) => { s.details.s1.questions = [themeQuestion()]; } });
  await page.goto(url);
  await page.locator('.task-row[data-id="s1"]').click();
  await page.locator("#pending-pane form").waitFor();
  await page.locator("#pending-pane .back-to-chat button").click();
  await page.locator("#prompt").fill("Bağlantı gidince kaybolmasın");
  state.unavailable = true;
  await page.evaluate(() => window.testStreams.at(-1).fail());
  await page.locator("#offline-banner").waitFor();
  assert.match(await page.locator("#status-card").innerText(), /Durum doğrulanamıyor/);
  assert.match(await page.locator("#status-card").innerText(), /Son güncelleme \d\d:\d\d/);
  assert.match(await page.locator("#task-conn").innerText(), /Çevrimdışı/);
  assert.equal(await page.locator("#messages .msg").count(), 2, "the conversation is not cleared");
  assert.ok(await page.locator("#send").isDisabled());
  assert.equal(await page.locator("#prompt").inputValue(), "Bağlantı gidince kaybolmasın");
  assert.ok(await page.locator("#stop-btn").isHidden(), "no stop control while state is unverified");
  assert.equal(await page.locator("#activity-time").innerText(), "2/3", "no live timer while offline");
  await page.locator("#waiting-banner").click();
  assert.ok(await page.locator("#pending-pane button[type=submit]").isDisabled(), "a pending question is not answered before it is verified");
  await page.locator("#pending-pane .back-to-chat button").click();
  state.unavailable = false;
  await page.locator("#retry-btn").click();
  await page.waitForFunction(() => document.getElementById("offline-banner").classList.contains("hidden"));
  await page.waitForFunction(() => !document.getElementById("send").disabled, null, { timeout: 5000 });
  assert.equal(await page.locator("#messages .msg").count(), 2, "reload does not duplicate messages");
  assert.equal(state.posts.length, 0);
});

test("retries stop after a bounded number of attempts", async (t) => {
  const { page, state, url } = await fixture(t);
  await page.addInitScript(() => {
    const orig = window.setTimeout;
    window.setTimeout = (fn, ms, ...a) => orig(fn, Math.min(ms || 0, 20), ...a);
  });
  await page.goto(url);
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
