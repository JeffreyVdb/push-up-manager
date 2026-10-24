// Detect transient offline claims, not just the final connected state.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert=require('node:assert/strict');
const base=process.env.PUSHUP_TEST_URL || 'http://localhost:5575';
(async()=>{
 const browser=await chromium.launch({headless:true,...(process.env.CHROMIUM_PATH?{executablePath:process.env.CHROMIUM_PATH}:{}),args:['--no-sandbox']});
 try {
  const context=await browser.newContext({viewport:{width:1440,height:1000}});
  await context.addInitScript(()=>{
   window.offlineClaims=[];
   const observer=new MutationObserver(()=>{
    const banner=document.querySelector('#sync-status');
    if(banner && !banner.hidden && !document.querySelector('#app').hidden && /^Offline\./.test(banner.textContent)) window.offlineClaims.push(banner.textContent);
   });
   observer.observe(document,{subtree:true,childList:true,attributes:true,characterData:true});
  });
  const page=await context.newPage();
  await page.goto(base);
  await page.locator('[data-mode=register]').click();
  await page.locator('#auth-user').fill('connectivity_'+Date.now());
  await page.locator('#auth-pass').fill('connectivity-test-password');
  await page.locator('#auth-submit').click();
  await page.waitForFunction(()=>document.querySelectorAll('.day').length>0);
  await page.evaluate(()=>navigator.serviceWorker.ready);
  // A brief, successful response delay makes the startup transition observable.
  await page.route('**/api/session',async route=>{await new Promise(r=>setTimeout(r,300));await route.continue()});
  await page.reload();
  await page.waitForFunction(()=>document.querySelectorAll('.day').length>0);
  await page.waitForTimeout(700);
  const claims=await page.evaluate(()=>window.offlineClaims);
  assert.deepEqual(claims,[],'online reload must never claim Offline while the first connection check is pending');
  assert.equal(await page.locator('#sync-status').isVisible(),false);
  console.log('Online desktop reload renders cached data without a false offline banner.');
  await page.unroute('**/api/session');
  await page.route('**/api/session',route=>route.abort('failed'));
  await page.reload();
  await page.waitForFunction(()=>document.querySelector('#sync-status')?.textContent.startsWith('Offline.'));
  assert.equal(await page.locator('#sync-status').isVisible(),true);
  console.log('A failed server check still shows the offline notice.');
  await page.unroute('**/api/session');
  await context.setOffline(true);
  await page.reload();
  await page.waitForFunction(()=>document.querySelector('#sync-status')?.textContent.startsWith('Offline.'));
  assert.equal(await page.locator('#sync-status').isVisible(),true);
  console.log('Airplane mode immediately shows the offline notice.');
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exit(1)});
