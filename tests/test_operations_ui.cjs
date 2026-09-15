/* Full production page, local files, browser fetch mocked at the HTTP boundary. */
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.resolve(__dirname,'../runtime/app/web-new');
const out=path.resolve(__dirname,'../../docs/evidence/operations-ui-20260915');fs.mkdirSync(out,{recursive:true});
const operation={operationId:'op-20260915123400-3456-012345abcdef',type:'server_operation',source:'SERVER_CHECK_AUTO',phase:'checking',state:'running',running:true,revision:1,cancelability:'cooperative',cancelRequested:false,startedAt:'2026-09-15T12:34:00Z',ownerStatus:'ACTIVE'};
let scenario='checking',paused=false,cancelled=false,checks=0;
function snapshot(){
  const op={...operation,cancelRequested:cancelled};
  if(scenario==='fetching'){op.type='subscription_update';op.phase='fetching';op.source='SUBSCRIPTION_AUTO';}
  if(scenario==='protected'){op.phase='committing';op.cancelability='protected';}
  if(scenario==='ambiguous'){op.ownerStatus='AMBIGUOUS';}
  return {ok:scenario!=='ambiguous',complete:scenario!=='ambiguous',operations:scenario==='idle'?[]:[op],automationPaused:paused,globalFence:scenario==='idle'?'absent':scenario==='ambiguous'?'ambiguous':'managed_active'};
}
(async()=>{
 const browser=await chromium.launch({headless:true}),context=await browser.newContext({acceptDownloads:true}),page=await context.newPage();
 const errors=[],mutations=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/*',async route=>{
  const req=route.request(),u=new URL(req.url());if(u.hostname!=='broray.test')return route.abort();
  if(u.pathname.startsWith('/api/operations/')){
   const endpoint=path.basename(u.pathname,'.cgi');
   if(endpoint==='status')return route.fulfill({status:scenario==='unavailable'||scenario==='ambiguous'?503:200,json:scenario==='unavailable'?{ok:false,errorCode:'STATE_UNAVAILABLE'}:snapshot()});
   if(endpoint==='events')return route.fulfill({json:{ok:true,complete:true,events:[{timestamp:'2026-09-15T12:34:00Z',event:'started',message:'<script>SECRET_RAW_MESSAGE</script>'}]}});
   if(endpoint==='report')return route.fulfill({json:{schemaVersion:1,reportKind:'broray-diagnostics',complete:false,operations:snapshot().operations,events:[]}});
   assert.equal(req.method(),'POST');assert.equal(req.headers()['x-broray-request'],'operations');
   const body=req.postDataJSON();mutations.push({endpoint,body});
   if(endpoint==='cancel')cancelled=true;
   if(endpoint==='stop-background'){paused=true;cancelled=true;}
   if(endpoint==='automation')paused=body.paused;
   if(endpoint==='recover')return route.fulfill({status:503,json:{ok:false,result:'AMBIGUOUS'}});
   return route.fulfill({status:202,json:{ok:true,cancelRequested:true,automationPaused:paused}});
  }
  if(u.pathname.startsWith('/api/'))return route.fulfill({json:{success:true,data:u.pathname.includes('session')?{user:'fixture'}:{version:'3.1.0',components:[],protocols:[],capabilities:[],running:false}}});
  const file=path.resolve(root,'.'+u.pathname);
  if(!file.startsWith(root+path.sep)||!fs.existsSync(file))return route.fulfill({status:404,body:''});
  const type={'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml','.woff2':'font/woff2'}[path.extname(file)]||'application/octet-stream';
  return route.fulfill({body:fs.readFileSync(file),contentType:type});
 });
 await page.goto('http://broray.test/broray.html');await page.locator('#bg-list .bg-operation').waitFor();
 for(const width of [1440,1024,390,360])for(const theme of ['broray','day']){
  await page.setViewportSize({width,height:1100});await page.evaluate(t=>document.documentElement.setAttribute('data-theme',t),theme);
  for(scenario of ['checking','fetching','idle','ambiguous','protected','unavailable']){
   const response=page.waitForResponse(r=>r.url().endsWith('/operations/status.cgi'));
   await page.click('#bg-refresh');await response;
   await page.waitForFunction(s=>{
    const t=document.querySelector('#bg-badge').textContent;
    if(s==='ambiguous'||s==='unavailable')return t.includes('проверить');
    if(s==='idle')return t==='Нет операций';
    return document.querySelector('#bg-list dd:nth-of-type(1)')&&document.querySelector('#bg-list').textContent.includes(s==='protected'?'Сохранение':s==='fetching'?'Загрузка':'Проверка');
   },scenario);
   const bad=await page.locator('.bg-card').evaluateAll(cards=>cards.flatMap(card=>[card,...card.querySelectorAll('button:not([hidden]),.status-badge')]).filter(el=>el.getBoundingClientRect().width>0).filter(el=>{const r=el.getBoundingClientRect();return r.left<0||r.right>innerWidth+1||el.scrollWidth>el.clientWidth+1;}).map(el=>el.id||el.className));
   assert.deepEqual(bad,[],`${width}/${theme}/${scenario}`);checks++;
  }
  scenario='checking';await page.click('#bg-refresh');await page.waitForFunction(()=>document.querySelector('#bg-badge').textContent.includes('Выполняется'));
  await page.locator('#bg-title').scrollIntoViewIfNeeded();await page.screenshot({path:path.join(out,`${width}-${theme}.png`),animations:'disabled'});
 }
 await page.locator('#bg-list button').click();await page.waitForFunction(()=>document.querySelector('#bg-list button').textContent==='Остановка запрошена');assert(await page.locator('#bg-list button').isDisabled());checks++;
 await page.click('#bg-stop-all');await page.waitForFunction(()=>document.querySelector('#bg-automation-badge').textContent==='На паузе');checks++;
 await page.click('#bg-resume');await page.waitForFunction(()=>document.querySelector('#bg-automation-badge').textContent==='Включена');checks++;
 scenario='protected';await page.click('#bg-refresh');await page.waitForFunction(()=>document.querySelector('#bg-list').textContent.includes('защищённый'));assert.equal(await page.locator('#bg-list button').count(),0);checks++;
 scenario='ambiguous';await page.click('#bg-refresh');await page.locator('#bg-recover').waitFor({state:'visible'});await page.click('#bg-recover');await page.waitForFunction(()=>document.querySelector('#bg-feedback').textContent.includes('Блокировка сохранена'));checks++;
 assert(!(await page.textContent('#bg-events')).includes('SECRET_RAW_MESSAGE'));checks++;
 const promise=page.waitForEvent('download');await page.click('#bg-download');const download=await promise;await download.saveAs(path.join(out,'report.json'));assert.equal(JSON.parse(fs.readFileSync(path.join(out,'report.json'))).reportKind,'broray-diagnostics');checks++;
 assert.deepEqual(errors,[]);
 fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({status:'PASS',checks,environment:'Full production page + mocked HTTP responses; no router',errors,mutations},null,2)+'\n');
 console.log(JSON.stringify({status:'PASS',checks}));await browser.close();
})().catch(e=>{console.error(e);process.exit(1);});
