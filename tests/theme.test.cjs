const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { test } = require('node:test');
const vm = require('node:vm');

// Run the real pre-paint script across browser visits, including browsers
// that deny storage. Preference failure must never block the workout UI.
function visit(storage = new Map(), blocked = false) {
  const root = { dataset: {} };
  const meta = { 'color-scheme': {}, 'theme-color': {} };
  const toggle = { checked: false };
  const icon = {};
  const listeners = {};
  const window = { addEventListener: (name, fn) => { listeners[name] = fn; } };
  const document = {
    documentElement: root,
    querySelector: (selector) => selector.includes('color-scheme') ? meta['color-scheme']
      : selector.includes('theme-color') ? meta['theme-color'] : icon,
    getElementById: () => toggle,
    addEventListener: (name, fn) => { listeners[name] = fn; },
  };
  const localStorage = {
    getItem(key) { if (blocked) throw new Error('Storage denied'); return storage.get(key) ?? null; },
    setItem(key, value) { if (blocked) throw new Error('Storage denied'); storage.set(key, value); },
  };
  vm.runInNewContext(readFileSync('web/theme.js', 'utf8'), { window, document, localStorage });
  return { root, meta, toggle, icon, listeners, set: window.PushupTheme.set };
}

test('first visit defaults to plant, while classic survives a new page load', () => {
  const storage = new Map();
  const first = visit(storage);
  assert.equal(first.root.dataset.theme, 'plant');
  assert.equal(first.meta['color-scheme'].content, 'light');
  assert.equal(first.set('classic'), true);
  const next = visit(storage);
  assert.equal(next.root.dataset.theme, 'classic');
  assert.equal(next.meta['color-scheme'].content, 'dark');
  assert.equal(next.meta['theme-color'].content, '#070807');
  assert.equal(next.toggle.checked, true);
  assert.equal(next.icon.href, '/icons/icon-192.png');
  next.set('plant');
  assert.equal(visit(storage).root.dataset.theme, 'plant');
});

test('denied storage still allows changing the theme for the current visit', () => {
  const app = visit(new Map(), true);
  assert.equal(app.root.dataset.theme, 'plant');
  assert.equal(app.set('classic'), false);
  assert.equal(app.root.dataset.theme, 'classic');
  assert.equal(app.toggle.checked, true);
});

test('unknown preferences safely select the plant theme', () => {
  const app = visit(new Map([['pushup-counter:theme', 'deleted-theme']]));
  assert.equal(app.root.dataset.theme, 'plant');
  assert.equal(app.toggle.checked, false);
});

test('another tab updates the theme and switch; unrelated storage is ignored', () => {
  const app = visit();
  app.listeners.storage({ key: 'pushup-counter:theme', newValue: 'classic' });
  assert.equal(app.root.dataset.theme, 'classic');
  assert.equal(app.toggle.checked, true);
  app.listeners.storage({ key: 'pushup:lastType:someone', newValue: '12' });
  assert.equal(app.root.dataset.theme, 'classic');
  app.listeners.storage({ key: null, newValue: null });
  assert.equal(app.root.dataset.theme, 'plant');
  assert.equal(app.toggle.checked, false);
});
