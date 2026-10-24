// Requires Playwright and axe-core. Run against a disposable server.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const axe = require.resolve(process.env.AXE_CORE_MODULE || 'axe-core/axe.min.js');
const fs = require('node:fs');
const assert = require('node:assert/strict');
const base = process.env.PUSHUP_TEST_URL || 'http://localhost:5580';
(async () => {
  const browser = await chromium.launch({headless:true,...(process.env.CHROMIUM_PATH ? {executablePath:process.env.CHROMIUM_PATH}:{}),args:['--no-sandbox']});
  const results=[];
  try {
    for(const theme of ['plant','classic']) for(const mode of ['light','dark']) {
      const ctx=await browser.newContext({viewport:{width:1440,height:1000},colorScheme:mode,reducedMotion:'reduce'});
      await ctx.addInitScript(theme=>localStorage.setItem('pushup-counter:theme',theme),theme);
      const p=await ctx.newPage();
      await p.route('**/api/quotes',r=>r.fulfill({json:{quotes:[]}}));
      async function audit(state,root) {
        await p.evaluate(()=>document.fonts.ready);
        if(!await p.evaluate(()=>Boolean(window.axe))) await p.addScriptTag({path:axe});
        const report=await p.evaluate(async root=>window.axe.run(root||document,{runOnly:{type:'rule',values:['color-contrast']}}),root);
        const manualChecks=[];
        // Scroll clipped dialog content into view and recheck it in isolation.
        for(const rule of report.incomplete) for(const node of rule.nodes) {
          // axe can report the audit root itself, not only its descendants.
          const locator=p.locator(node.target.join(' ')).first();
          if(await locator.count() && !node.target.includes('.tail')) {
            await locator.evaluate(el=>{el.scrollIntoView({block:'center'});el.setAttribute('data-contrast-check','')});
            const retry=await p.evaluate(()=>window.axe.run('[data-contrast-check]',{runOnly:{type:'rule',values:['color-contrast']}}));
            await locator.evaluate(el=>el.removeAttribute('data-contrast-check'));
            if(!retry.violations.length && !retry.incomplete.length) {
              manualChecks.push({target:node.target,result:'passed after scrolling into view'});
              node.resolved=true;
            }
            else if(!retry.violations.length) {
              // axe can mistake content behind a native dialog for overlapping
              // content. Verify actual inherited text and opaque dialog colors.
              const measured=await locator.evaluate(el=>{
                const style=getComputedStyle(el);let background=null;
                for(let current=el;current;current=current.parentElement){
                  const s=getComputedStyle(current);
                  if(Number(s.opacity)!==1||s.backgroundImage!=='none')return null;
                  if(!background && s.backgroundColor.startsWith('rgb('))background=s.backgroundColor;
                }
                if(!background||!style.color.startsWith('rgb('))return null;
                const lum=c=>{const rgb=c.match(/[\d.]+/g).slice(0,3).map(Number).map(n=>n/255).map(n=>n<=.04045?n/12.92:((n+.055)/1.055)**2.4);return rgb[0]*.2126+rgb[1]*.7152+rgb[2]*.0722};
                const a=lum(style.color),b=lum(background);
                return{foreground:style.color,background,ratio:(Math.max(a,b)+.05)/(Math.min(a,b)+.05)};
              });
              if(measured){
                assert.ok(measured.ratio>=4.5,`dialog text contrast: ${JSON.stringify(measured)}`);
                manualChecks.push({target:node.target,...measured,result:'opaque dialog colors verified'});
                node.resolved=true;
              }
            }
          }
        }
        // Classic's transparent, outlined footer uses stroke color, which axe
        // cannot evaluate. Check that actual stroke against its page ground.
        if(report.incomplete.some(r=>r.nodes.some(n=>n.target.includes('.tail')))) {
          const ratio=await p.locator('.tail').evaluate(el=>{
            const style=getComputedStyle(el),bg=getComputedStyle(document.body).backgroundColor;
            const lum=c=>{const rgb=c.match(/[\d.]+/g).slice(0,3).map(Number).map(n=>n/255).map(n=>n<=.04045?n/12.92:((n+.055)/1.055)**2.4);return rgb[0]*.2126+rgb[1]*.7152+rgb[2]*.0722};
            const a=lum(style.webkitTextStrokeColor),b=lum(bg);return(Math.max(a,b)+.05)/(Math.min(a,b)+.05);
          });
          assert.ok(ratio>=3,`outlined large footer text contrast: ${ratio}`);
          manualChecks.push({target:['.tail'],ratio,result:'stroke contrast passed'});
          for(const rule of report.incomplete)for(const node of rule.nodes)if(node.target.includes('.tail'))node.resolved=true;
        }
        results.push({theme,mode,width:p.viewportSize().width,state,passes:report.passes.reduce((n,r)=>n+r.nodes.length,0),manualChecks,violations:report.violations.flatMap(r=>r.nodes.map(n=>({target:n.target,summary:n.failureSummary}))),incomplete:report.incomplete.flatMap(r=>r.nodes.filter(n=>!n.resolved).map(n=>({target:n.target,summary:n.failureSummary})))});
        fs.writeFileSync(process.env.PUSHUP_CONTRAST_REPORT||'/tmp/pushup-contrast-report.json',JSON.stringify(results,null,2));
      }
      await p.goto(base);await p.locator('#gate').waitFor({state:'visible'});
      await audit('sign-in');
      await p.locator('#auth-submit').hover();await p.waitForTimeout(150);await audit('sign-in hover','#auth-submit');
      await p.mouse.down();await audit('sign-in pressed','#auth-submit');await p.mouse.up();
      await p.locator('#gate [data-open-settings]').click();await p.locator('.settings-nav [href="#about-settings"]').click();await audit('signed-out settings','#settings');await p.locator('#settings-close').click();
      await p.goto(base+'/forgot-password');await p.locator('#recovery').waitFor({state:'visible'});await audit('password recovery');
      await p.goto(base);await p.locator('[data-mode=register]').click();
      await p.locator('#auth-user').fill(`contrast_${theme}_${mode}_${Date.now()}`);await p.locator('#auth-pass').fill('contrast-test-password');await p.locator('#auth-submit').click();await p.locator('#app').waitFor({state:'visible'});
      await audit('email reminder');await p.locator('#email-prompt-dismiss').click();
      const session=await(await ctx.request.get(base+'/api/session')).json();
      const types=await(await ctx.request.get(base+'/api/types')).json();
      assert.ok((await ctx.request.put(base+'/api/goals',{headers:{'X-CSRF-Token':session.csrf_token},data:{target:20,effective_day:session.today}})).ok());
      for(const [i,count] of [20,50,80,100].entries()) {
        const day=new Date(Date.parse(session.today+'T12:00:00Z')-i*86400000).toISOString().slice(0,10);
        assert.ok((await ctx.request.post(base+'/api/reps',{headers:{'X-CSRF-Token':session.csrf_token},data:{day,type_id:types.types[0].id,count}})).ok());
      }
      await p.reload();await p.locator('#goal-reflection').waitFor({state:'visible'});
      for(const width of [1440,390]) {
        await p.setViewportSize({width,height:1000});await p.waitForTimeout(400);
        await audit('log and completed goal');
        await p.locator('#day-entries button').first().click();
        await audit('delete confirmation','#confirm-action');
        await p.locator('#confirm-cancel').click();
        await p.locator('#app [data-open-settings]').click();
        await audit('settings overview','#settings');
        for (const id of ['appearance-settings','account-settings','about-settings','goal-settings']) {
          if (await p.locator('#settings-back').isVisible()) await p.locator('#settings-back').click();
          await p.locator(`.settings-nav [href="#${id}"]`).click();
          await audit(id,'#settings');
        }
        await p.evaluate(()=>{const error=document.querySelector('#goal-settings-error');error.hidden=false;error.textContent='Reconnect to save a goal. Your saved targets still apply offline.'});
        await audit('settings error','#goal-settings-error');
        await p.locator('#settings-close').click();
        if(width<768)await p.locator('#fab').click();
        await p.locator('#reps').fill('12');
        await audit('log controls','#log');
        await p.locator('#log-submit').hover();await p.waitForTimeout(150);await audit('log hover','#log-submit');
        await p.mouse.down();await audit('log pressed','#log-submit');await p.mouse.move(0,0);await p.mouse.up();
        await p.locator('#type-trigger').click();await audit('variation picker','#type-picker');await p.keyboard.press('Escape');
        if(width<768)await p.keyboard.press('Escape');
        await p.locator('#nav-crew').click();await p.locator('#scope-everyone').click();await p.locator('#global-board .lb').first().waitFor();await audit('global board');
        await p.locator('#scope-crew').click();await audit('crew board');
        await p.locator('#nav-log').click();
      }
      await ctx.close();
    }
    const output=process.env.PUSHUP_CONTRAST_REPORT||'/tmp/pushup-contrast-report.json';fs.writeFileSync(output,JSON.stringify(results,null,2));
    const failed=results.filter(r=>r.violations.length);
    console.log(JSON.stringify({states:results.length,textChecks:results.reduce((n,r)=>n+r.passes,0),failures:failed,incomplete:results.filter(r=>r.incomplete.length),report:output},null,2));
    if(failed.length||results.some(r=>r.incomplete.length))process.exitCode=1;
  } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exit(1)});
