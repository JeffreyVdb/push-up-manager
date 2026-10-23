/* Push-Up Counter service worker.
   Shell is cache-first. Account data and the outbox live in IndexedDB;
   private API responses are never stored in service-worker caches. The build ID is substituted by the Go server at startup, so a
   changed frontend always produces new cache names. */
"use strict";

const VERSION = "__BUILD__";
const SHELL = `pushup-shell-${VERSION}`;
const RUNTIME = `pushup-runtime-${VERSION}`;

const PRECACHE = [
  "/",
  "/app.css?v=__BUILD__",
  "/garden.css?v=__BUILD__",
  "/theme.js?v=__BUILD__",
  "/offline.js?v=__BUILD__",
  "/account.js?v=__BUILD__",
  "/passkeys.js?v=__BUILD__",
  "/app.js?v=__BUILD__",
  "/fonts/bigshoulders.woff2?v=__BUILD__",
  "/fonts/archivo.woff2?v=__BUILD__",
  // Stylesheets request these unversioned URLs, including on the first
  // offline visit to a theme that has not previously been displayed.
  "/fonts/bigshoulders.woff2",
  "/fonts/archivo.woff2",
  "/fonts/fraunces.woff2",
  "/manifest.webmanifest",
  "/icons/icon-192.png",
  "/icons/icon-512.png",
  "/icons/icon-512-maskable.png",
  "/icons/plant-icon-192.png",
  "/icons/plant-icon-512.png",
  "/icons/plant-icon-512-maskable.png",
  "/icons/plant-apple-touch-icon-180.png",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(SHELL).then((cache) => cache.addAll(PRECACHE))
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    const keep = new Set([SHELL, RUNTIME]);
    const keys = await caches.keys();
    await Promise.all(keys.filter((k) => k.startsWith("pushup-") && !keep.has(k)).map((k) => caches.delete(k)));
    await self.clients.claim();
  })());
});

self.addEventListener("message", (event) => {
  if (event.data && event.data.type === "SKIP_WAITING") self.skipWaiting();
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  // Never touch mutations: they must reach the server or fail visibly.
  if (request.method !== "GET") return;

  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;

  // Never globally cache private data. An old account's cached response
  // must not be served after sign-out or an account switch.
  if (url.pathname.startsWith("/api/")) return;

  if (request.mode === "navigate") {
    event.respondWith(shellFirst(request));
    return;
  }
  event.respondWith(cacheFirst(request));
});

async function shellFirst(request) {
  const cached = await caches.match("/", { ignoreSearch: false });
  if (cached) return cached;
  try {
    return await fetch(request);
  } catch {
    return new Response("<h1>Offline</h1><p>Reconnect and reload.</p>", {
      status: 503,
      headers: { "Content-Type": "text/html; charset=utf-8" },
    });
  }
}

async function cacheFirst(request) {
  const cached = await caches.match(request);
  if (cached) return cached;
  const response = await fetch(request);
  if (response.ok) {
    const cache = await caches.open(RUNTIME);
    await cache.put(request, response.clone());
  }
  return response;
}
