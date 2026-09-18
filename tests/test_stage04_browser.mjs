/** Real Chromium + baseline page/CSS/scripts; HTTP replaced by fixtures. No router. */
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import {fileURLToPath,pathToFileURL} from 'node:url';
const root=process.env.BRORAY_STAGE_ROOT || path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..','..');
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE ? pathToFileURL(process.env.PLAYWRIGHT_MODULE).href : 'playwright');
const evidence=path.join(root,'evidence');fs.mkdirSync(evidence,{recursive:true});
const browser=await chromium.launch({headless:true});const results=[];
function fixture(){
 const servers=[['google-primary','8.8.8.8','dns.google'],['cloudflare-primary','1.1.1.1','cloudflare-dns.com'],['quad9','9.9.9.9','dns.quad9.net']].map(([id,address,sni])=>({id,name:id,provider:'QA',address,sni,port:853,effectivePort:853,spki:'',interface:'',domain:'',present:true,test:{ok:true,status:'ok',testedEpoch:Math.floor(Date.now()/1000),latencyMs:0}}));
 const dot=servers.map(s=>({...s,valid:true,unknownTokenCount:0,deleteEligible:true,portRaw:'853',portState:'explicit',classification:'catalog:'+s.id,catalogMatchIds:[s.id],ownership:s.id==='google-primary'?'external':'broray'}));
 return {servers,actual:{dot,determinate:true,dohCount:0,totalSecure:3},selectedIds:['google-primary','cloudflare-primary'],requestedIds:['google-primary','cloudflare-primary'],deleteEligible:true,writeProtocolEnabled:true,mutationAvailable:true,runningConfigAvailable:true,installationState:'installed',matchesSelection:true,testAvailable:true,maxServers:8,managed:[],managedPresentCount:0,externalDotCount:3};
}
async function setup(options={}){
 const context=await browser.newContext({viewport:{width:options.mobile?390:1440,height:options.mobile?844:1000}});const page=await context.newPage();let state=fixture(),forces=0;const posts=[],errors=[];page.on('pageerror',e=>errors.push(e.message));
 await context.route('**/*',async route=>{
  const u=new URL(route.request().url()),req=route.request();
  if(u.hostname!=='broray-qa.invalid')return route.fulfill({status:204,body:''});
  if(u.pathname==='/api/session.cgi')return route.fulfill({json:{user:'fixture-admin'}});
  if(u.pathname==='/api/routes/dot-status.cgi'){
   if(u.searchParams.has('force')){forces++;if(options.forceFailure && forces===1)return route.fulfill({status:502,json:{success:false,error:{code:'KEENETIC_UNAVAILABLE',message:'fixture read failure'}}});if(options.change && forces===2)state.actual.dot[0].sni='changed.invalid';if(options.unauth && forces===1)return route.fulfill({status:401,json:{error:{code:'AUTH_REQUIRED',message:'expired fixture'}}});}
   return route.fulfill({json:{success:true,data:state}});
  }
  if(u.pathname==='/api/routes/dot-delete-preview.cgi'){
   const ids=state.selectedIds.slice().sort(),entries=state.actual.dot.filter(x=>ids.includes(x.id)).map(x=>({id:x.id,address:x.address,effectivePort:x.effectivePort,sni:x.sni,spki:x.spki||'',interface:x.interface||x.on||'',domain:x.domain||''})).sort((a,b)=>a.id.localeCompare(b.id));
   return route.fulfill({json:{success:true,data:{schemaVersion:1,expectedFingerprint:'a'.repeat(64),serverIds:ids,entries}}});
  }
  if(u.pathname==='/api/routes/dot-delete.cgi'){
   posts.push({url:u.pathname,method:req.method(),body:req.postData()});
   if(options.backendStale)return route.fulfill({status:409,json:{success:false,error:{code:'DOT_DELETE_CONFIRMATION_STALE',message:'Список DNS-over-TLS изменился после подтверждения. Ничего не удалено.'}}});
   state.selectedIds=[];state.requestedIds=[];state.actual.dot=state.actual.dot.slice(2);state.deleteEligible=false;state.matchesSelection=false;state.servers.forEach(s=>s.present=s.id==='quad9');return route.fulfill({json:{success:true,data:state}});
  }
  if(u.pathname==='/api/routes/dot-test.cgi'){
   state.servers[0].test={ok:false,status:'failed',testedEpoch:Math.floor(Date.now()/1000),latencyMs:0};return route.fulfill({json:{success:true,data:state}});
  }
  if(u.pathname==='/')return route.fulfill({status:200,contentType:'text/html',body:'<!doctype html><title>QA login destination</title>'});
  if(u.pathname.startsWith('/api/'))return route.fulfill({json:{success:true,data:{}}});
  const name=u.pathname==='/'?'index.html':u.pathname.slice(1);if(name.includes('..'))return route.abort();
  const rel='runtime/app/web-new/'+name;const file=[path.join(root,'files',rel),path.join(root,'baseline',rel)].find(x=>fs.existsSync(x));
  if(!file)return route.fulfill({status:204,body:''});
  const ext=path.extname(file);return route.fulfill({body:fs.readFileSync(file),contentType:ext==='.js'?'text/javascript; charset=utf-8':ext==='.css'?'text/css; charset=utf-8':ext==='.html'?'text/html; charset=utf-8':'application/octet-stream'});
 });
 await page.goto('http://broray-qa.invalid/dns.html');await page.waitForFunction(()=>document.querySelector('#dns-server-list')?.children.length===3);await page.locator('#dns-delete').waitFor();
 return {page,context,posts,errors,forceCount:()=>forces};
}
async function check(name,fn,options={}){const h=await setup(options);try{await fn(h);assert.deepEqual(h.errors,[]);results.push({name,status:'PASS'});}catch(e){results.push({name,status:'FAIL',error:e.message});await h.page.screenshot({path:path.join(evidence,'browser-failure-'+results.length+'.png'),fullPage:true});throw e;}finally{await h.context.close();fs.writeFileSync(path.join(evidence,'browser-results.json'),JSON.stringify({tests:results,browser:browser.version(),routerAccessed:false,http:'intercepted synthetic fixtures'},null,2));}}
try {
 await check('label, duration and native details keyboard',async({page:p})=>{assert.equal(await p.locator('#dns-delete').innerText(),'Удалить выбранные DoT-записи');assert.equal(await p.locator('details[open]').count(),0);assert.match(await p.locator('#dns-server-list').innerText(),/< 1 с/);assert.doesNotMatch(await p.locator('#dns-server-list').innerText(),/0 мс/);await p.locator('#dns-technical-details summary').focus();await p.keyboard.press('Enter');assert.equal(await p.locator('#dns-technical-details').getAttribute('open'),'');});
 await check('exact selected records and cancel sends no mutation',async({page:p,posts,forceCount})=>{await p.locator('#dns-delete').click();await p.locator('#confirm-root').waitFor({state:'visible'});const msg=await p.locator('#confirm-message').innerText();assert.match(msg,/8\.8\.8\.8:853/);assert.match(msg,/1\.1\.1\.1:853/);assert.doesNotMatch(msg,/9\.9\.9\.9/);assert.match(msg,/независимо/);await p.locator('#confirm-cancel').click();await p.waitForFunction(()=>!document.querySelector('#dns-refresh').disabled);assert.equal(posts.length,0);assert.equal(forceCount(),1);});
 await check('unsaved checkboxes block deletion and explain persistence',async({page:p,posts})=>{await p.getByRole('checkbox',{name:'Выбрать google-primary',exact:true}).uncheck();assert.ok(await p.locator('#dns-delete').isDisabled());assert.match(await p.locator('#dns-delete-hint').innerText(),/Проверить выбранные/);assert.equal(posts.length,0);});
 await check('confirmed server preview posts bound fingerprint once and resets saved selection',async({page:p,posts,forceCount})=>{await p.locator('#dns-delete').click();await p.locator('#confirm-accept').click();await p.waitForFunction(()=>document.querySelector('#dns-selected').textContent==='0');assert.equal(posts.length,1);assert.equal(posts[0].method,'POST');const body=JSON.parse(posts[0].body);assert.equal(body.schemaVersion,1);assert.deepEqual(body.serverIds,['cloudflare-primary','google-primary']);assert.equal(body.expectedFingerprint,'a'.repeat(64));assert.equal(forceCount(),1);});
 await check('backend stale confirmation fails closed and preserves selection',async({page:p,posts})=>{await p.locator('#dns-delete').click();await p.locator('#confirm-accept').click();await p.locator('#dns-feedback').waitFor({state:'visible'});assert.match(await p.locator('#dns-feedback').innerText(),/изменились после подтверждения/);assert.equal(posts.length,1);assert.equal(await p.locator('#dns-selected').innerText(),'2');},{backendStale:true});
 await check('failed read before preview is read-only and unlocks controls',async({page:p,posts})=>{await p.locator('#dns-delete').click();await p.locator('#dns-feedback').waitFor({state:'visible'});assert.match(await p.locator('#dns-feedback').innerText(),/не отправлен/);await p.waitForFunction(()=>!document.querySelector('#dns-refresh').disabled);assert.equal(posts.length,0);},{forceFailure:true});
 await check('401 before deletion redirects without POST',async({page:p,posts})=>{await p.locator('#dns-delete').click();await p.waitForURL(u=>u.pathname==='/');assert.equal(posts.length,0);},{unauth:true});
 await check('failed TLS test reported as error and warning not success',async({page:p})=>{await p.locator('#dns-test').click();await p.waitForFunction(()=>document.querySelector('#toast-root').textContent.includes('подтверждено 1 из 2'));assert.match(await p.locator('#dns-server-list').innerText(),/TLS\/SNI: ошибка/);});
 await check('error details stay text, refresh is not automatic or parallel',async({page:p,posts})=>{await p.evaluate(()=>{window.__qaCalls=0;window.BROrayActionFeedback.show('qa-feedback',window.BROrayActionFeedback.describe({code:'QA',message:'<img src=x onerror=alert(1)>',details:'<script>bad()</script>'},'preflight'),()=>{window.__qaCalls++;return new Promise(resolve=>setTimeout(resolve,100));});});assert.equal(await p.locator('#qa-feedback img,#qa-feedback script').count(),0);assert.equal(await p.evaluate(()=>window.__qaCalls),0);await p.locator('#qa-feedback details summary').click();assert.match(await p.locator('#qa-feedback pre').innerText(),/<script>/);await p.locator('#qa-feedback button').click();assert.ok(await p.locator('#qa-feedback button').isDisabled());assert.equal(await p.evaluate(()=>window.__qaCalls),1);assert.equal(posts.length,0);});
 for(const mobile of [false,true])await check('render and modal wrap '+(mobile?'mobile':'desktop'),async({page:p})=>{assert.ok(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));await p.screenshot({path:path.join(evidence,'dns-'+(mobile?'mobile':'desktop')+'.png'),fullPage:true});await p.locator('#dns-delete').click();await p.locator('#confirm-root').waitFor({state:'visible'});assert.ok(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));await p.screenshot({path:path.join(evidence,'dns-confirm-'+(mobile?'mobile':'desktop')+'.png'),fullPage:true});},{mobile});
 console.log(JSON.stringify({passed:results.length,failed:0,browser:browser.version(),networkAccess:false}));
}finally{await browser.close();}
