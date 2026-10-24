// Run against a disposable server. Checks the real touch highlight and tab geometry.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const assert=require('node:assert/strict');
const base=process.env.PUSHUP_TEST_URL||'http://localhost:5580';
(async()=>{
  const browser=await chromium.launch({headless:true,...(process.env.CHROMIUM_PATH?{executablePath:process.env.CHROMIUM_PATH}:{}),args:['--no-sandbox']});
  try {
    const ctx=await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true,reducedMotion:'reduce'});
    assert.ok((await ctx.request.post(base+'/api/register',{data:{username:`nav_${Date.now()}`,password:'nav-test-password'}})).ok());
    const page=await ctx.newPage();await page.goto(base);await page.locator('#app').waitFor({state:'visible'});await page.evaluate(()=>document.fonts.ready);
    for(const theme of ['plant','classic'])for(const mode of ['light','dark'])for(const width of [390,1440]) {
      await page.setViewportSize({width,height:1000});
      await page.evaluate(({theme,mode})=>{PushupTheme.set(theme);PushupTheme.setMode(mode)},{theme,mode});
      await page.evaluate(()=>document.fonts.ready);
      for(const id of ['nav-log','nav-crew']) {
        await page.locator('#'+id).tap();
        const result=await page.locator('#'+id).evaluate(button=>{
          const range=document.createRange();range.selectNodeContents(button.firstChild);
          const text=range.getBoundingClientRect(),rect=button.getBoundingClientRect(),style=getComputedStyle(button);
          return{left:text.left-rect.left,right:rect.right-text.right,highlight:style.webkitTapHighlightColor,width:rect.width,rail:Number(getComputedStyle(button.parentElement).getPropertyValue('--rail-width'))};
        });
        assert.equal(result.highlight,'rgba(0, 0, 0, 0)',`${theme}/${mode}/${width}: native blue tap highlight`);
        assert.ok(Math.abs(result.left-result.right)<1,`${theme}/${mode}/${width}: ${id} text off-center ${JSON.stringify(result)}`);
        assert.ok(Math.abs(result.width-result.rail)<1,'underline matches the active button');
      }
      const settings=page.locator('#app [data-open-settings]');
      assert.equal(await settings.evaluate(el=>getComputedStyle(el).webkitTapHighlightColor),'rgba(0, 0, 0, 0)');
      await settings.tap();await page.locator('#settings-close').tap();
      await page.keyboard.press('Tab');await page.locator('#nav-log').focus();
      assert.equal(await page.locator('#nav-log').evaluate(el=>el.matches(':focus-visible')),true);
      assert.equal(await page.locator('#nav-log').evaluate(el=>getComputedStyle(el).outlineStyle),'solid');
    }
    console.log('Touch highlights removed, LOG/BOARD centered over their underline, and keyboard focus preserved in all four palettes.');
  } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exit(1)});
