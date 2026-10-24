// Run against a disposable server. Exercises actual deletion paths and the
// shared sheet used by logging and destructive confirmations.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const base=process.env.PUSHUP_TEST_URL||'http://localhost:5583';
const captures=process.env.PUSHUP_SCREENSHOTS;
(async()=>{
  const browser=await chromium.launch({headless:true,...(process.env.CHROMIUM_PATH?{executablePath:process.env.CHROMIUM_PATH}:{}),args:['--no-sandbox']});
  try {
    const ctx=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});
    const registration=await ctx.request.post(base+'/api/register',{data:{username:`confirm_${Date.now()}`,password:'confirmation-password'}});
    assert.ok(registration.ok());const session=await registration.json();
    const headers={'X-CSRF-Token':session.csrf_token};
    const page=await ctx.newPage();const mutations=[],errors=[];
    page.on('request',r=>{if(r.method()==='DELETE')mutations.push(r.url())});page.on('pageerror',e=>errors.push(e.message));
    await page.route('**/api/quotes',r=>r.fulfill({json:{quotes:[]}}));
    await page.goto(base);await page.locator('#app').waitFor({state:'visible'});await page.locator('#email-prompt-dismiss').click();
    if(captures)fs.mkdirSync(captures,{recursive:true});
    for(const theme of ['plant','classic'])for(const mode of ['light','dark'])for(const width of [1440,390]) {
      await page.setViewportSize({width,height:1000});
      await page.evaluate(({theme,mode})=>{PushupTheme.set(theme);PushupTheme.setMode(mode)},{theme,mode});
      const typeResponse=await ctx.request.post(base+'/api/types',{headers,data:{name:`Test ${theme} ${mode} ${width}`}});assert.ok(typeResponse.ok());const type=await typeResponse.json();
      const entryResponse=await ctx.request.post(base+'/api/reps',{headers,data:{day:session.today,type_id:type.id,count:17}});assert.ok(entryResponse.ok());
      await page.reload();await page.locator('#app').waitFor({state:'visible'});
      const row=page.locator('#day-entries li').filter({hasText:type.name});await row.waitFor();
      const before=mutations.length;
      await row.locator('button').click();await page.locator('#confirm-action').waitFor({state:'visible'});
      assert.match(await page.locator('#confirm-message').textContent(),new RegExp(`17 ${type.name}`));
      assert.equal(await page.evaluate(()=>document.activeElement.id),'confirm-cancel');
      assert.equal(mutations.length,before,'opening never mutates');
      const rect=await page.locator('#confirm-action').boundingBox();
      if(width<768){assert.ok(Math.abs(rect.y+rect.height-1000)<2,'mobile sheet anchored to bottom');assert.ok(Math.abs(rect.width-width)<2,'mobile sheet spans width');}
      else {assert.ok(Math.abs(rect.x+rect.width/2-width/2)<2,'desktop dialog centered horizontally');assert.ok(Math.abs(rect.y+rect.height/2-500)<2,'desktop dialog centered vertically');}
      if(captures)await page.screenshot({path:`${captures}/${theme}-${mode}-${width}.png`});
      await page.keyboard.press('Enter');await page.locator('#confirm-action').waitFor({state:'hidden'});
      assert.equal(mutations.length,before,'Enter on initial Cancel never deletes');
      assert.ok(await row.isVisible());
      await row.locator('button').click();await page.keyboard.press('Escape');await page.locator('#confirm-action').waitFor({state:'hidden'});
      assert.equal(mutations.length,before,'Escape never deletes');
      assert.equal(await row.locator('button').evaluate(el=>el===document.activeElement),true,'focus returns to trigger');
      await row.locator('button').click();await page.mouse.click(2,2);await page.locator('#confirm-action').waitFor({state:'hidden'});
      assert.equal(mutations.length,before,'backdrop cancels');
      await row.locator('button').click();await page.locator('#confirm-accept').click();await row.waitFor({state:'detached'});
      assert.equal(mutations.length,before+1,'exactly one confirmed set deletion');
      // Variation confirmation explicitly covers cascading deletion across days.
      assert.ok((await ctx.request.post(base+'/api/reps',{headers,data:{day:session.today,type_id:type.id,count:23}})).ok());
      await page.reload();await page.locator('#app').waitFor({state:'visible'});
      const variation=page.locator(`#type-list li[data-type-id="${type.id}"]`);
      await variation.locator('button').click();
      assert.match(await page.locator('#confirm-message').textContent(),/every set.*across all days/);
      await page.locator('#confirm-cancel').click();assert.ok(await variation.isVisible());
      await variation.locator('button').click();await page.locator('#confirm-accept').click();await variation.waitFor({state:'detached'});
      assert.equal(mutations.length,before+2,'exactly one confirmed variation deletion');
      const day=await(await ctx.request.get(base+'/api/day/'+session.today)).json();assert.ok(!day.entries.some(e=>e.type_id===type.id),'variation deletion removes its sets');
      if(width<768){
        await page.locator('#fab').click();assert.equal(await page.locator('#log').evaluate(el=>el.classList.contains('is-open')),true);
        assert.equal(await page.evaluate(()=>document.body.style.overflow),'hidden');
        assert.equal(await page.evaluate(()=>document.querySelector('.chrome').inert),true);
        await page.locator('#reps').fill('31');await page.keyboard.press('Escape');
        assert.equal(await page.locator('#log').evaluate(el=>el.classList.contains('is-open')),false);
        assert.equal(await page.locator('#reps').inputValue(),'31','closing shared sheet preserves draft');
        assert.equal(await page.evaluate(()=>document.querySelector('.chrome').inert),false);
        assert.equal(await page.evaluate(()=>document.body.style.overflow),'');
        await page.locator('#fab').click();await page.setViewportSize({width:1440,height:1000});
        await page.waitForFunction(()=>!document.querySelector('#log').classList.contains('is-open'));
        assert.equal(await page.evaluate(()=>document.body.style.overflow),'','docking releases shared scroll lock');
      }
    }
    // Nested dialog: Cancel returns to Settings without closing or resetting it.
    await page.route('**/api/account',async r=>{
      const response=await r.fetch();const account=await response.json();account.email='test@example.invalid';account.email_verified=true;
      await r.fulfill({response,json:account});
    });
    await page.locator('#app [data-open-settings]').click();await page.locator('.settings-nav [href="#account-settings"]').click();await page.locator('#account-email-remove').waitFor({state:'visible'});
    await page.locator('#account-password').fill('confirmation-password');
    await page.locator('#account-email-remove').click();await page.locator('#confirm-action').waitFor({state:'visible'});
    await page.locator('#confirm-cancel').click();assert.ok(await page.locator('#settings').isVisible());
    assert.equal(await page.locator('#account-password').inputValue(),'confirmation-password');
    await page.locator('#account-email-remove').click();
    const before=mutations.length;
    await page.evaluate(()=>window.AppAccount.leave());await page.locator('#confirm-action').waitFor({state:'hidden'});
    assert.equal(mutations.length,before,'account change cancels outstanding approval');
    assert.deepEqual(errors,[]);
    console.log('Desktop dialogs, mobile sheets, cancellation, safe default focus, exact-once deletion, cascades, nested Settings and shared logging/docking behavior passed.');
  }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exit(1)});
