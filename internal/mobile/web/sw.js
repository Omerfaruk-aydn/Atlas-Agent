"use strict";
// Caches only the app shell so the page opens and can show "disconnected".
// API responses are never cached: task state must always be live.
const SHELL = "atlas-shell-v2";
const FILES = ["/", "/app.js", "/app.css", "/icon.svg", "/apple-touch-icon.png", "/icon-192.png", "/icon-512.png", "/manifest.webmanifest"];
self.addEventListener("install", (e) => e.waitUntil(caches.open(SHELL).then((c) => c.addAll(FILES)).then(() => self.skipWaiting())));
self.addEventListener("activate", (e) => e.waitUntil(caches.keys().then((ks) => Promise.all(ks.filter((k) => k !== SHELL).map((k) => caches.delete(k)))).then(() => self.clients.claim())));
self.addEventListener("fetch", (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== "GET" || url.pathname.startsWith("/api/")) return;
  e.respondWith(fetch(e.request).catch(() => caches.match(e.request).then((r) => r || caches.match("/"))));
});
