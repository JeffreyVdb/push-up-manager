// Run against a disposable server, like browser-flows.cjs.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const base = process.env.PUSHUP_TEST_URL || 'http://localhost:5574';

(async () => {
  const browser = await chromium.launch({
    ...(process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {}),
    headless: true, args: ['--no-sandbox'],
  });
  try {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    // Hold API requests without changing navigator.onLine or the service worker.
    // This models a connection that stalls before the browser reports offline.
    await context.addInitScript(() => {
      sessionStorage.pageLoads = String(Number(sessionStorage.pageLoads || 0) + 1);
      const fetch = window.fetch.bind(window);
      window.fetch = (...args) => {
        if (sessionStorage.holdAPI === 'yes' && String(args[0]).startsWith('/api/')) {
          return new Promise((resolve, reject) => {
            args[1]?.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')));
          });
        }
        return fetch(...args);
      };
    });
    const page = await context.newPage();
    const capture = async name => {
      if (!process.env.PUSHUP_SCREENSHOTS) return;
      fs.mkdirSync(process.env.PUSHUP_SCREENSHOTS, { recursive: true });
      await page.screenshot({ path: `${process.env.PUSHUP_SCREENSHOTS}/${name}.png` });
    };
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(base);
    await page.evaluate(() => navigator.serviceWorker.ready);
    await page.waitForFunction(() => navigator.serviceWorker.controller);
    assert.equal(await page.evaluate(() => sessionStorage.pageLoads), '1', 'first worker activation must not reload or interrupt sign-in');
    await page.reload();
    await page.locator('#gate').waitFor({ state: 'visible' });
    await page.evaluate(() => { sessionStorage.holdAPI = 'yes'; });
    await page.reload({ waitUntil: 'domcontentloaded' });
    const startupVisible = await page.locator('#startup').isVisible();
    console.log('Visible surface while first session check stalls:', startupVisible);
    await capture('startup-mobile');
    await page.setViewportSize({ width: 1440, height: 1000 });
    await capture('startup-desktop');
    await page.setViewportSize({ width: 390, height: 844 });

    await page.evaluate(() => { sessionStorage.removeItem('holdAPI'); });
    await page.reload();
    await page.locator('#gate').waitFor({ state: 'visible' });
    await page.locator('[data-mode="register"]').click();
    await page.locator('#auth-user').fill('startup_' + Date.now());
    await page.locator('#auth-pass').fill('startup-test-password');
    await page.locator('#auth-submit').click();
    await page.locator('#app').waitFor({ state: 'visible' });
    await page.waitForFunction(() => !document.querySelector('#app').hidden && document.querySelector('#type-trigger')?.textContent.trim());
    await page.evaluate(() => navigator.serviceWorker.ready);
    await page.reload();
    await page.waitForFunction(() => !document.querySelector('#app').hidden && document.querySelector('#type-trigger')?.textContent.trim());
    await page.evaluate(() => { sessionStorage.holdAPI = 'yes'; });
    await page.reload({ waitUntil: 'domcontentloaded' });
    let restored = true;
    try {
      await page.waitForFunction(() => !document.querySelector('#app').hidden && document.querySelector('#type-trigger')?.textContent.trim(), null, { timeout: 1000 });
    } catch { restored = false; }
    console.log('Saved application usable before any network response:', restored);
    assert.ok(startupVisible, 'first visit must show a visible startup surface during a stalled session check');
    assert.ok(restored, 'cached log must render without waiting for the network timeout');

    await page.evaluate(() => { sessionStorage.removeItem('holdAPI'); });
    await context.setOffline(true);
    await page.reload();
    await page.waitForFunction(() => !document.querySelector('#app').hidden && document.querySelector('#type-trigger')?.textContent.trim(), null, { timeout: 1000 });
    await page.locator('#fab').click();
    await page.locator('#reps').fill('13');
    await page.locator('#log-submit').click();
    await page.waitForFunction(() => document.querySelector('#sync-status').textContent.includes('1 saved set'));
    await page.reload();
    await page.waitForFunction(() => document.querySelector('#day-entries').textContent.includes('13'), null, { timeout: 1000 });
    assert.match(await page.locator('#sync-status').innerText(), /Offline/);
    await capture('offline-mobile');
    console.log('Airplane-mode reload and durable logging passed');

    // Cached-first startup must still respect session expiry and explicit logout.
    await context.clearCookies();
    await context.setOffline(false);
    await page.reload();
    await page.waitForFunction(() => document.querySelector('#sync-status').textContent.includes('Sign in to this account again'));
    assert.equal(await page.locator('#app').isVisible(), true);
    assert.match(await page.locator('#day-entries').innerText(), /13/);
    await context.setOffline(true);
    await page.locator('#logout').click();
    await page.locator('#gate').waitFor({ state: 'visible' });
    await page.reload();
    await page.locator('#gate').waitFor({ state: 'visible' });
    assert.equal(await page.locator('#app').isVisible(), false);
    assert.equal(await page.locator('#startup').isVisible(), false);
    assert.deepEqual(errors, []);
    console.log('Expired session retains saved sets; offline sign-out stays signed out');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exit(1); });
