// Run against a disposable server; creates an account to exercise real settings.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const base = process.env.PUSHUP_TEST_URL || 'http://localhost:5582';
const captures = process.env.PUSHUP_SCREENSHOTS;

(async () => {
  const browser = await chromium.launch({headless:true,...(process.env.CHROMIUM_PATH ? {executablePath:process.env.CHROMIUM_PATH}:{}),args:['--no-sandbox']});
  try {
    const context = await browser.newContext({viewport:{width:390,height:844},reducedMotion:'reduce'});
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.route('**/api/quotes', route => route.fulfill({json:{quotes:[]}}));
    await page.goto(base);
    const nav = page.locator('.settings-nav');
    const choose = async id => {
      if (await page.locator('#settings-back').isVisible()) await page.locator('#settings-back').click();
      await nav.locator(`[href="#${id}"]`).click();
      assert.equal(await page.locator('.settings-section:visible').count(), 1, 'only the chosen section is shown');
      assert.ok(await page.locator(`#${id}`).isVisible());
      const focused = await page.evaluate(() => document.activeElement.id);
      assert.equal(focused, page.viewportSize().width < 768 ? 'settings-title' : await page.locator(`#${id}`).getAttribute('aria-labelledby'));
    };
    const close = async () => {
      await page.locator('#settings-close').click();
      await page.waitForFunction(() => !history.state?.pushupSettings);
    };
    const checkLayout = async () => {
      const dialog = await page.locator('#settings').boundingBox();
      const close = await page.locator('#settings-close').boundingBox();
      assert.ok(close.y >= dialog.y && close.y + close.height <= dialog.y + dialog.height, 'close stays visible');
      assert.ok(await page.locator('#settings').evaluate(el => el.scrollWidth <= el.clientWidth), 'no horizontal dialog overflow');
      assert.ok(await page.locator('.settings-content').evaluate(el => el.scrollWidth <= el.clientWidth), 'no horizontal content overflow');
    };
    if (captures) fs.mkdirSync(captures,{recursive:true});
    const capture = async name => { if(captures) await page.screenshot({path:`${captures}/${name}.png`}); };

    await page.locator('#gate [data-open-settings]').click();
    assert.deepEqual(await nav.locator('a:visible strong').allTextContents(), ['Appearance','About']);
    assert.equal(await page.locator('.settings-section:visible').count(), 0);
    await choose('about-settings');
    assert.equal(await page.locator('#settings-title').textContent(), 'About');
    await page.keyboard.press('Tab');
    assert.equal(await page.evaluate(() => document.activeElement.id), 'settings-close');
    await page.keyboard.press('Tab');
    assert.ok(await page.evaluate(() => document.querySelector('#settings').contains(document.activeElement)), 'focus stays inside Settings');
    assert.ok(await page.locator('#whats-new .release-entry').first().isVisible());
    await page.goBack();
    await nav.waitFor({state:'visible'});
    assert.equal(await nav.locator('[href="#about-settings"]').evaluate(el => el === document.activeElement), true);
    await page.goBack();
    await page.locator('#settings').waitFor({state:'hidden'});
    assert.equal(await page.locator('#gate [data-open-settings]').evaluate(el => el === document.activeElement), true);

    await page.locator('[data-mode=register]').click();
    await page.locator('#auth-user').fill(`settings_${Date.now()}`);
    await page.locator('#auth-pass').fill('browser-settings-password');
    await page.locator('#auth-submit').click();
    await page.locator('#app').waitFor({state:'visible'});
    await page.locator('#email-prompt-settings').click();
    assert.ok(await page.locator('#account-settings').isVisible());
    assert.equal(await page.evaluate(() => document.activeElement.id), 'account-email');
    await close();
    await page.locator('#email-prompt-dismiss').click();
    await page.locator('[data-open-goals]').first().click();
    assert.ok(await page.locator('#goal-settings').isVisible());
    await page.locator('#goal-target').fill('125');
    await page.locator('#settings-back').click();
    await nav.waitFor({state:'visible'});
    await close();

    for (const theme of ['plant','classic']) for (const mode of ['light','dark']) {
      await page.evaluate(({theme,mode}) => { window.PushupTheme.set(theme); window.PushupTheme.setMode(mode); }, {theme,mode});
      for (const width of [1440,768,390,320]) {
        await page.setViewportSize({width,height:width === 320 ? 568 : 1000});
        await page.locator('#app [data-open-settings]').click();
        assert.deepEqual(await nav.locator('a:visible strong').allTextContents(), ['Appearance','Training','Account & security','About']);
        if (width >= 768) {
          assert.ok(await page.locator('#appearance-settings').isVisible());
          const dialog = await page.locator('#settings').boundingBox();
          assert.ok(dialog.width >= Math.min(928,width - 64) - 1, 'desktop uses a wide list/detail window');
        } else assert.equal(await page.locator('.settings-section:visible').count(), 0);
        for (const link of await nav.locator('a:visible').all()) {
          const rect = await link.boundingBox();
          assert.ok(rect.width >= 48 && rect.height >= 48);
        }
        await checkLayout();
        if ([1440,390].includes(width)) await capture(`${theme}-${mode}-${width}-overview`);
        await choose('goal-settings');
        await page.locator('#goal-target').fill('175');
        if ([1440,390].includes(width)) await capture(`${theme}-${mode}-${width}-training`);
        await choose('account-settings');
        await page.locator('#account-email').fill('draft@example.invalid');
        await page.locator('#account-password').fill('draft-password');
        await page.locator('.settings-content').evaluate(el => { el.scrollTop = el.scrollHeight; });
        await checkLayout();
        await choose('about-settings');
        await page.locator('.settings-content').evaluate(el => { el.scrollTop = el.scrollHeight; });
        await checkLayout();
        await choose('account-settings');
        assert.equal(await page.locator('#account-email').inputValue(), 'draft@example.invalid');
        assert.equal(await page.locator('#account-password').inputValue(), 'draft-password', 'navigation preserves password draft');
        if ([1440,390].includes(width)) await capture(`${theme}-${mode}-${width}-account`);
        await choose('goal-settings');
        assert.equal(await page.locator('#goal-target').inputValue(), '175', 'navigation preserves goal drafts');
        await close();
        assert.equal(await page.locator('#account-password').inputValue(), '', 'closing clears passwords');
      }
    }

    await page.setViewportSize({width:390,height:844});
    await page.evaluate(() => { document.documentElement.style.fontSize = '200%'; });
    await page.locator('#app [data-open-settings]').click();
    await choose('account-settings');
    await checkLayout();
    await capture('text-zoom');
    await close();
    await page.evaluate(() => { document.documentElement.style.fontSize = ''; });

    // Resizing preserves the active form and gives the phone a path to the list.
    await page.setViewportSize({width:1440,height:1000});
    await page.locator('#app [data-open-settings]').click();
    await choose('account-settings');
    await page.locator('#passkey-name').fill('Draft device');
    assert.ok(await page.locator('.settings-content').evaluate(el => el.scrollTop > 0));
    await page.setViewportSize({width:390,height:844});
    await page.waitForFunction(() => history.state?.pushupSettings?.depth === 2);
    assert.equal(await page.evaluate(() => document.activeElement.id), 'passkey-name');
    const field = await page.locator('#passkey-name').boundingBox();
    const pane = await page.locator('.settings-content').boundingBox();
    assert.ok(field.y >= pane.y && field.y + field.height <= pane.y + pane.height, 'focused input stays in view after reflow');
    await page.goBack();
    await nav.waitFor({state:'visible'});
    await choose('goal-settings');
    await page.setViewportSize({width:1440,height:1000});
    assert.ok(await page.locator('#goal-settings').isVisible());
    assert.ok(await nav.isVisible());
    await close();

    await page.locator('#nav-crew').click();
    await page.locator('#app [data-open-settings]').click();
    await choose('about-settings');
    assert.equal(new URL(page.url()).hash, '#crew');
    await close();
    assert.equal(new URL(page.url()).hash, '#crew');
    await page.evaluate(() => navigator.serviceWorker.ready);
    await context.setOffline(true);
    await page.reload();
    await page.locator('#app').waitFor({state:'visible'});
    await page.locator('#app [data-open-settings]').click();
    await choose('goal-settings');
    await choose('about-settings');
    assert.match(await page.locator('#whats-new').textContent(), /Find your settings faster/);
    await close();
    await context.setOffline(false);
    await page.locator('#logout').click();
    await page.locator('#gate').waitFor({state:'visible'});
    await page.locator('#gate [data-open-settings]').click();
    assert.deepEqual(await nav.locator('a:visible strong').allTextContents(), ['Appearance','About']);
    assert.deepEqual(errors, []);
    console.log('Mobile list/detail and Back, desktop sidebar, focus, shortcuts, drafts, zoom, themes, routes, sign-out and offline settings passed.');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exit(1); });
