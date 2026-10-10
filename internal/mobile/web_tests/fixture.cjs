"use strict";
const assert = require("node:assert/strict");
const http = require("node:http");
const fs = require("node:fs/promises");
const path = require("node:path");
const { devices } = require("playwright");
async function fixture(browser, t, options = {}) {
  const state = { authenticated: true, unavailable: false, unavailableAt: [], meReads: 0, posts: [], csrf: "csrf-1", question: true, questionID: "q1" };
  const server = http.createServer(async (req, res) => {
    if (req.url.startsWith("/api/")) {
      res.setHeader("Content-Type", "application/json");
      if (!state.authenticated) { res.writeHead(401); res.end('{"error":{"message":"expired"}}'); return; }
      if (state.unavailable) { state.meReads++; state.unavailableAt.push(Date.now()); res.writeHead(503); res.end('{"error":{"message":"Atlas is offline"}}'); return; }
      const session = { id: "s1", title: "iPhone test", created_at: 1, updated_at: 2, busy: true };
      if (req.url === "/api/me") {
        state.meReads++;
        res.end(JSON.stringify({ device: { name: "My iPhone", previews: false }, language: "tr", csrf: state.csrf, seq: 1, fingerprint: "fixture" }));
      } else if (req.url === "/api/sessions") {
        res.end(JSON.stringify({ sessions: [session], seq: 1 }));
      } else if (req.url === "/api/sessions/s1") {
        res.end(JSON.stringify({ session, workflow: { available: true, revision: "r1" }, messages: [], permissions: [],
          questions: state.question ? [{ id: state.questionID, session_id: "s1", questions: [{ id: "item1", type: "yes_no", question: "Devam edelim mi?" }] }] : [], seq: 1 }));
      } else if (req.method === "POST") {
        assert.equal(req.headers["x-csrf-token"], state.csrf);
        assert.ok(req.headers["idempotency-key"]);
        let body = "";
        for await (const chunk of req) body += chunk;
        state.posts.push({ path: req.url, body: JSON.parse(body) });
        state.question = false;
        res.end('{"ok":true}');
      } else { res.writeHead(404); res.end("{}"); }
      return;
    }
    const name = req.url === "/" ? "index.html" : req.url.slice(1);
    if (!["index.html", "app.js", "app.css", "icon.svg", "manifest.webmanifest", "sw.js", "apple-touch-icon.png", "icon-192.png", "icon-512.png"].includes(name)) {
      res.writeHead(404); res.end(); return;
    }
    const types = { ".html": "text/html", ".js": "application/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png", ".webmanifest": "application/manifest+json" };
    res.setHeader("Content-Type", types[path.extname(name)]);
    res.end(await fs.readFile(path.join(__dirname, "../web", name)));
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const context = await browser.newContext({ ...devices[options.device || "iPhone 13"], locale: "tr-TR", serviceWorkers: "block" });
  await context.addInitScript(({ standalone, deniedStorage }) => {
    Object.defineProperty(navigator, "standalone", { value: standalone });
    if (deniedStorage) Object.defineProperty(window, "localStorage", { get() { throw new DOMException("Unavailable", "SecurityError"); } });
    window.testStreams = [];
    window.EventSource = class extends EventTarget {
      constructor(url) { super(); this.url = url; this.closed = false; this.readyState = 0; window.testStreams.push(this); setTimeout(() => { this.readyState = 1; this.dispatchEvent(new Event("open")); }, 0); }
      close() { this.closed = true; this.readyState = 2; }
      // fail simulates a stream the browser will not retry, such as a 503.
      fail() { this.readyState = 2; this.dispatchEvent(new Event("error")); }
    };
  }, { standalone: !!options.standalone, deniedStorage: !!options.deniedStorage });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (err) => errors.push(err.message));
  const url = "http://127.0.0.1:" + server.address().port;
  t.after(async () => { await context.close(); await new Promise((resolve) => server.close(resolve)); assert.deepEqual(errors, []); });
  return { page, state, url };
}

module.exports = { fixture };
