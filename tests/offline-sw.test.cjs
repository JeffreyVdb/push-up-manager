const assert = require('node:assert/strict');
const { test } = require('node:test');
const { readFileSync } = require('node:fs');
const vm = require('node:vm');

test('service worker never intercepts private APIs or mutations', () => {
  const handlers = new Map();
  const context = vm.createContext({ URL, self: { location: { origin: 'https://counter.example' }, addEventListener: (name, fn) => handlers.set(name, fn) } });
  vm.runInContext(readFileSync('web/sw.js', 'utf8'), context);
  for (const path of ['/api/session', '/api/offline', '/api/day/2026-10-23', '/api/friends', '/api/events']) {
    let intercepted = false;
    handlers.get('fetch')({ request: { method: 'GET', url: `https://counter.example${path}` }, respondWith: () => { intercepted = true; } });
    assert.equal(intercepted, false, path);
  }
  let intercepted = false;
  handlers.get('fetch')({ request: { method: 'POST', url: 'https://counter.example/api/reps' }, respondWith: () => { intercepted = true; } });
  assert.equal(intercepted, false);
});

test('service worker upgrade deletes legacy globally cached account data', async () => {
  const handlers = new Map();
  const removed = [];
  const context = vm.createContext({
    self: { addEventListener: (name, fn) => handlers.set(name, fn), clients: { claim: async () => {} } },
    caches: { keys: async () => ['pushup-api-OLD', 'pushup-shell-OLD', 'another-app'], delete: async (key) => { removed.push(key); } },
  });
  vm.runInContext(readFileSync('web/sw.js', 'utf8'), context);
  let done;
  handlers.get('activate')({ waitUntil: (promise) => { done = promise; } });
  await done;
  assert.deepEqual(removed.sort(), ['pushup-api-OLD', 'pushup-shell-OLD']);
});

test('a new service worker caches a fresh shell and takes over without waiting', async () => {
  const handlers = new Map();
  const added = [];
  let skipped = false;
  const context = vm.createContext({
    Request: class { constructor(url, init) { this.url = url; this.cache = init?.cache; } },
    self: { addEventListener: (name, fn) => handlers.set(name, fn), skipWaiting: async () => { skipped = true; } },
    caches: { open: async () => ({ addAll: async (requests) => { added.push(...requests); } }) },
  });
  vm.runInContext(readFileSync('web/sw.js', 'utf8'), context);
  let done;
  handlers.get('install')({ waitUntil: (promise) => { done = promise; } });
  await done;
  assert.ok(added.some((request) => request.url === '/'));
  assert.ok(added.every((request) => request.cache === 'reload'), 'precache must bypass the HTTP cache');
  assert.equal(skipped, true);
});
