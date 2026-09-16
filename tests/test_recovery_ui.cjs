/* Actual production page; local browser with mocked HTTP recovery outcomes. */
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.resolve(__dirname,'../runtime/app/web-new');
const out=path.resolve(__dirname,'../../docs/evidence/recovery-ui-20260916');
fs.mkdirSync(out,{recursive:false});
let scenario='complete',calls=0,paused=false;
const errors=[],checks=[],mutations=[];
(async()=>{
 const browser=await chromium.launch({headless:true});
 try {
 const page=await browser.newPage();page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/*',async route=>{
  const req=route.request(),u=new URL(req.url());
  if(u.hostname!=='broray.test')return route.abort();
  if(u.pathname.startsWith('/api/operations/')){
   const ep=path.basename(u.pathname,'.cgi');
   if(ep==='status')return route.fulfill({status:503,json:{ok:false,complete:false,operations:[],automationPaused:paused,globalFence:'ambiguous'}});
   if(ep==='events')return route.fulfill({json:{ok:true,complete:true,events:[]}});
   assert.equal(ep,'recover');assert.equal(req.method(),'POST');assert.equal(req.headers()['x-broray-request'],'operations');assert.deepEqual(req.postDataJSON(),{});
   calls++;mutations.push({scenario,endpoint:ep});
   if(scenario==='unavailable')return route.fulfill({status:503,json:{ok:false,errorCode:'STATE_UNAVAILABLE'}});
   paused=true;
   if(scenario==='complete'&&calls>=3)return route.fulfill({status:202,json:{ok:true,result:'recovered',automationPaused:true,retryable:false}});
   const reason={complete:'ACTIVE',timeout:'children_unconfirmed',legacy:'legacy_owner_ambiguous',protected:'protected_recovery',updater:'updater_pending',ambiguous:'AMBIGUOUS'}[scenario];
   return route.fulfill({status:409,json:{ok:false,result:reason,automationPaused:true,errorCode:'RECOVERY_BLOCKED',retryable:['complete','timeout'].includes(scenario)}});
  }
  if(u.pathname.startsWith('/api/'))return route.fulfill({json:{success:true,data:u.pathname.includes('session')?{user:'fixture'}:{version:'3.1.1',components:[],protocols:[],capabilities:[],running:false}}});
  const file=path.resolve(root,'.'+u.pathname);
  if(!file.startsWith(root+path.sep)||!fs.existsSync(file))return route.fulfill({status:404,body:''});
  return route.fulfill({body:fs.readFileSync(file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]||'application/octet-stream'});
 });
 await page.goto('http://broray.test/broray.html');
 for(const [name,wanted,count] of [
  ['complete','Автоматика остаётся на паузе.',3],['timeout','Завершение дочерних задач',6],
  ['legacy','У старой блокировки',1],['protected','незавершённое изменение',1],
  ['updater','незавершённое обновление',1],['ambiguous','Не удалось подтвердить владельца',1],
  ['unavailable','Восстановление не подтверждено.',1]]){
  scenario=name;calls=0;paused=false;
  await page.locator('#bg-recover').waitFor({state:'visible'});
  await page.click('#bg-recover');
  await page.waitForFunction(text=>document.querySelector('#bg-feedback').textContent.includes(text),wanted);
  await page.waitForFunction(()=>!document.querySelector('#bg-recover').disabled);
  assert.equal(calls,count,name);
  const feedback=await page.locator('#bg-feedback').textContent();
  if(name==='unavailable')assert(!feedback.includes('Автоматика на паузе'));
  if(name==='complete')assert(!feedback.includes('Блокировка сохранена'));
  else assert(feedback.includes('Блокировка сохранена'));
  checks.push(name);
 }
 scenario='legacy';calls=0;await page.click('#bg-recover');
 await page.waitForFunction(()=>document.querySelector('#bg-feedback').textContent.includes('У старой блокировки'));
 for(const width of [1440,360]){
  await page.setViewportSize({width,height:1000});
  await page.locator('#bg-feedback').scrollIntoViewIfNeeded();
  const fits=await page.locator('#bg-feedback').evaluate(el=>el.scrollWidth<=el.clientWidth+1);
  assert(fits,`feedback overflows at ${width}`);
  await page.screenshot({path:path.join(out,`recovery-${width}.png`),animations:'disabled'});
 }
 assert.deepEqual(errors,[]);
 fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({status:'PASS',checks,errors,mutations,environment:'Production page in Chromium; mocked HTTP, no router'},null,2)+'\n');
 console.log(JSON.stringify({status:'PASS',checks:checks.length}));
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exit(1);});
