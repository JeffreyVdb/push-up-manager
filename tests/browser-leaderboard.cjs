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
    // The board opens on the crew, and someone alone has nobody to rank.
    await page.locator('#crew-panel').waitFor({state:'visible'});
    await page.locator('#board-empty').waitFor({state:'visible'});
    assert.equal(await page.locator('#global-panel').isHidden(),true);
    assert.equal(await page.locator('#crew-race').isHidden(),true);
    await page.locator('#scope-everyone').click();
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

    // A crew: today's strip, week lanes built from days, the race view and its memory.
    const rival = await request.newContext();
    const rivalName = `rival_${Date.now()}`;
    const rivalUser = await (await rival.post(base+'/api/register',{data:{username:rivalName,password:'leaderboard-password'}})).json();
    const rivalTypes = await (await rival.get(base+'/api/types')).json();
    assert.equal((await add(rival,rivalUser.csrf_token,rivalTypes.types[0].id,400)).status(),201);
    await ctx.request.post(base+'/api/friends/requests',{headers:{'X-CSRF-Token':session.csrf_token},data:{username:rivalName}});
    const incoming = (await (await rival.get(base+'/api/friends')).json()).incoming;
    await rival.post(base+`/api/friends/requests/${incoming[0].id}/accept`,{headers:{'X-CSRF-Token':rivalUser.csrf_token}});
    await rival.dispose();
    await page.reload();
    await page.locator('#app').waitFor({state:'visible'});
    await page.locator('#nav-crew').click();
    await page.waitForFunction(()=>document.querySelectorAll('#lanes > li').length===2);
    assert.equal(await page.locator('#global-panel').isHidden(),true);
    assert.equal(await page.locator('#today-lead').textContent(),'400');
    assert.equal(await page.locator('#today-who').textContent(),`${rivalName} owns today.`);
    assert.equal(await page.locator('#today-gap').textContent(),'You have 325. 76 to pass.');
    const mine = page.locator('#lanes > li.is-me');
    assert.equal(await mine.locator('.lane-gap').textContent(),`76 to pass ${rivalName}`);
    assert.equal(await mine.locator('.seg.is-today').count(),1);
    assert.match(await mine.getAttribute('aria-label'),/^2\. board_\d+ \(you\), 325 push-ups this week, 75 behind/);
    assert.equal(await page.locator('#lanes > li').first().locator('.lb-who').textContent(),rivalName);
    await page.locator('#week-tab-race').click();
    await page.locator('#week-race').waitFor({state:'visible'});
    assert.equal(await page.locator('#week-lanes').isHidden(),true);
    assert.equal(await page.locator('#race-chart .race-line').count(),2);
    assert.equal(await page.locator('#race-standing').textContent(),`${rivalName} leads the week. 76 to pass ${rivalName}.`);
    assert.match(await page.locator('#race-chart').getAttribute('aria-label'),new RegExp(`${rivalName} 400: (.*, )?today 400\\. board_\\d+ \\(you\\) 325: .*today 325`));
    await page.reload();
    await page.locator('#app').waitFor({state:'visible'});
    await page.locator('#nav-crew').click();
    await page.locator('#race-chart .race-line').first().waitFor();
    assert.equal(await page.locator('#week-tab-race').getAttribute('aria-selected'),'true');
    await page.locator('#week-tab-race').press('ArrowLeft');
    assert.equal(await page.locator('#week-tab-lanes').getAttribute('aria-selected'),'true');
    await page.locator('#week-lanes').waitFor({state:'visible'});
    // Month and all time stay one tab away below the week.
    assert.equal(await page.locator('.long-periods .ptab.is-on').textContent(),'MONTH');
    assert.match(await page.locator('#board > li.is-me .lb-gap').textContent(),/^\+76 TO PASS$/);

    assert.deepEqual(errors,[]);
    console.log('Leaderboard: crew default, today strip, week lanes, race view memory, seven metrics, top ten, self marker, crew isolation, selection races, both themes, mobile and connection recovery passed.');
    await ctx.close();
  } finally { await browser.close(); }
})().catch(error=>{console.error(error);process.exit(1)});
