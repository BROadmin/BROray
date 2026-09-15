/* Offline UI test: file URLs only, no router address or API requests. */
const {chromium}=require('playwright');
const path=require('node:path'),fs=require('node:fs'),assert=require('node:assert/strict');
const {pathToFileURL}=require('node:url');
const root=path.resolve(__dirname,'../..');
const evidence=path.join(root,'docs/evidence/ui-preview');fs.mkdirSync(evidence,{recursive:true});
(async()=>{
  const browser=await chromium.launch({headless:true});
  const context=await browser.newContext({acceptDownloads:true});
  const page=await context.newPage();const errors=[],network=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',route=>{
    const url=route.request().url();
    if(url.startsWith('http:')||url.startsWith('https:')) {network.push(url);return route.abort();}
    return route.continue();
  });
  let checks=0;
  try {
    await page.goto(pathToFileURL(path.join(root,'implementation/preview/operations.html')).href);
    for(const width of [1440,1024,390,360]) for(const theme of ['broray','day']) {
      await page.setViewportSize({width,height:1000});await page.selectOption('#theme',theme);
      for(const scenario of ['checking','fetching','idle','ambiguous','protected','unavailable']) {
        await page.selectOption('#scenario',scenario);
        const layout=await page.evaluate(()=>({width:innerWidth,scroll:document.documentElement.scrollWidth,
          bad:[...document.querySelectorAll('button:not([hidden])')].filter(e=>e.getBoundingClientRect().width>0).filter(e=>{
            const r=e.getBoundingClientRect();return r.left<0||r.right>innerWidth+1||e.scrollWidth>e.clientWidth+1;
          }).map(e=>e.id)}));
        assert(layout.scroll<=layout.width+1,`${width}/${theme}/${scenario}: page overflow`);
        assert.deepEqual(layout.bad,[],`${width}/${theme}/${scenario}: clipped button`);checks++;
      }
      await page.selectOption('#scenario','checking');
      await page.screenshot({path:path.join(evidence,`${width}-${theme}.png`),fullPage:true,animations:'disabled'});
    }
    await page.click('#cancel');assert(await page.isDisabled('#cancel'));
    assert.match(await page.textContent('#ops-badge'),/Остановка запрошена/);checks++;
    await page.click('#stop-all');assert.match(await page.textContent('#automation-badge'),/На паузе/);
    await page.click('#resume');assert.match(await page.textContent('#automation-badge'),/Включена/);checks++;
    await page.selectOption('#scenario','protected');assert(!(await page.isVisible('#cancel')));checks++;
    await page.selectOption('#scenario','ambiguous');await page.click('#recover');
    assert.match(await page.textContent('#operation-feedback'),/Блокировка сохранена/);checks++;
    await page.selectOption('#scenario','unavailable');
    assert(!(await page.textContent('#ops-summary')).includes('Активных фоновых операций нет'));checks++;
    const downloadPromise=page.waitForEvent('download');await page.click('#download');
    const download=await downloadPromise;const downloadPath=path.join(evidence,'example-report.json');await download.saveAs(downloadPath);
    const report=JSON.parse(fs.readFileSync(downloadPath,'utf8'));assert.equal(report.demonstration,true);assert.equal(report.routerAccessed,false);checks++;
    assert.deepEqual(errors,[]);assert.deepEqual(network,[]);
    fs.writeFileSync(path.join(root,'docs/evidence/ui-preview-tests.json'),JSON.stringify({status:'PASS',checks,
      viewports:[1440,1024,390,360],themes:['broray','day'],scenarios:6,screenshots:8,scope:'Offline mock using exact baseline CSS; production backend not integrated',routerAccessed:false,externalRequests:network},null,2));
    console.log(JSON.stringify({status:'PASS',checks,screenshots:8}));
  } finally {await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
