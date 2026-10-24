const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { test } = require('node:test');
const vm = require('node:vm');

function visit(storage = new Map(), blocked = false, dark = false) {
  const root = { dataset: {} };
  const meta = { 'color-scheme': {}, 'theme-color': {} };
  const controls = { 'classic-theme': {}, 'color-mode': {}, 'mode-note': {} };
  const icon = {};
  const listeners = {};
  const system = { matches: dark, addEventListener: (name, fn) => { listeners.system = fn; } };
  const window = { matchMedia: () => system, addEventListener: (name, fn) => { listeners[name] = fn; } };
  const document = {
    documentElement: root,
    querySelector: (selector) => selector.includes('color-scheme') ? meta['color-scheme']
      : selector.includes('theme-color') ? meta['theme-color'] : icon,
    getElementById: (id) => controls[id],
    addEventListener: (name, fn) => { listeners[name] = fn; },
  };
  const localStorage = {
    getItem(key) { if (blocked) throw new Error('Storage denied'); return storage.get(key) ?? null; },
    setItem(key, value) { if (blocked) throw new Error('Storage denied'); storage.set(key, value); },
  };
  vm.runInNewContext(readFileSync('web/theme.js', 'utf8'), { window, document, localStorage });
  return { root, meta, controls, icon, listeners, system, ...window.PushupTheme };
}

test('both themes follow the system on first visit, including saved legacy themes', () => {
  for (const theme of ['plant', 'classic']) for (const dark of [true, false]) {
    const app = visit(new Map([['pushup-counter:theme', theme]]), false, dark);
    assert.equal(app.root.dataset.theme, theme);
    assert.equal(app.root.dataset.mode, dark ? 'dark' : 'light');
    assert.equal(app.meta['color-scheme'].content, dark ? 'dark' : 'light');
    assert.equal(app.controls['color-mode'].value, 'system');
  }
  assert.equal(visit().root.dataset.theme, 'plant');
});

test('theme and mode persist independently and set browser chrome for all four palettes', () => {
  const storage = new Map();
  const app = visit(storage);
  const colors = { plant: { light: '#edf3e7', dark: '#111b15' }, classic: { light: '#f3f1ea', dark: '#070807' } };
  for (const theme of ['classic', 'plant']) for (const mode of ['light', 'dark']) {
    assert.equal(app.set(theme), true);
    assert.equal(app.setMode(mode), true);
    const next = visit(storage, false, mode !== 'dark');
    assert.equal(next.root.dataset.theme, theme);
    assert.equal(next.root.dataset.mode, mode);
    assert.equal(next.controls['color-mode'].value, mode);
    assert.equal(next.meta['theme-color'].content, colors[theme][mode]);
    assert.equal(next.controls['classic-theme'].checked, theme === 'classic');
    assert.equal(next.icon.href, theme === 'classic' ? '/icons/icon-192.png' : '/icons/plant-icon-192.png');
  }
});

test('live system changes apply only in System mode and resume when selected again', () => {
  const app = visit();
  app.system.matches = true; app.listeners.system();
  assert.equal(app.root.dataset.mode, 'dark');
  assert.match(app.controls['mode-note'].textContent, /Currently dark/);
  app.setMode('light'); app.listeners.system();
  assert.equal(app.root.dataset.mode, 'light');
  app.setMode('system');
  assert.equal(app.root.dataset.mode, 'dark');
  app.system.matches = false; app.listeners.system();
  assert.equal(app.root.dataset.mode, 'light');
});

test('denied storage allows both preferences for this visit, including DOM ready', () => {
  const app = visit(new Map(), true, true);
  assert.equal(app.root.dataset.mode, 'dark');
  assert.equal(app.set('classic'), false);
  assert.equal(app.setMode('light'), false);
  app.listeners.DOMContentLoaded();
  assert.equal(app.root.dataset.theme, 'classic');
  assert.equal(app.root.dataset.mode, 'light');
});

test('unknown preferences safely select plant and system', () => {
  const app = visit(new Map([['pushup-counter:theme', 'deleted-theme'], ['pushup-counter:mode', 'invalid']]), false, true);
  assert.equal(app.root.dataset.theme, 'plant');
  assert.equal(app.root.dataset.mode, 'dark');
  assert.equal(app.controls['color-mode'].value, 'system');
});

test('another tab updates either preference; removals and clearing restore defaults', () => {
  const app = visit(new Map(), false, true);
  app.listeners.storage({ key: 'pushup-counter:theme', newValue: 'classic' });
  app.listeners.storage({ key: 'pushup-counter:mode', newValue: 'light' });
  assert.equal(app.root.dataset.theme, 'classic');
  assert.equal(app.root.dataset.mode, 'light');
  app.listeners.storage({ key: 'pushup:lastType:someone', newValue: '12' });
  assert.equal(app.root.dataset.theme, 'classic');
  app.listeners.storage({ key: 'pushup-counter:mode', newValue: null });
  assert.equal(app.root.dataset.mode, 'dark');
  app.listeners.storage({ key: null, newValue: null });
  assert.equal(app.root.dataset.theme, 'plant');
  assert.equal(app.controls['classic-theme'].checked, false);
  assert.equal(app.controls['color-mode'].value, 'system');
});
