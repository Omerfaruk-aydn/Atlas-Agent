"use strict";
// Caches only the app shell so the page opens and can show "disconnected".
// API responses, credential cards, questions, permissions, chat images and
// the live screen stream are never cached: everything under /api/ goes
// straight to the network, so an offline start never shows old data as
// live. The shell is fetched from the network first and cached as one
// versioned set, so an offline start never mixes new HTML with older CSS
// or JavaScript.
const SHELL = "atlas-shell-v6";
const FILES = ["/", "/app.js", "/app.css", "/icon.svg", "/apple-touch-icon.png", "/icon-192.png", "/icon-512.png", "/manifest.webmanifest"];
self.addEventListener("install", (e) => e.waitUntil(caches.open(SHELL).then((c) => c.addAll(FILES.map((f) => new Request(f, { cache: "reload" })))).then(() => self.skipWaiting())));
self.addEventListener("activate", (e) => e.waitUntil(caches.keys().then((ks) => Promise.all(ks.filter((k) => k !== SHELL).map((k) => caches.delete(k)))).then(() => self.clients.claim())));
self.addEventListener("fetch", (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== "GET" || url.origin !== self.location.origin || url.pathname.startsWith("/api/")) return;
  const path = url.pathname === "/index.html" ? "/" : url.pathname;
  if (!FILES.includes(path)) return;
  e.respondWith(fetch(e.request, { cache: "no-cache" }).then((r) => {
    // Keep the offline copy current with what the network just served.
    if (r.ok) { const copy = r.clone(); caches.open(SHELL).then((c) => c.put(path, copy)).catch(() => {}); }
    return r;
  }).catch(() => caches.open(SHELL).then((c) => c.match(path)).then((r) => r || caches.match("/"))));
});
