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
