"use strict";
// A loopback stand-in for the phone gateway API. It serves the real panel
// files and answers with the same JSON shapes as internal/mobile/handlers.go.
// The Go tests cover the real gateway, TLS, cookies, pairing and
// authorization; these fixtures only drive the browser UI.
const assert = require("node:assert/strict");
const http = require("node:http");
const fs = require("node:fs/promises");
const path = require("node:path");
const { devices } = require("playwright");

const now = () => Math.floor(Date.now() / 1000);

// sampleData returns realistic, fixture-only conversation data.
function sampleData() {
  const t = now();
  const yesterday = new Date();
  yesterday.setDate(yesterday.getDate() - 1);
  yesterday.setHours(18, 24, 0, 0);
  const y = Math.floor(yesterday.getTime() / 1000);
  const sessions = [
    { id: "s1", title: "Remote arayüzü", busy: true, created_at: t - 7200, updated_at: t - 60, todos_done: 2, todos_total: 3, goal: "" },
    { id: "s2", title: "Mobil yerleşim", busy: false, created_at: t - 9000, updated_at: t - 900, todos_done: 0, todos_total: 0, goal: "", pending_permissions: 1 },
    { id: "s3", title: "Hesap sonuçları", busy: false, created_at: t - 12000, updated_at: t - 5400, todos_done: 3, todos_total: 3, goal: "" },
    { id: "s4", title: "Tarayıcı akışları", busy: false, created_at: y - 3600, updated_at: y, todos_done: 0, todos_total: 0, goal: "Yeni günlük dosyası oluştur" },
    { id: "s5", title: "Proje incelemesi", busy: false, created_at: y - 20000, updated_at: y - 15000, todos_done: 0, todos_total: 0, goal: "" },
  ];
  const workflow = { available: true, revision: "r1", paused: false, busy: false };
  const details = {
    s1: {
      session: sessions[0], workflow: { ...workflow, busy: true },
      todos: [
        { content: "Mobil yerleşimi incele", active_form: "Mobil yerleşim inceleniyor", status: "completed" },
        { content: "Sohbet alanını güncelle", active_form: "Sohbet alanı güncelleniyor", status: "completed" },
        { content: "İzin ekranını hazırla", active_form: "İzin ekranı hazırlanıyor", status: "in_progress" },
      ],
      messages: [
        { id: "m1", role: "user", text: "Mobil sohbet ekranını düzenle.\nİzinleri de buradan yanıtlayabileyim.", tools: [], tool_calls: [], created_at: t - 84, finished: true },
        { id: "m2", role: "assistant", text: "Sohbeti sadeleştiriyorum. İzin ve soru akışlarını aynı ekrana bağlıyorum.", model: "claude-haiku-4-5", tools: ["edit"], tool_calls: [{ name: "edit", path: "internal/mobile/web/app.css", finished: true }], created_at: t - 70, finished: false },
      ],
      permissions: [], questions: [],
    },
    s2: {
      session: sessions[1], workflow, todos: [],
      messages: [{ id: "m3", role: "user", text: "Mobil yerleşimi düzelt.", tools: [], tool_calls: [], created_at: t - 1000, finished: true }],
      permissions: [{
        id: "perm1", session_id: "s2", tool_call_id: "tc1", tool: "edit", action: "write",
        description: "Atlas, mobil sohbet ekranının stilini güncellemek istiyor.", path: "internal/mobile/web",
        file_path: "internal/mobile/web/app.css", additions: 2, removals: 1,
        diff: "--- a/internal/mobile/web/app.css\n+++ b/internal/mobile/web/app.css\n@@ -1,3 +1,4 @@\n .thread {\n-  padding: 8px;\n+  padding: 8px 16px;\n+  gap: 12px;\n }\n",
      }],
      questions: [],
    },
    s3: { session: sessions[2], workflow: { available: false }, todos: [], messages: [], permissions: [], questions: [] },
    s4: { session: sessions[3], workflow: { available: false }, todos: [], messages: [], permissions: [], questions: [] },
    s5: { session: sessions[4], workflow: { available: false }, todos: [], messages: [], permissions: [], questions: [] },
  };
  return { sessions, details };
}

