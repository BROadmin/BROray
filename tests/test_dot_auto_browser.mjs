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
 state.autoCheck={schemaVersion:1,enabled:options.auto===true,settingsValid:true,paused:options.paused===true,intervalSeconds:300,savedIds:state.selectedIds,last:{}};
 if(options.old)state.servers.forEach(s=>s.test.testedEpoch-=700);
 if(options.failed)state.servers[0].test={...state.servers[0].test,ok:false,status:'failed'};
 await context.route('**/*',async route=>{
  const u=new URL(route.request().url()),req=route.request();
  if(u.hostname!=='broray-qa.invalid')return route.fulfill({status:204,body:''});
  if(u.pathname==='/api/session.cgi')return route.fulfill({json:{user:'fixture-admin'}});
  if(u.pathname==='/api/routes/dot-auto-settings.cgi'){
   assert.equal(req.headers()['x-broray-request'],'operations');
   posts.push({url:u.pathname,method:req.method(),body:req.postData()});
   if(options.saveFailure)return route.fulfill({status:409,json:{success:false,error:{code:'ROUTES_OPERATION_BUSY',message:'Busy fixture'}}});
   state.autoCheck.enabled=JSON.parse(req.postData()).enabled;return route.fulfill({json:{success:true,data:state.autoCheck}});
  }
  if(u.pathname==='/api/routes/dot-status.cgi'){
   if(u.searchParams.has('force')){forces++;if(options.forceFailure && forces===1)return route.fulfill({status:502,json:{success:false,error:{code:'KEENETIC_UNAVAILABLE',message:'fixture read failure'}}});if(options.change && forces===2)state.actual.dot[0].sni='changed.invalid';if(options.unauth && forces===1)return route.fulfill({status:401,json:{error:{code:'AUTH_REQUIRED',message:'expired fixture'}}});}
   return route.fulfill({json:{success:true,data:state}});
  }
  if(u.pathname==='/api/routes/dot-delete.cgi'){
   posts.push({url:u.pathname,method:req.method(),body:req.postData()});state.selectedIds=[];state.requestedIds=[];state.actual.dot=state.actual.dot.slice(2);state.deleteEligible=false;state.matchesSelection=false;state.servers.forEach(s=>s.present=s.id==='quad9');return route.fulfill({json:{success:true,data:state}});
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
 return {page,context,posts,errors,forceCount:()=>forces,fresh:()=>state.servers.forEach(s=>s.test={ok:true,status:"ok",testedEpoch:Math.floor(Date.now()/1000),latencyMs:0})};
}
async function check(name,fn,options={}){const h=await setup(options);try{await fn(h);assert.deepEqual(h.errors,[]);results.push({name,status:'PASS'});}catch(e){results.push({name,status:'FAIL',error:e.message});await h.page.screenshot({path:path.join(evidence,'browser-failure-'+results.length+'.png'),fullPage:true});throw e;}finally{await h.context.close();fs.writeFileSync(path.join(evidence,'browser-results.json'),JSON.stringify({tests:results,browser:browser.version(),routerAccessed:false,http:'intercepted synthetic fixtures'},null,2));}}
try {
 await check('off by default, page issues no modifying requests',async({page:p,posts})=>{assert.equal(await p.locator('#dns-auto-check').isChecked(),false);assert.equal(posts.length,0);assert.match(await p.locator('#dns-auto-hint').innerText(),/выключена/);});
 await check('keyboard enable saves setting only',async({page:p,posts})=>{await p.locator('#dns-auto-check').focus();await p.keyboard.press('Space');await p.waitForFunction(()=>document.querySelector('#dns-auto-check').checked&&!document.querySelector('#dns-auto-check').disabled);assert.equal(posts.length,1);assert.deepEqual(JSON.parse(posts[0].body),{enabled:true});assert.match(posts[0].url,/dot-auto-settings/);assert.equal(await p.getByRole('checkbox',{name:'Выбрать google-primary',exact:true}).isChecked(),true);});
 await check('disable sends no DNS writes',async({page:p,posts})=>{await p.locator('#dns-auto-check').uncheck();await p.waitForFunction(()=>!document.querySelector('#dns-auto-check').disabled);assert.deepEqual(JSON.parse(posts[0].body),{enabled:false});assert.equal(posts.length,1);},{auto:true});
 await check('failed save restores previous toggle',async({page:p,posts})=>{await p.locator('#dns-auto-check').check();await p.waitForFunction(()=>!document.querySelector('#dns-auto-check').disabled);assert.equal(await p.locator('#dns-auto-check').isChecked(),false);assert.equal(posts.length,1);},{saveFailure:true});
 await check('unsaved server choice not overwritten by settings',async({page:p,posts})=>{await p.getByRole('checkbox',{name:'Выбрать google-primary',exact:true}).uncheck();await p.locator('#dns-auto-check').check();await p.waitForFunction(()=>!document.querySelector('#dns-auto-check').disabled);assert.equal(await p.getByRole('checkbox',{name:'Выбрать google-primary',exact:true}).isChecked(),false);assert.match(await p.locator('#dns-auto-hint').innerText(),/сохранённый выбор/);assert.deepEqual(JSON.parse(posts[0].body),{enabled:true});});
 await check('old successful check waits, does not masquerade as fresh',async({page:p})=>{assert.match(await p.locator('#dns-server-list').innerText(),/Ожидает перепроверки/);assert.match(await p.locator('#dns-notice').getAttribute('class'),/status-neutral/);assert.equal(await p.locator('#dns-tested').innerText(),'0 из 2');assert.ok(await p.locator('#dns-apply').isDisabled());},{auto:true,old:true});
 await check('old failed check stays an error',async({page:p})=>{assert.match(await p.locator('#dns-server-list').innerText(),/TLS\/SNI: ошибка/);assert.match(await p.locator('#dns-notice').getAttribute('class'),/status-error/);},{auto:true,old:true,failed:true});
 await check('pause shown explicitly',async({page:p})=>{assert.match(await p.locator('#dns-auto-hint').innerText(),/общей паузой/);},{auto:true,paused:true});
 await check('refresh displays new result without issuing test',async({page:p,posts,fresh})=>{fresh();await p.locator('#dns-refresh').click();await p.waitForFunction(()=>document.querySelector('#dns-tested').textContent==='2 из 2');assert.equal(posts.length,0);},{auto:true,old:true});
 await check('mobile control fits available width',async({page:p})=>{assert.ok(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));await p.screenshot({path:path.join(evidence,'dot-auto-mobile.png'),fullPage:true});},{mobile:true,auto:true});
 console.log(JSON.stringify({passed:results.length,failed:0,routerAccessed:false}));
} finally {await browser.close();}
