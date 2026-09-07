const { createRequire } = require('node:module');
const path = require('node:path');
const requireTest = createRequire(path.resolve(__dirname, '../../tests/e2e-apps/package.json'));
const { chromium } = requireTest('@playwright/test');
(async () => {
 const browser = await chromium.launch({headless:true});
 try {
  for (const door of ['verify','enrolment','worker']) {
   const context = await browser.newContext({serviceWorkers:'allow'});
   const page = await context.newPage();
   const errors=[]; page.on('pageerror', e=>errors.push(e.message));
   await page.goto(`http://localhost:59510/${door}/`,{waitUntil:'networkidle'});
   await page.evaluate(async()=>{await navigator.serviceWorker.ready;});
   await page.waitForFunction(()=>!!navigator.serviceWorker.controller);
   const initial = await page.locator('#root').innerText();
   if (initial.length<50 || errors.length) throw new Error(`${door}: initial render failed: ${errors.join(';')}`);
   await context.setOffline(true);
   await page.close();
   const cold=await context.newPage();
   cold.on('pageerror',e=>errors.push(e.message));
   await cold.goto(`http://localhost:59510/${door}/`,{waitUntil:'domcontentloaded'});
   await cold.waitForFunction(()=>document.getElementById('root')?.innerText.length>50);
   if(errors.length)throw new Error(`${door}: offline render errors: ${errors.join(';')}`);
   if (door === 'worker') {
    let calls = 0;
    cold.on('request', request => { if(request.url().includes('/api/')) calls++; });
    await cold.getByRole('link', { name: 'Open saved device wallet' }).click();
    await cold.getByRole('button', { name: 'Unlock browser wallet' }).waitFor({state:'visible'});
    if(calls) throw new Error('local wallet unlock screen contacted the backend without a session');
    console.log('PASS: logged-out offline worker can reach passphrase unlock without contacting the backend');
   }
   console.log(`PASS: ${door} cold offline app-shell reload (no login, seeded data, or provider substitution)`);
   await context.close();
  }
 } finally {await browser.close();}
})().catch(err=>{console.error(err);process.exit(1);});
