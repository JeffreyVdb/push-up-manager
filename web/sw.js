/* Push-Up Counter service worker.
   Shell is cache-first, API GETs are network-first, mutations are never
   intercepted. The build ID is substituted by the Go server at startup, so a
   changed frontend always produces new cache names. */
"use strict";

const VERSION = "__BUILD__";
const SHELL = `pushup-shell-${VERSION}`;
const RUNTIME = `pushup-runtime-${VERSION}`;
const API = `pushup-api-${VERSION}`;

const PRECACHE = [
  "/",
  "/app.css?v=__BUILD__",
  "/app.js?v=__BUILD__",
  "/fonts/bigshoulders.woff2?v=__BUILD__",
  "/fonts/archivo.woff2?v=__BUILD__",
  "/manifest.webmanifest",
  "/icons/icon-192.png",
  "/icons/icon-512.png",
  "/icons/icon-512-maskable.png",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(SHELL).then((cache) => cache.addAll(PRECACHE))
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    const keep = new Set([SHELL, RUNTIME, API]);
    const keys = await caches.keys();
    await Promise.all(keys.filter((k) => !keep.has(k)).map((k) => caches.delete(k)));
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

  // The event stream never finishes, and Cache.put reads a whole body before
  // it resolves — networkFirst would hold the response forever and buffer the
  // clone without bound, so EventSource would never open. The session also
  // goes straight to network: a cached date must never masquerade as today.
  if (url.pathname === "/api/events" || url.pathname === "/api/session") return;

  if (url.pathname.startsWith("/api/")) {
    event.respondWith(networkFirst(request));
    return;
  }
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

async function networkFirst(request) {
  try {
    const response = await fetch(request);
    if (response.ok) {
      const cache = await caches.open(API);
      await cache.put(request, response.clone());
    }
    return response;
  } catch {
    const cached = await caches.match(request);
    if (cached) return cached;
    return new Response(JSON.stringify({ error: "You are offline." }), {
      status: 503,
      headers: { "Content-Type": "application/json; charset=utf-8" },
    });
  }
}