function themeQuestion(id = "q1") {
  return {
    id, session_id: "s1", tool_call_id: "tq1",
    questions: [{
      id: "look", type: "single_choice", label: "Görünüm",
      question: "Atlas, mobil sohbetin görünümü için bir seçim bekliyor.", description: "",
      choices: [
        { id: "light", label: "Açık tema", description: "Beyaz zemin, net tipografi." },
        { id: "dark", label: "Koyu tema", description: "Düşük ışıkta daha rahat." },
        { id: "system", label: "Sistem ayarı", description: "Telefonun görünümünü takip et." },
      ],
    }],
  };
}

async function fixture(browser, t, options = {}) {
  const data = sampleData();
  const state = {
    authenticated: true, unavailable: false, unavailableAt: [], meReads: 0, posts: [], csrf: "csrf-1",
    previews: false, previewAvailable: true, staleNext: false, failNext: false,
    pairState: "pending", pairCode: "305 719".replace(" ", ""), pairCancelled: false, uploads: 0,
    ...data,
  };
  if (options.setup) options.setup(state, { themeQuestion });
  const send = (res, status, body) => { res.writeHead(status, { "Content-Type": "application/json" }); res.end(JSON.stringify(body)); };
  const fail = (res, status, code, message) => send(res, status, { error: { code, message } });
  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, "http://fixture");
    const p = url.pathname;
    if (p.startsWith("/api/")) {
      let body = "";
      for await (const chunk of req) body += chunk;
      if (p === "/api/pair") {
        const b = JSON.parse(body);
        if (b.nonce !== "fixture-only") return fail(res, 403, "pairing_invalid", "pairing link is invalid, expired or already used");
        state.pairName = b.name;
        return send(res, 200, { pending_id: "pp-1", code: state.pairCode, name: b.name, poll: "poll-1", expires: Date.now() + (state.pairTTL || 300000), fingerprint: "fixture", computer_os: "Windows" });
      }
      if (p === "/api/pair/poll") {
        if (state.pairState === "accepted") state.authenticated = true;
        return send(res, 200, { state: state.pairState });
      }
      if (p === "/api/pair/cancel") {
        if (state.pairState !== "pending") return fail(res, 409, "not_pending", "already decided");
        state.pairState = "cancelled";
        state.pairCancelled = true;
        return send(res, 200, { state: "cancelled" });
      }
      if (!state.authenticated) return fail(res, 401, "not_paired", "expired");
      if (state.unavailable) { state.meReads++; state.unavailableAt.push(Date.now()); return fail(res, 503, "offline", "Atlas is offline"); }
      if (req.method === "GET") {
        if (p === "/api/me") {
          state.meReads++;
          return send(res, 200, { device: { id: "phone-1", name: "Telefonum", previews: state.previews, paired_at: Date.parse("2026-10-01T09:00:00Z") }, language: options.language || "tr", version: "v0.16.3-dev", csrf: state.csrf, seq: 1, fingerprint: state.tailscale ? "" : "AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89", tailscale: !!state.tailscale, preview_available: state.previewAvailable, computer: { name: "ATLAS-PC", os: "Windows" } });
        }
        if (p === "/api/sessions") {
          const sessions = state.sessions.map((s) => {
            const d = state.details[s.id];
            return { ...s, pending_permissions: d ? d.permissions.length : 0, pending_questions: d ? d.questions.length : 0 };
          });
          return send(res, 200, { sessions, seq: 1 });
        }
        const m = p.match(/^\/api\/sessions\/([^/]+)$/);
        if (m) {
          const d = state.details[decodeURIComponent(m[1])];
          if (!d) return fail(res, 404, "not_found", "no such task");
          return send(res, 200, { ...d, seq: 1 });
        }
        if (p === "/api/preview") {
          if (!state.previews) return fail(res, 403, "previews_off", "screen previews are off for this phone");
          res.writeHead(200, { "Content-Type": "image/png" });
          return res.end(await fs.readFile(path.join(__dirname, "../web/icon-192.png")));
        }
        return fail(res, 404, "not_found", "unknown");
      }
      assert.equal(req.headers["x-csrf-token"], state.csrf);
      assert.ok(req.headers["idempotency-key"]);
      const json = req.headers["content-type"] === "application/json" ? JSON.parse(body) : null;
      state.posts.push({ path: p, body: json, key: req.headers["idempotency-key"] });
      if (state.failNext) { state.failNext = false; return fail(res, 500, "boom", "backend failed"); }
      if (state.staleNext) {
        state.staleNext = false;
        for (const d of Object.values(state.details)) { d.permissions = []; d.questions = []; }
        return fail(res, 409, "stale_request", "this request is no longer pending");
      }
      let m;
      if ((m = p.match(/^\/api\/permissions\/(.+)$/))) {
        for (const d of Object.values(state.details)) d.permissions = d.permissions.filter((x) => x.id !== decodeURIComponent(m[1]));
        return send(res, 200, { ok: true, decision: json.decision });
      }
      if ((m = p.match(/^\/api\/questions\/(.+)$/))) {
        for (const d of Object.values(state.details)) d.questions = d.questions.filter((x) => x.id !== decodeURIComponent(m[1]));
        return send(res, 200, { ok: true });
      }
      if (p.endsWith("/attachments")) {
        state.uploads++;
        const name = (body.match(/filename="([^"]+)"/) || [])[1] || "file";
        return send(res, 200, { attachment_id: "up-" + state.uploads, name, type: "text/plain", size: 2048, sha256: "x" });
      }
      if (p.endsWith("/prompt")) return send(res, 202, { accepted: true, queued: false });
      if (p.endsWith("/control")) return send(res, 200, { ok: true, action: json.action });
      if (p === "/api/logout") { state.authenticated = false; return send(res, 200, { revoked: "phone-1" }); }
      return fail(res, 404, "not_found", "unknown");
    }
    const name = p === "/" ? "index.html" : p.slice(1);
    if (!["index.html", "app.js", "app.css", "icon.svg", "manifest.webmanifest", "sw.js", "apple-touch-icon.png", "icon-192.png", "icon-512.png"].includes(name)) {
      res.writeHead(404); res.end(); return;
    }
    const types = { ".html": "text/html", ".js": "application/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png", ".webmanifest": "application/manifest+json" };
    res.setHeader("Content-Type", types[path.extname(name)]);
    res.end(await fs.readFile(path.join(__dirname, "../web", name)));
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const profile = options.device ? devices[options.device] : options.viewport ? { ...devices["iPhone 13"], viewport: options.viewport } : devices["iPhone 13"];
  const context = await browser.newContext({ ...profile, locale: options.locale || "tr-TR", timezoneId: options.timezoneId || "Europe/Istanbul", colorScheme: options.colorScheme || "light", serviceWorkers: "block", reducedMotion: "reduce" });
  await context.addInitScript(({ standalone, deniedStorage, theme }) => {
    Object.defineProperty(navigator, "standalone", { value: standalone });
    if (deniedStorage) Object.defineProperty(window, "localStorage", { get() { throw new DOMException("Unavailable", "SecurityError"); } });
    else if (theme) localStorage.setItem("atlas.theme", theme);
    window.testStreams = [];
    window.EventSource = class extends EventTarget {
      constructor(url) { super(); this.url = url; this.closed = false; this.readyState = 0; window.testStreams.push(this); setTimeout(() => { if (!this.closed) { this.readyState = 1; this.dispatchEvent(new Event("open")); } }, 0); }
      close() { this.closed = true; this.readyState = 2; }
      // fail simulates a stream the browser will not retry, such as a 503.
      fail() { this.readyState = 2; this.dispatchEvent(new Event("error")); }
      emit(type, data) { const e = new MessageEvent(type, { data: JSON.stringify(data) }); this.dispatchEvent(e); }
    };
    window.emit = (type, data) => { const s = window.testStreams[window.testStreams.length - 1]; s.emit(type, data); };
  }, { standalone: !!options.standalone, deniedStorage: !!options.deniedStorage, theme: options.theme || "" });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (err) => errors.push(err.message));
  const url = "http://127.0.0.1:" + server.address().port;
  t.after(async () => { await context.close(); await new Promise((resolve) => server.close(resolve)); assert.deepEqual(errors, []); });
  return { page, state, url };
}

module.exports = { fixture, themeQuestion, sampleData };
