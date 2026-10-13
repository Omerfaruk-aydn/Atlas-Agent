"use strict";
const assert = require("node:assert/strict");
const http = require("node:http");
const fs = require("node:fs/promises");
const path = require("node:path");
const { devices } = require("playwright");
async function fixture(browser, t, options = {}) {
  const state = { authenticated: true, unavailable: false, unavailableAt: [], meReads: 0, posts: [], csrf: "csrf-1", question: true, questionID: "q1" };
  const server = http.createServer(async (req, res) => {
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
