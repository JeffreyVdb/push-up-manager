// Run against a disposable server; creates an account and tests real touch input.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const base = process.env.PUSHUP_TEST_URL || 'http://localhost:5577';
const captures = process.env.PUSHUP_SCREENSHOTS;

(async () => {
  const browser = await chromium.launch({ headless: true,
    ...(process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {}) });
  try {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.route('**/api/quotes', route => route.fulfill({ json: { quotes: [] } }));
    await page.goto(base);
    await page.locator('[data-mode=register]').click();
    await page.locator('#auth-user').fill(`swipe_${Date.now()}`);
    await page.locator('#auth-pass').fill('calendar-test-password');
    await page.locator('#auth-submit').click();
    await page.locator('#app').waitFor({ state: 'visible' });
    await page.locator('#email-prompt-dismiss').click();
    await page.locator('#cal .day').first().waitFor();
    const cdp = await context.newCDPSession(page);
    const shape = () => page.locator('#cal-body').getAttribute('data-shape');
    const settled = () => page.waitForFunction(() => document.querySelector('#cal-body').dataset.shape === `${state.cal.year}-${state.cal.month}` &&
      !document.querySelector('.calendar-departing') &&
      !document.querySelector('#cal').getAnimations().length && !document.querySelector('#cal').style.transform);
    async function month(year, month) {
      await page.evaluate(async ({ year, month }) => { state.cal = { year, month }; await loadCalendar(); }, { year, month });
      await settled();
    }
    async function swipe(dx, dy = 0, { cancel = false, duration = 300, inspect = false } = {}) {
      await page.locator('#cal').scrollIntoViewIfNeeded();
      const box = await page.locator('#cal').boundingBox();
      const x = box.x + box.width / 2 - dx / 2;
      const y = Math.max(110, Math.min(650, box.y + box.height / 2));
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
      for (let i = 1; i <= 8; i++) {
        await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x + dx * i / 8, y: y + dy * i / 8 }] });
        await page.waitForTimeout(duration / 8);
      }
      if (inspect) {
        const offset = await page.locator('#cal').evaluate(el => new DOMMatrixReadOnly(getComputedStyle(el).transform).m41);
        assert.ok(Math.sign(offset) === Math.sign(dx) && Math.abs(offset) > 60, 'calendar follows the finger: ' + offset);
      }
      await cdp.send('Input.dispatchTouchEvent', { type: cancel ? 'touchCancel' : 'touchEnd', touchPoints: [] });
      await settled();
    }
    await month(2026, 1);
    const selected = await page.evaluate(() => state.selectedDay);
    await swipe(150, 4, { inspect: true });
    assert.equal(await shape(), '2025-12', 'right swipe shows previous year');
    assert.equal(await page.evaluate(() => state.selectedDay), selected, 'swipe does not select a day');
    await swipe(-150, 0, { inspect: true });
    assert.equal(await shape(), '2026-1', 'left swipe shows next year');
    await swipe(20);
    assert.equal(await shape(), '2026-1', 'short drag snaps back');
    await swipe(-45, 0, { duration: 40 });
    assert.equal(await shape(), '2026-2', 'short quick flick advances');
    await swipe(140, 0, { cancel: true });
    assert.equal(await shape(), '2026-2', 'cancelled touch leaves month unchanged');
    const scroll = await page.evaluate(() => scrollY);
    await swipe(4, -130);
    assert.equal(await shape(), '2026-2', 'vertical scroll does not change month');
    assert.ok(await page.evaluate(() => scrollY) > scroll + 30, 'vertical page scrolling stays native');
    await page.locator('#cal .day').first().tap();
    await page.waitForFunction(() => state.selectedDay === '2026-02-01');
    assert.equal(await page.evaluate(() => state.selectedDay), '2026-02-01', 'ordinary tap still selects date');
    await page.locator('#cal-next').focus();
    await page.keyboard.press('Enter');
    await settled();
    assert.equal(await shape(), '2026-3', 'keyboard month navigation remains available');
    await page.evaluate(async () => { await Promise.all([shiftMonth(1), shiftMonth(1), shiftMonth(-1)]); });
    await settled();
    assert.equal(await shape(), '2026-4', 'rapid navigation keeps latest request');
    // The cached calendar must remain swipeable offline, including after reload.
    await page.evaluate(() => navigator.serviceWorker.ready);
    await context.setOffline(true);
    await page.reload();
    await page.locator('#cal .day').first().waitFor();
    await month(2026, 1);
    await swipe(-150);
    assert.equal(await shape(), '2026-2');
    await context.setOffline(false);
    // Exercise the uncached failure path and retry without a stuck translation.
    await page.evaluate(() => {
      window.savedCalendarAPI = api;
      api = async (method, path, body) => {
        if (path.startsWith('/api/calendar')) throw new Error('Calendar unavailable');
        return window.savedCalendarAPI(method, path, body);
      };
    });
    await swipe(-150);
    assert.equal(await shape(), '2026-2');
    assert.equal(await page.evaluate(() => state.cal.month), 2, 'failed request restores navigation state');
    await page.evaluate(() => { api = window.savedCalendarAPI; });
    await swipe(-150);
    assert.equal(await shape(), '2026-3', 'retry works after failure');
    await page.locator('#toast').waitFor({ state: 'hidden' });
    if (captures) fs.mkdirSync(captures, { recursive: true });
    for (const theme of ['plant', 'classic']) {
      await page.evaluate(theme => { document.documentElement.dataset.theme = theme; }, theme);
      await swipe(150);
      if (captures) await page.screenshot({ path: `${captures}/${theme}-phone.png`, fullPage: true });
    }
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await month(2026, 1);
    await swipe(150);
    assert.equal(await shape(), '2025-12', 'reduced motion retains navigation');
    assert.equal(await page.locator('#cal').evaluate(el => el.getAnimations().length), 0);
    assert.equal(await page.locator('.calendar-departing').count(), 0);
    await page.setViewportSize({ width: 320, height: 760 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'no narrow-phone horizontal overflow');
    // Desktop drag uses the same behavior; arrows remain the visible affordance.
    const desktop = await browser.newPage({ viewport: { width: 1440, height: 1000 }, storageState: await context.storageState() });
    desktop.on('pageerror', e => errors.push(e.message));
    await desktop.goto(base);
    await desktop.locator('#cal .day').first().waitFor();
    await desktop.evaluate(async () => { state.cal = { year: 2026, month: 1 }; await loadCalendar(); });
    await desktop.waitForTimeout(400);
    for (const theme of ['plant', 'classic']) {
      await desktop.evaluate(theme => { document.documentElement.dataset.theme = theme; }, theme);
      const aligned = await desktop.evaluate(async () => {
        const before = document.querySelector('#cal').getBoundingClientRect();
        await shiftMonth(1);
        const copy = document.querySelector('.calendar-departing');
        const animation = copy.getAnimations()[0];
        animation.pause(); animation.currentTime = 0;
        const after = copy.getBoundingClientRect();
        animation.play();
        return Math.abs(before.x - after.x) < 1 && Math.abs(before.width - after.width) < 1;
      });
      assert.ok(aligned, 'outgoing month stays aligned in both themes');
      await desktop.evaluate(async () => { await shiftMonth(-1); });
      await desktop.waitForTimeout(400);
      const box = await desktop.locator('#cal').boundingBox();
      await desktop.mouse.move(box.x + box.width * .25, box.y + 100);
      await desktop.mouse.down();
      await desktop.mouse.move(box.x + box.width * .75, box.y + 100, { steps: 10 });
      await desktop.mouse.up();
      await desktop.waitForTimeout(400);
      if (captures) await desktop.screenshot({ path: `${captures}/${theme}-desktop.png`, fullPage: true });
    }
    assert.equal(await desktop.locator('#cal-body').getAttribute('data-shape'), '2025-11');
    assert.deepEqual(errors, []);
    console.log('Calendar touch/drag, directions, year boundaries, flick, cancellation, scrolling, taps, keyboard, rapid input, offline, failure recovery, themes and reduced motion passed.');
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exit(1); });
