// Production WebUI, synthetic HTTP data; no physical-router claims.
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path'),assert=require('assert/strict');
const root=path.resolve(__dirname,'../runtime/app/web-new'),out=process.env.BRORAY_BROWSER_OUTPUT;
fs.mkdirSync(out,{recursive:false});
const rid=i=>'q-'+i.toString(16).padStart(32,'0');
let fail=false,cancels=0,checks=0;
let rows=Array.from({length:25},(_,i)=>({requestId:rid(i+1),priority:3,type:'unknown',source:'UNKNOWN',stage:'finished',state:i===3?'failed':'completed'}));
let ops=rows.map((r,i)=>({requestId:r.requestId,operationId:'op-q-'+(i+1).toString(16).padStart(32,'0'),type:'server_operation',source:'SERVER_CHECK_AUTO',state:r.state,running:false,finishedAt:new Date(Date.UTC(2026,8,27,0,i)).toISOString(),startedAt:new Date(Date.UTC(2026,8,27,0,i)).toISOString()}));
rows.push({requestId:rid(99),priority:2,type:'subscription_update',source:'USER',state:'queued',reason:'awaiting_resource'});
const errors=[];
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.BRORAY_CHROMIUM});
 const page=await browser.newPage({viewport:{width:390,height:844}});page.on('pageerror',e=>errors.push(e.message));
 try{
  await page.route('**/*',async route=>{
   const req=route.request(),u=new URL(req.url());if(u.hostname!=='broray.test')return route.abort();
   if(u.pathname==='/api/operations/status.cgi')return route.fulfill({status:fail?503:200,json:fail?{ok:false,errorCode:'STATE_UNAVAILABLE'}:{ok:true,complete:true,automationPaused:false,globalFence:'absent',operations:ops,queue:rows}});
   if(u.pathname==='/api/operations/events.cgi')return route.fulfill({json:{ok:true,complete:true,events:[]}});
   if(u.pathname==='/api/operations/cancel.cgi'){assert.equal(req.postDataJSON().requestId,rid(99));cancels++;rows[rows.length-1].state='cancelled';return route.fulfill({status:202,json:{ok:true,state:'cancelled'}});}
   if(u.pathname.startsWith('/api/'))return route.fulfill({json:{success:true,data:u.pathname.includes('session')?{user:'fixture'}:{version:'3.2.0',components:[],protocols:[],capabilities:[]}}});
   const file=path.resolve(root,'.'+u.pathname);if(!file.startsWith(root+path.sep)||!fs.existsSync(file))return route.fulfill({status:404});
   return route.fulfill({body:fs.readFileSync(file),contentType:({'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'})[path.extname(file)]||'application/octet-stream'});
  });
  await page.goto('http://broray.test/broray.html');await page.locator('#bg-list .bg-operation').first().waitFor();
  assert.equal(await page.locator('#bg-journal').count(),1,'Journal must be a disclosure');
  assert(await page.locator('#bg-download').isHidden());checks++;
  await page.locator('#bg-journal > summary').click();assert(await page.locator('#bg-download').isVisible());
  await page.locator('#bg-journal-refresh').click();assert.notEqual(await page.locator('#bg-journal').getAttribute('open'),null);checks++;
  await page.locator('#bg-journal > summary').focus();await page.keyboard.press('Enter');assert(await page.locator('#bg-download').isHidden());checks++;
  const gaps=await page.locator('.bg-card').evaluateAll(cards=>cards.map(el=>{const n=el.nextElementSibling;return n&&n.checkVisibility()?n.getBoundingClientRect().top-el.getBoundingClientRect().bottom:18;}));
  assert(gaps.every(g=>g>=16),'Adjacent background cards must have spacing');checks++;
  assert.equal(await page.locator('#bg-list .bg-operation').count(),1,'Finished operations must not expand current work');checks++;
  assert.equal(await page.locator('#bg-history').count(),1);assert.equal(await page.locator('#bg-history').getAttribute('open'),null);checks++;
  assert.match(await page.locator('#bg-history-summary').innerText(),/25.*1/);checks++;
  await page.locator('#bg-history-summary').click();assert.equal(await page.locator('#bg-history-list .bg-operation').count(),10);checks++;
  assert.equal(await page.locator('#bg-history-list .bg-operation').first().getAttribute('data-request-id'),rid(25));
  assert.match(await page.locator('#bg-history-list').innerText(),/Автопроверка серверов/);checks++;
  await page.locator('#bg-history-more').click();assert.equal(await page.locator('#bg-history-list .bg-operation').count(),20);checks++;
  await page.locator('#bg-refresh').click();await page.waitForTimeout(100);assert.notEqual(await page.locator('#bg-history').getAttribute('open'),null);assert.equal(await page.locator('#bg-history-list .bg-operation').count(),20);checks++;
  await page.locator('#bg-history-more').click();assert.equal(await page.locator('#bg-history-list .bg-operation').count(),25);assert(await page.locator('#bg-history-more').isHidden());checks++;
  fail=true;await page.locator('#bg-refresh').click();await page.waitForFunction(()=>document.querySelector('#bg-badge').textContent.includes('проверить'));
  assert.equal(await page.locator('#bg-history-list .bg-operation').count(),25);assert(await page.locator('#bg-list button').isDisabled());checks++;
  fail=false;await page.locator('#bg-refresh').click();await page.waitForFunction(()=>!document.querySelector('#bg-list button').disabled);
  await page.locator('#bg-list button').evaluate(b=>{b.click();b.click();});await page.waitForFunction(()=>document.querySelector('#bg-history-summary').textContent.includes('26'));
  assert.equal(cancels,1);assert.equal(await page.locator('#bg-list .bg-operation').count(),0);checks++;
  for(const width of [360,390,1435]){await page.setViewportSize({width,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);checks++;}
  rows=[];ops=[];await page.locator('#bg-refresh').click();await page.waitForFunction(()=>document.querySelector('#bg-history').hidden);checks++;
  assert.deepEqual(errors,[]);fs.writeFileSync(path.join(out,'RESULT.json'),JSON.stringify({status:'PASS',checks,errors,scope:'HTTP fixture only'}));console.log(JSON.stringify({status:'PASS',checks}));
 }catch(e){await page.screenshot({path:path.join(out,'failure.png'),fullPage:true});fs.writeFileSync(path.join(out,'RESULT.json'),JSON.stringify({status:'FAIL',checks,error:String(e),errors}));throw e;}
 finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
