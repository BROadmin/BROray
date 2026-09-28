/* Production page in Chromium, mocked HTTP only; no physical installation claim. */
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path'),assert=require('assert/strict');
const root=path.resolve(__dirname,'../runtime/app/web-new');
const out=process.env.BRORAY_TEST_EVIDENCE;assert(out);fs.mkdirSync(out,{recursive:false});
const records=JSON.parse(fs.readFileSync(path.resolve(root,'../share/xray-compatibility.json'))).records;
const partial=records.filter(r=>r.candidateId==='3.2.0-r01c23'&&r.status==='incompatible');assert.equal(partial.length,6);
const make=(tag,compatibility)=>({tagName:tag,version:tag.slice(1),available:true,installed:tag==='v26.9.9',archiveSha256:'a'.repeat(64),asset:{size:20000000},prerelease:false,compatibility});
const rows=partial.map(r=>make(r.xrayTag,r));
rows.push(make('v26.9.9',{status:'compatible'}),make('v26.10.1',{status:'untested'}));
const catalog={currentVersion:'26.9.9',latestVersion:'26.10.1',catalogComplete:true,releases:rows,storage:{ok:true,freeBytes:1e9},temporaryStorage:{reinstallAllowed:true,requiredBytes:50000000,freeBytes:1e9}};
(async()=>{
 const browser=await chromium.launch({headless:true}),page=await browser.newPage({viewport:{width:1435,height:890}});
 const checks=[],errors=[],posts=[];let failure;
 page.on('pageerror',e=>errors.push(String(e)));
 await page.route('**/*',async route=>{
  const u=new URL(route.request().url());if(u.hostname!=='broray.test')return route.abort();
  if(u.pathname.startsWith('/api/')){
   let data={};
   if(u.pathname==='/api/session.cgi')data={user:'fixture'};
   if(u.pathname==='/api/xray/status.cgi')data={running:true,version:'26.9.9',pid:1234,configValid:true,socksActive:true};
   if(u.pathname==='/api/xray/update-check.cgi')data=catalog;
   if(u.pathname==='/api/xray/install.cgi'){
    posts.push(route.request().postDataJSON());
    return route.fulfill({status:202,json:{success:true,data:{accepted:true,operation:'install',operationId:'fixture-only'}}});
   }
   return route.fulfill({json:{success:true,data}});
  }
  const file=path.resolve(root,'.'+u.pathname);
  if(!file.startsWith(root+path.sep)||!fs.existsSync(file))return route.fulfill({status:404,body:''});
  return route.fulfill({body:fs.readFileSync(file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]||'application/octet-stream'});
 });
 async function check(label,fn){await fn();checks.push(label);console.log('PASS',label);}
 async function load(){await page.goto('http://broray.test/xray.html');await page.locator('#xray-update-check:not([disabled])').waitFor();await page.locator('#xray-update-check').click();await page.locator('#xray-version-select:not([disabled])').waitFor();}
 try{
  await load();
  for(const row of rows){await check('Selection '+row.tagName,async()=>{
   await page.locator('#xray-version-select').selectOption(row.tagName);assert(await page.locator('#xray-update-install').isEnabled());
   const text=await page.locator('#xray-selected-release-note').innerText();assert(text.includes(row.version));
   for(const limitation of row.compatibility.limitations||[])assert(text.includes(limitation),limitation);
   if(row.compatibility.status==='compatible')assert(!text.includes('Не поддерживаются:'));
   if(row.compatibility.status==='untested')assert(text.includes('не подтверждали'));
  });}
  await page.locator('#xray-version-select').selectOption('v26.3.27');
  await check('Partial confirm/cancel preserves selection and sends nothing',async()=>{
   await page.locator('#xray-update-install').click();await page.locator('#confirm-accept').waitFor();
   assert((await page.locator('#confirm-message').innerText()).includes('mkcp-legacy'));
   await page.screenshot({path:path.join(out,'partial-confirm.png'),fullPage:true});
   await page.locator('#confirm-cancel').click();assert.equal(posts.length,0);
  });
  await check('Confirmed selection carries explicit partial consent and exact digest',async()=>{
   await page.locator('#xray-update-install').click();const reply=page.waitForResponse(r=>r.url().endsWith('/xray/install.cgi'));
   await page.locator('#confirm-accept').click();await reply;
   assert.deepEqual(posts[0],{tag:'v26.3.27',currentVersion:'26.9.9',archiveSha256:'a'.repeat(64),allowPartial:true,allowUntested:false,allowPrerelease:false,allowDowngrade:true});
  });
  await check('Installed partial release can be reinstalled with its own warning',async()=>{
   rows.find(r=>r.installed).compatibility={status:'incompatible',limitations:['Проверенное ограничение установленной версии']};await load();
   await page.locator('#xray-version-select').selectOption('v26.3.27');assert(await page.locator('#xray-reinstall').isEnabled());
   await page.locator('#xray-reinstall').click();assert((await page.locator('#confirm-message').innerText()).includes('Проверенное ограничение установленной версии'));
   await page.locator('#confirm-cancel').click();assert.equal(posts.length,1);
  });
  await check('Mobile explanation remains visible without horizontal overflow',async()=>{
   await page.setViewportSize({width:390,height:844});await page.keyboard.press('Escape');
   await page.waitForFunction(()=>document.querySelector('.sidebar')?.getBoundingClientRect().right<=1);
   await page.locator('#xray-version-select').selectOption('v26.2.6');
   assert((await page.locator('#xray-selected-release-note').innerText()).includes('fragment'));
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
   await page.locator('#xray-selected-release-note').scrollIntoViewIfNeeded();await page.screenshot({path:path.join(out,'partial-mobile.png')});
  });
  await check('Insufficient space still blocks installation with a reason',async()=>{
   catalog.storage.freeBytes=1;await load();await page.locator('#xray-version-select').selectOption('v26.3.27');
   assert(await page.locator('#xray-update-install').isDisabled());assert((await page.locator('#xray-selected-release-note').innerText()).includes('Недостаточно'));
  });
  await check('Unverified official digest still blocks installation',async()=>{
   catalog.storage.freeBytes=1e9;rows[0].archiveSha256='';await load();await page.locator('#xray-version-select').selectOption(rows[0].tagName);
   assert(await page.locator('#xray-update-install').isDisabled());assert((await page.locator('#xray-selected-release-note').innerText()).includes('архив'));
  });
  assert.deepEqual(errors,[]);
 }catch(e){failure=String(e);await page.screenshot({path:path.join(out,'failure.png'),fullPage:true});}
 fs.writeFileSync(path.join(out,'RESULT.json'),JSON.stringify({status:failure?'FAIL':'PASS',failure,checks,errors,posts,scope:'production UI, mocked HTTP, no router'},null,2)+'\n');
 await browser.close();if(failure)throw Error(failure);
 console.log(JSON.stringify({status:'PASS',checks:checks.length}));
})().catch(e=>{console.error(e);process.exitCode=1;});
