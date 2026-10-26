// Run against a disposable server. Creates one account and exercise entries.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const base = process.env.PUSHUP_TEST_URL || 'http://localhost:5575';
const captures = process.env.PUSHUP_SCREENSHOTS;
(async () => {
  const browser = await chromium.launch({headless:true,...(process.env.CHROMIUM_PATH ? {executablePath:process.env.CHROMIUM_PATH}:{}),args:['--no-sandbox']});
  try {
    const ctx = await browser.newContext({viewport:{width:1440,height:1000}});
    const page = await ctx.newPage();
    const errors = []; page.on('pageerror', e=>errors.push(e.message));
    // Quote providers must not make the browser test dependent on the internet.
    await page.route('**/api/quotes', r=>r.fulfill({json:{quotes:[]}}));
    await page.addInitScript(() => {
      window.goalAnimations=[];
      const animate=Element.prototype.animate;
      Element.prototype.animate=function(...args){if(this.id==='goal-check') window.goalAnimations.push(this.id);return animate.apply(this,args)};
    });
    await page.goto(base);
    await page.locator('[data-mode=register]').click();
    await page.locator('#auth-user').fill(`goals_${Date.now()}`);
    await page.locator('#auth-pass').fill('browser-goals-password');
    await page.locator('#auth-submit').click();
    await page.locator('#app').waitFor({state:'visible'});
    await page.locator('#email-prompt-dismiss').click();
    await page.waitForFunction(()=>document.querySelectorAll('.day').length>0);
    const session = await (await ctx.request.get(base+'/api/session')).json();
    const types = await (await ctx.request.get(base+'/api/types')).json();
    const yesterday = new Date(Date.parse(`${session.today}T12:00:00Z`)-86400000).toISOString().slice(0,10);
    await ctx.request.post(base+'/api/reps',{headers:{'X-CSRF-Token':session.csrf_token},data:{day:yesterday,type_id:types.types[0].id,count:100}});
    await page.locator('[data-open-goals]').click();
    await page.locator('#goal-target').fill('100');
    assert.match(await page.locator('#goal-preview').textContent(), /Earlier days keep their previous targets/);
    assert.match(await page.locator('#goal-preview').textContent(), /Saving again today replaces today's target/);
    if(captures) {fs.mkdirSync(captures,{recursive:true});await page.screenshot({path:captures+'/settings-desktop.png',fullPage:true});}
    await page.locator('#goal-save').click();
    await page.waitForFunction(()=>document.querySelector('#goal-settings-status').textContent.startsWith('Daily goal saved'));
    await page.locator('#settings-close').click();
    await page.locator('#reps').fill('99'); await page.locator('#log-submit').click();
    await page.waitForFunction(()=>document.querySelector('#goal-progress-text').textContent.includes('99 / 100'));
    assert.equal(await page.locator('.day.goal-reached').count(),0);
    const fill = d => page.locator(`.day[data-day="${d}"]`).evaluate(el=>({level:el.style.getPropertyValue('--fill'),partial:el.classList.contains('is-partial'),total:el.querySelector('.day-fill .t').textContent}));
    assert.deepEqual(await fill(session.today),{level:'99%',partial:true,total:'99'},'a goal day fills to its share of the target');
    if(await page.locator(`.day[data-day="${yesterday}"]`).count()) {
      const before = await fill(yesterday);
      assert.deepEqual(before,{level:before.total==='0'?'0%':'100%',partial:false,total:before.total},'a day before any goal is empty or full, never partial');
    }
    await page.locator('#reps').fill('1'); await page.locator('#log-submit').click();
    await page.locator('#goal-reflection').waitFor({state:'visible'});
    await page.locator(`.day[data-day="${session.today}"].goal-reached`).waitFor();
    if(await page.locator(`.day[data-day="${yesterday}"]`).count()) assert.equal(await page.locator(`.day[data-day="${yesterday}"].goal-reached`).count(),0);
    assert.match(await page.locator(`.day[data-day="${session.today}"]`).getAttribute('aria-label'),/Goal reached: 100/);
    const firstQuote = await page.locator('#goal-quote').textContent();
    assert.ok((await page.evaluate(()=>window.goalAnimations)).length>0,'completion check animates');
    await page.evaluate(()=>navigator.serviceWorker.ready);
    await page.reload(); await page.locator('#goal-reflection').waitFor({state:'visible'});
    assert.equal(await page.locator('#goal-quote').textContent(),firstQuote);
    await page.emulateMedia({reducedMotion:'reduce'});
    if(captures) {
      await page.screenshot({path:captures+'/desktop.png',fullPage:true});
      await page.setViewportSize({width:390,height:844});
      await page.waitForTimeout(400); // settle the dock-to-sheet visibility transition
      await page.evaluate(()=>window.scrollTo(0,0));
      await page.screenshot({path:captures+'/mobile.png',fullPage:true});
      await page.locator('[data-open-goals]').click();
      await page.locator('#goal-target').fill('150');
      await page.screenshot({path:captures+'/settings-mobile.png',fullPage:true});
      await page.locator('#classic-theme').check();
      await page.screenshot({path:captures+'/settings-classic.png',fullPage:true});
      await page.locator('#settings-close').click();
      await page.screenshot({path:captures+'/classic-mobile.png',fullPage:true});
      await page.setViewportSize({width:320,height:760});
      assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'no 320px overflow');
      await page.screenshot({path:captures+'/narrow.png',fullPage:true});
      await page.setViewportSize({width:1440,height:1000});
      await page.screenshot({path:captures+'/classic-desktop.png',fullPage:true});
    }
    await page.setViewportSize({width:1440,height:1000});
    await page.locator('[data-open-goals]').click();
    await page.locator('#goal-target').fill('150');
    await page.locator('#goal-save').click();
    await page.waitForFunction(()=>document.querySelector('#goal-settings-status').textContent.includes('saved: 150'));
    await page.locator('#settings-close').click();
    await page.locator('#goal-reflection').waitFor({state:'hidden'});
    assert.equal(await page.locator('.day.goal-reached').count(),0);
    await ctx.setOffline(true);
    await page.reload(); await page.locator('#app').waitFor({state:'visible'});
    await page.waitForFunction(()=>document.querySelector('#goal-progress-text').textContent.includes('100 / 150'));
    await page.locator('#reps').fill('50'); await page.locator('#log-submit').click();
    await page.locator('#goal-reflection').waitFor({state:'visible'});
    assert.equal(await page.locator('#goal-quote').textContent(),firstQuote);
    assert.equal((await page.evaluate(()=>window.goalAnimations)).length,0,'reduced motion suppresses animation');
    await page.reload(); await page.locator('#goal-reflection').waitFor({state:'visible'});
    assert.match(await page.locator('#goal-progress-text').textContent(),/150 \/ 150/);
    await ctx.setOffline(false);
    await page.locator('#sync-status button').first().click();
    await page.locator('#sync-status').waitFor({state:'hidden'});
    const snapshot=await(await ctx.request.get(base+'/api/offline')).json();
    assert.equal(snapshot.goals.length,1); assert.equal(snapshot.goals[0].target,150);
    const entry=snapshot.entries.find(e=>e.count===50 && e.day===session.today);
    await ctx.request.delete(base+'/api/reps/'+entry.id,{headers:{'X-CSRF-Token':session.csrf_token}});
    await page.locator('#goal-reflection').waitFor({state:'hidden'});
    assert.equal(await page.locator('.day.goal-reached').count(),0,'deletion removes completion');
    assert.equal((await fill(session.today)).level,'66.7%','deletion lowers the fill');
    // Start a separate touch device at phone width; do not infer phone behavior
    // solely from resizing the desktop context.
    const phone = await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true,deviceScaleFactor:2,reducedMotion:'reduce',storageState:await ctx.storageState()});
    const touch = await phone.newPage();
    await touch.addInitScript(()=>localStorage.setItem("pushup-counter:theme","plant"));
    touch.on('pageerror',e=>errors.push(e.message));
    await touch.route('**/api/quotes',r=>r.fulfill({json:{quotes:[]}}));
    await touch.goto(base);
    await touch.waitForFunction(()=>document.querySelector('#goal-progress-text').textContent.includes('100 / 150'));
    await touch.locator('[data-open-goals]').tap();
    await touch.locator('#goal-target').waitFor({state:'visible'});
    const input = await touch.locator('#goal-target').boundingBox();
    assert.ok(input.height>=48 && input.width>=250,'large phone input');
    await touch.locator('#goal-target').fill('150');
    await touch.evaluate(()=>document.querySelector('.settings-content').scrollTop=10000);
    const close = await touch.locator('#settings-close').boundingBox();
    assert.ok(close.y>=0 && close.y+close.height<100,'close remains pinned while scrolling');
    await touch.locator('#goal-target').scrollIntoViewIfNeeded();
    if(captures) await touch.screenshot({path:captures+'/phone-settings.png'});
    await touch.locator('#settings-close').tap();
    await touch.locator('#fab').tap();
    await touch.locator('#reps').fill('50');
    await touch.locator('#log-submit').tap();
    await touch.locator('#goal-reflection').waitFor({state:'visible'});
    const reflection = await touch.locator('#goal-reflection').boundingBox();
    assert.ok(reflection.y>=0 && reflection.y+reflection.height<=844,'reflection comes into view after phone completion');
    assert.equal(await touch.locator('#log').evaluate(el=>el.classList.contains('is-open')),false);
    if(captures) await touch.screenshot({path:captures+'/phone-completion.png'});
    await touch.evaluate(()=>navigator.serviceWorker.ready);
    await phone.setOffline(true);
    await touch.reload(); await touch.locator('#goal-reflection').waitFor({state:'visible'});
    await touch.locator('[data-open-goals]').tap();
    await touch.waitForFunction(()=>document.querySelector('#goal-settings-error').textContent.includes('Reconnect'));
    assert.equal(await touch.locator('#goal-save').isDisabled(),true);
    await touch.locator('#settings-close').tap();
    await touch.setViewportSize({width:320,height:760});
    assert.ok(await touch.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'phone has no horizontal overflow');
    await phone.close();
    assert.deepEqual(errors,[]);
    console.log('Goal preview, date boundary, completion, animation, both themes, offline reload/sync, deletion and reduced motion passed.');
  } finally { await browser.close(); }
})().catch(e=>{console.error(e);process.exit(1)});
