// Run against a disposable server; creates synthetic accounts and records.
const { chromium, request } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const base = process.env.PUSHUP_TEST_URL || 'http://localhost:5578';
const captures = process.env.PUSHUP_SCREENSHOTS;
(async () => {
  const browser = await chromium.launch({headless:true,...(process.env.CHROMIUM_PATH ? {executablePath:process.env.CHROMIUM_PATH}:{}),args:['--no-sandbox']});
  try {
    const ctx = await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});
    const page = await ctx.newPage();
    const errors = []; page.on('pageerror', e => errors.push(e.message));
    await page.route('**/api/quotes', r => r.fulfill({json:{quotes:[]}}));
    await page.goto(base);
    await page.locator('[data-mode=register]').click();
    const name = `board_${Date.now()}`;
    await page.locator('#auth-user').fill(name);
    await page.locator('#auth-pass').fill('leaderboard-password');
    await page.locator('#auth-submit').click();
    await page.locator('#app').waitFor({state:'visible'});
    await page.locator('#email-prompt-dismiss').click();
    await page.locator('#nav-crew').click();
    await page.waitForFunction(()=>document.querySelector('#leaderboard-status').textContent.startsWith('No reps'));
    const session = await (await ctx.request.get(base+'/api/session')).json();
    const types = await (await ctx.request.get(base+'/api/types')).json();
    const add = (api, csrf, type, count) => api.post(base+'/api/reps',{headers:{'X-CSRF-Token':csrf},data:{day:session.today,type_id:type,count}});
    await add(ctx.request,session.csrf_token,types.types[0].id,300);
    await add(ctx.request,session.csrf_token,types.types[1].id,25);
    for(let i=0;i<11;i++) {
      const api = await request.newContext();
      const registration = await api.post(base+'/api/register',{data:{username:`athlete_${String(i).padStart(2,'0')}_${Date.now()}`,password:'leaderboard-password'}});
      assert.equal(registration.status(),201);
      const user = await registration.json();
      const t = await (await api.get(base+'/api/types')).json();
      assert.equal((await add(api,user.csrf_token,t.types[0].id,200-i*10)).status(),201);
      await api.dispose();
    }
    await page.locator('#scope-crew').click();
    await page.locator('#crew-panel').waitFor({state:'visible'});
    await page.locator('#board-empty').waitFor({state:'visible'});
    await page.locator('#scope-everyone').click();
    await page.waitForFunction(()=>document.querySelectorAll('#global-board > li').length===10);
    assert.equal(await page.locator('#global-board .lb-you:not([hidden])').count(),1);
    for(const metric of ['best_year','this_year','variations_day','best_month','this_month','average_day','best_day']) {
      await page.locator('#leaderboard-metric').selectOption(metric);
      await page.waitForFunction(()=>document.querySelector('#global-board').getAttribute('aria-busy')==='false');
      assert.equal(await page.locator('#global-board > li').count(),10);
      assert.equal(await page.locator('#global-board .global-score').first().textContent(),metric==='variations_day'?'2':'325');
    }
    // Stale responses from a previous selection must not replace the current category.
    await page.route('**/api/leaderboard?metric=best_year', async r=>{await new Promise(resolve=>setTimeout(resolve,400));await r.continue();});
    await page.locator('#leaderboard-metric').selectOption('best_year');
    await page.locator('#leaderboard-metric').selectOption('variations_day');
    await page.waitForFunction(()=>document.querySelector('#global-board .global-score')?.textContent==='2');
    await page.waitForTimeout(500);
    assert.equal(await page.locator('#global-board .global-score').first().textContent(),'2');
    await page.locator('#leaderboard-metric').selectOption('best_day');
    await page.waitForFunction(()=>document.querySelector('#global-board .global-score')?.textContent==='325');
    if(captures) {
      fs.mkdirSync(captures,{recursive:true});
      for(const theme of ['plant','classic']) {
        await page.evaluate(theme=>window.PushupTheme.set(theme),theme);
        for(const [label,width,height] of [['desktop',1440,1000],['mobile',390,844],['narrow',320,760]]) {
          await page.setViewportSize({width,height});
          await page.waitForTimeout(450); // Settle the dock-to-phone-sheet transition.
          await page.evaluate(()=>window.scrollTo(0,0));
          assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),`${theme} ${width}: no overflow`);
          await page.screenshot({path:`${captures}/${theme}-${label}.png`,fullPage:true});
        }
      }
    }
    await ctx.setOffline(true);
    await page.locator('#scope-crew').click(); await page.locator('#scope-everyone').click();
    await page.waitForFunction(()=>document.querySelector('#leaderboard-status').textContent.includes('out of date'));
    assert.equal(await page.locator('#global-board > li').count(),10);
    await page.locator('#leaderboard-metric').selectOption('this_month');
    await page.waitForFunction(()=>document.querySelector('#leaderboard-status').textContent.includes('need a connection'));
    assert.equal(await page.locator('#global-board > li').count(),0);
    await ctx.setOffline(false);
    await page.locator('#leaderboard-retry').click();
    await page.waitForFunction(()=>document.querySelectorAll('#global-board > li').length===10);
    assert.deepEqual(errors,[]);
    console.log('Leaderboard: seven metrics, top ten, self marker, crew isolation, selection races, both themes, mobile and connection recovery passed.');
    await ctx.close();
  } finally { await browser.close(); }
})().catch(error=>{console.error(error);process.exit(1)});
