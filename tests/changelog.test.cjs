const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { test } = require('node:test');
const vm = require('node:vm');

function visit(storage = new Map(), blocked = false, latest = 'release-3') {
  const listeners = {};
  const notes = { querySelector: () => ({ dataset: { release: latest } }) };
  const badge = {};
  const button = { toggleAttribute(name, value) { this[name] = value; }, setAttribute(name, value) { this[name] = value; } };
  const document = { getElementById: id => id === 'whats-new' ? notes : badge, querySelectorAll: () => [button] };
  const localStorage = {
    getItem(key) { if (blocked) throw Error(); return storage.get(key) ?? null; },
    setItem(key, value) { if (blocked) throw Error(); storage.set(key, value); },
  };
  const window = { addEventListener: (name, fn) => { listeners[name] = fn; } };
  vm.runInNewContext(readFileSync('web/changelog.js', 'utf8'), { document, localStorage, window });
  return { badge, button, listeners, openAbout: () => window.PushupChangelog.markRead() };
}

test('notes are new until opened, remain read on reload, and become new for a later release', () => {
  const storage = new Map();
  const app = visit(storage);
  assert.equal(app.badge.hidden, false);
  assert.equal(storage.size, 0, 'loading notes does not mark them read before About is visited');
  app.openAbout();
  assert.equal(app.badge.hidden, true);
  assert.equal(app.button['aria-label'], 'Settings');
  assert.equal(visit(storage).badge.hidden, true);
  assert.equal(visit(storage, false, 'release-5').badge.hidden, false, 'skipped updates still notify');
});

test('reading works without storage and synchronizes between tabs', () => {
  const blocked = visit(new Map(), true);
  blocked.openAbout();
  assert.equal(blocked.badge.hidden, true);
  const app = visit();
  app.listeners.storage({ key: 'unrelated', newValue: 'release-3' });
  assert.equal(app.badge.hidden, false);
  app.listeners.storage({ key: 'pushup-counter:release-read', newValue: 'release-3' });
  assert.equal(app.badge.hidden, true);
  app.listeners.storage({ key: null, newValue: null });
  assert.equal(app.badge.hidden, false);
});

test('release entries have unique stable IDs and dates, with newest dates first', () => {
  const source = readFileSync('web/changelog.html', 'utf8');
  const ids = [...source.matchAll(/data-release="([^"]+)"/g)].map(m => m[1]);
  const dates = [...source.matchAll(/datetime="(\d{4}-\d{2}-\d{2})"/g)].map(m => m[1]);
  assert.ok(ids.length > 0);
  assert.equal(new Set(ids).size, ids.length);
  assert.equal(ids.length, dates.length);
  assert.deepEqual(dates, [...dates].sort().reverse());
});
