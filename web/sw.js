/* Push-Up Counter service worker.
   Shell is cache-first. Account data and the outbox live in IndexedDB;
   private API responses are never stored in service-worker caches. The build ID is substituted by the Go server at startup, so a
   changed frontend always produces new cache names.
   A new worker takes over as soon as its complete shell is cached. Waiting
   for every window to close never happens in an installed app that stays in
   the background, so an open page offers a refresh instead (see app.js). */
"use strict";

const VERSION = "__BUILD__";
const SHELL = `pushup-shell-${VERSION}`;
const RUNTIME = `pushup-runtime-${VERSION}`;

const PRECACHE = [
  "/",
  "/app.css?v=__BUILD__",
  "/garden.css?v=__BUILD__",
  "/appearance.css?v=__BUILD__",
  "/changelog.js?v=__BUILD__",
  "/confirm.js?v=__BUILD__",
  "/sheet.js?v=__BUILD__",
  "/theme.js?v=__BUILD__",
  "/offline.js?v=__BUILD__",
  "/quotes.js?v=__BUILD__",
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
  event.waitUntil((async () => {
    const cache = await caches.open(SHELL);
    // Bypass the HTTP cache: unversioned icons and fonts may be an hour old,
    // and the shell must be exactly this build.
    await cache.addAll(PRECACHE.map((url) => new Request(url, { cache: "reload" })));
    await self.skipWaiting();
  })());
});

self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    const keep = new Set([SHELL, RUNTIME]);
    const keys = await caches.keys();
    await Promise.all(keys.filter((k) => k.startsWith("pushup-") && !keep.has(k)).map((k) => caches.delete(k)));
    await self.clients.claim();
  })());
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
  // This worker's own shell first: its assets are guaranteed to be cached
  // beside it, so an offline start never waits on a missing stylesheet.
  const cached = await (await caches.open(SHELL)).match("/") || await caches.match("/");
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
  const cached = await (await caches.open(SHELL)).match(request) || await caches.match(request);
  if (cached) return cached;
  const response = await fetch(request);
  if (response.ok) {
    const cache = await caches.open(RUNTIME);
    await cache.put(request, response.clone());
  }
  return response;
}
