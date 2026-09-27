/* Real production page; HTTP fixture only. No physical router/network. */
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.resolve(__dirname,'../runtime/app/web-new');
const output=process.env.BRORAY_BROWSER_OUTPUT;
assert(output,'Fresh evidence directory required');fs.mkdirSync(output,{recursive:false});
const requestId='q-'+'c'.repeat(32), operationId='op-q-'+'d'.repeat(32);
let state='queued',reason='awaiting_resource',cancelCalls=0,checks=0;
const clientMode=process.env.BRORAY_BROWSER_MODE==='client';
const controlsMode=process.env.BRORAY_BROWSER_MODE==='controls';
const watchRaceMode=process.env.BRORAY_BROWSER_MODE==='watch-race';
let slowPending=false,statusFailed=false,finishSlow=false,releaseSlow=null,firstRequest=null,slowRequest=null;
let submissions=0,lookups=0,submittedNonce=null;
const admitted=[],queueRows=[];
const errors=[];
function snapshot(){
 return {ok:true,complete:true,automationPaused:reason==='automation_paused',globalFence:'absent',
  queue:[{requestId,priority:3,stage:'probe',state,reason,type:'server_operation',
   source:'SERVER_CHECK_AUTO',operationId:state==='running'?operationId:null}],
  operations:state==='running'?[{operationId,type:'server_operation',source:'SERVER_CHECK_AUTO',
   phase:'checking',state:'running',running:true,cancelability:'cooperative',cancelRequested:false,
   ownerStatus:'ACTIVE',resourceLocks:['background-prepare']}]:[]};
}
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.BRORAY_CHROMIUM});
 try{
  const context=await browser.newContext({viewport:{width:390,height:844}});
  const page=await context.newPage();
  page.on('pageerror',error=>errors.push(error.message));
  await page.context().route('**/*',async route=>{
   const req=route.request(),u=new URL(req.url());
   if(u.hostname!=='broray.test')return route.abort();
   if(watchRaceMode && u.pathname==='/api/servers/check.cgi'){
    const body=req.postDataJSON(),row={ok:true,accepted:true,requestId:'q-'+body.nonce,priority:2,state:'queued'};
    if(body.id==='first')firstRequest=row;
    else {slowRequest=row;slowPending=true;await new Promise(resolve=>{releaseSlow=resolve;});}
    return route.fulfill({status:202,json:row});
   }
   if(watchRaceMode && u.pathname==='/api/operations/status.cgi'){
    if(slowPending && !statusFailed){statusFailed=true;return route.fulfill({status:503,json:{ok:false,errorCode:'STATE_UNAVAILABLE'}});}
    return route.fulfill({json:{ok:true,complete:true,automationPaused:false,globalFence:'absent',operations:[],
     queue:[...(firstRequest?[firstRequest]:[]),...(slowRequest?[{...slowRequest,state:finishSlow?'completed':'queued'}]:[])]}});
   }
   if(u.pathname==='/api/operations/status.cgi'){
    if(req.method()==='POST'){
     if(controlsMode){
      const row=queueRows.find(item=>item.requestId==='q-'+req.postDataJSON().nonce);
      assert(row);return route.fulfill({json:{ok:true,...row}});
     }
     assert.equal(req.postDataJSON().nonce,submittedNonce);lookups++;
     return route.fulfill({json:{ok:true,requestId,state,priority:2}});
    }
    return route.fulfill({json:controlsMode?{...snapshot(),queue:queueRows,operations:[]}:snapshot()});
   }
   if(controlsMode && ['/api/servers/check.cgi','/api/subscriptions/refresh.cgi'].includes(u.pathname)){
    const body=req.postDataJSON();admitted.push({path:u.pathname,body,headers:req.headers()});
    if(req.headers()['x-broray-queue']!=='1')return route.fulfill({json:{success:true,data:{}}});
    const row={ok:true,accepted:true,requestId:'q-'+body.nonce,priority:2,state:'queued'};
    queueRows.push({...row,type:u.pathname.includes('subscriptions')?'subscription_update':'server_operation',
      source:'USER',stage:'probe',reason:'awaiting_resource'});
    return route.fulfill({status:202,json:row});
   }
   if(clientMode && u.pathname==='/api/servers/check.cgi'){
    assert.equal(req.headers()['x-broray-queue'],'1');
    assert.equal(req.headers()['x-broray-request'],'operations');
    submissions++;submittedNonce=req.postDataJSON().nonce;
    assert.match(submittedNonce,/^[0-9a-f]{32}$/);
    assert.equal(req.postDataJSON().id,'fixture');
    // Backend admitted this nonce, but the browser lost the POST response.
    return route.abort('failed');
   }
   if(u.pathname==='/api/operations/events.cgi')return route.fulfill({json:{ok:true,complete:true,events:[]}});
   if(u.pathname==='/api/operations/cancel.cgi'){
    assert.equal(req.method(),'POST');assert.equal(req.headers()['x-broray-request'],'operations');
    if(controlsMode){
     const body=req.postDataJSON(),row=queueRows.find(item=>item.requestId===body.requestId);
     assert(row);assert.equal(row.state,'queued');row.state='cancelled';cancelCalls++;
     return route.fulfill({status:202,json:{ok:true,requestId:row.requestId,state:'cancelled'}});
    }
    assert.deepEqual(req.postDataJSON(),{requestId});cancelCalls++;state='cancelled';
    return route.fulfill({status:202,json:{ok:true,requestId,state}});
   }
   if(u.pathname.startsWith('/api/')){
    let data=u.pathname.includes('session')?{ok:true,user:'fixture'}:{version:'3.2.0',components:[],protocols:[],capabilities:[],running:false};
    if(controlsMode && u.pathname==='/api/servers/summary.cgi'){
     const server={id:'fixture',name:'Fixture server',protocol:'vless',network:'raw',security:'tls',
      address:'example.invalid',port:443,active:true,quality:{status:'available',ping:20}};
     data={activeServer:server,servers:[server],total:1,available:1,unavailable:0,
      xrayRunning:true,socksActive:true,connectionState:'connected',autoSwitch:{enabled:false}};
    }
    if(controlsMode && u.pathname==='/api/subscriptions/list.cgi')data=[{id:'test',name:'Fixture subscription',displayUrl:'https://example.invalid/sub',
      enabled:true,autoUpdateEnabled:false,serversCount:1,updateIntervalMinutes:360,lastUpdateStatus:'never'}];
    if(controlsMode && u.pathname==='/api/subscriptions/summary.cgi')data={total:1,enabled:1,serversReceived:1};
    if(controlsMode && u.pathname==='/api/home/summary.cgi'){
     const health={severity:'ok',reasons:[]};
     data={health,errors:[],updatedAt:'2026-09-27T00:00:00Z',
      xray:{health,version:'26.9.9',configValid:true,socksActive:true},
      servers:{health,connectionState:'degraded',total:1,activeServer:{name:'Fixture server',quality:{ping:20,freshness:'fresh'}}},
      subscriptions:{health,total:1,enabled:1,serversReceived:1,lastUpdateStatus:'success'},
      dns:{health,selectedCount:1,maxServers:8,effectiveCount:1,managedPresentCount:1},
      routes:{health,installedBundles:0,availableBundles:10,updatesAvailableCount:0},
      keenetic:{health,interfaceDisplayName:'BROray fixture',link:true,connected:true,state:'up'},
      broray:{installationHealthy:true,version:'3.2.0',updateAvailable:false}};
    }
    if(controlsMode && u.pathname==='/api/routes/dot-status.cgi'){
     data={servers:[{id:'google-primary',name:'Google',provider:'Google',address:'8.8.8.8',sni:'dns.google',
       port:853,effectivePort:853,present:true,test:{ok:true,status:'ok',testedEpoch:Math.floor(Date.now()/1000),latencyMs:0}}],
      actual:{dot:[],determinate:true,dohCount:0,totalSecure:1},
      selectedIds:['google-primary'],requestedIds:['google-primary'],effectiveIds:['google-primary'],
      deleteEligible:false,writeProtocolEnabled:true,mutationAvailable:true,runningConfigAvailable:true,
      installationState:'installed',matchesSelection:true,testAvailable:true,maxServers:8,managed:[],managedPresentCount:1,externalDotCount:0,
      autoCheck:{schemaVersion:1,enabled:true,settingsValid:true,paused:false,intervalSeconds:300,savedIds:['google-primary'],last:{}}};
    }
    return route.fulfill({json:{success:true,data}});
   }
   const file=path.resolve(root,'.'+u.pathname);
   if(!file.startsWith(root+path.sep)||!fs.existsSync(file))return route.fulfill({status:404,body:''});
   return route.fulfill({body:fs.readFileSync(file),contentType:({'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml','.woff2':'font/woff2'})[path.extname(file)]||'application/octet-stream'});
  });
  await page.goto('http://broray.test/broray.html');
  if(watchRaceMode){
   await page.evaluate(async()=>{
    window.observations=[];
    await BROrayUI.followQueued('/api/servers/check.cgi','first',r=>observations.push(['first',r.state]));
    window.slowPromise=BROrayUI.followQueued('/api/servers/check.cgi','slow',r=>observations.push(['slow',r.state]));
   });
   await page.waitForFunction(()=>observations.some(r=>r[0]==='first'&&r[1]==='unknown'));
   assert.equal(await page.evaluate(()=>observations.filter(r=>r[0]==='slow').length),0,
    'Status failure must not settle an admission whose reply has not arrived');
   checks++;assert(releaseSlow);releaseSlow();
   await page.waitForFunction(()=>observations.some(r=>r[0]==='slow'&&r[1]==='queued'));
   finishSlow=true;
   await page.waitForFunction(()=>observations.some(r=>r[0]==='slow'&&r[1]==='completed'));
   checks++;assert.deepEqual(errors,[]);
   const result={status:'PASS',checks,errors,observations:await page.evaluate(()=>observations),environment:'Production client; concurrent admission plus status503 fixture'};
   fs.writeFileSync(path.join(output,'RESULT.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));return;
  }
  if(controlsMode){
   await page.goto('http://broray.test/servers.html');
   const siblings=[];
   for(const name of ['home','dns','broray']){
    const tab=await context.newPage();tab.on('pageerror',error=>errors.push(error.message));
    await tab.goto('http://broray.test/'+name+'.html');siblings.push({name,tab});
   }
   await siblings[0].tab.locator('[data-module="dns"]').waitFor();
   await siblings[1].tab.waitForFunction(()=>document.querySelector('#dns-auto-check').checked);
   assert(await siblings[1].tab.locator('#dns-auto-check').isChecked());
   const active=page.locator('#check-active-server');
   await active.waitFor();await active.evaluate(el=>{el.click();el.click();});
   await page.waitForFunction(()=>document.querySelector('#check-active-server').textContent.includes('В очереди'));
   assert.equal(admitted.length,1);assert.equal(admitted[0].body.id,'fixture');checks++;
   await page.locator('#check-all-servers').evaluate(el=>{el.click();el.click();});
   await page.waitForFunction(()=>document.querySelector('#check-all-servers').textContent.includes('В очереди'));
   assert.equal(admitted.length,2);assert.equal(admitted[1].body.id,'all');checks++;
   const subscriptionPage=await page.context().newPage();subscriptionPage.on('pageerror',error=>errors.push(error.message));
   await subscriptionPage.goto('http://broray.test/subscriptions.html');
   await subscriptionPage.locator('[data-action="refresh"]').evaluate(el=>{el.click();el.click();});
   await subscriptionPage.waitForFunction(()=>document.querySelector('[data-action="refresh"]').textContent.includes('В очереди'));
   assert.equal(admitted.length,3);assert.equal(admitted[2].body.id,'test');checks++;
   for(const item of admitted){assert.equal(item.headers['x-broray-queue'],'1');assert.match(item.body.nonce,/^[0-9a-f]{32}$/);}
   assert(!(await page.textContent('body')).includes('Проверка сервера завершена.'));
   await page.reload();await subscriptionPage.reload();
   await page.waitForFunction(()=>document.querySelector('#check-active-server').textContent.includes('В очереди') &&
     document.querySelector('#check-all-servers').textContent.includes('В очереди'));
   await subscriptionPage.waitForFunction(()=>document.querySelector('[data-action="refresh"]').textContent.includes('В очереди'));
   assert.equal(admitted.length,3);checks++;
   await page.evaluate(()=>{
    window.queueCompletionToasts=0;const original=BROrayUI.toast;
    BROrayUI.toast=function(message,...args){if(message==='Проверка сервера завершена.')window.queueCompletionToasts++;return original(message,...args);};
   });
   await page.locator('#refresh-servers').click();
   await page.waitForFunction(()=>!document.querySelector('#refresh-servers').disabled && document.querySelector('#check-active-server').textContent.includes('В очереди'));
   await siblings[2].tab.click('#bg-refresh');
   await siblings[2].tab.waitForFunction(()=>document.querySelector('#bg-list').textContent.includes('В очереди'));
   assert.equal(await siblings[2].tab.locator('#bg-list .bg-operation').count(),3);checks++;
   await siblings[2].tab.locator('#bg-title').scrollIntoViewIfNeeded();
   await siblings[2].tab.screenshot({path:path.join(output,'five-pages-queue-mobile.png')});
   await subscriptionPage.locator('[data-action="cancel-update"]').click();
   await subscriptionPage.waitForFunction(()=>!document.querySelector('[data-action="refresh"]').disabled);
   assert.equal(cancelCalls,1);assert.equal(queueRows.filter(row=>row.state==='cancelled').length,1);checks++;
   queueRows.forEach(row=>{if(row.state==='queued')row.state='completed';row.stage='finished';row.reason=null;});
   await page.waitForFunction(()=>!document.querySelector('#check-active-server').disabled &&
      !document.querySelector('#check-all-servers').disabled);
   await subscriptionPage.waitForFunction(()=>!document.querySelector('[data-action="refresh"]').disabled);
   assert.equal(admitted.length,3);checks++;
   assert.equal(await page.evaluate(()=>window.queueCompletionToasts),1,'Summary refresh must not register duplicate completion callbacks');checks++;
   assert.deepEqual(errors,[]);
   await siblings[0].tab.click('#refresh-status');
   assert(await siblings[1].tab.locator('#dns-auto-check').isChecked());
   await siblings[2].tab.click('#bg-refresh');
   await siblings[2].tab.waitForFunction(()=>document.querySelector('#bg-list').textContent.includes('Завершено'));
   assert.equal(await siblings[2].tab.locator('#bg-list .bg-operation').count(),3);checks++;
   const result={status:'PASS',checks,pages:['Servers','Subscriptions','Home','DNS-over-TLS','BROray'],errors,admissions:admitted.map(r=>({path:r.path,id:r.body.id})),environment:'Five production pages simultaneously + fixture HTTP; no router'};
   fs.writeFileSync(path.join(output,'RESULT.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));return;
  }
  if(clientMode){
   const responses=await page.evaluate(()=>Promise.all([
    BROrayUI.submitQueued('/api/servers/check.cgi','fixture'),
    BROrayUI.submitQueued('/api/servers/check.cgi','fixture')]));
   assert.equal(submissions,1);assert.equal(responses[0].requestId,requestId);
   assert.equal(responses[1].requestId,requestId);assert.equal(responses[0].state,'queued');checks++;
   await page.reload();
   const resumed=await page.evaluate(()=>BROrayUI.submitQueued('/api/servers/check.cgi','fixture'));
   assert.equal(submissions,1);assert.equal(resumed.state,'queued');assert(lookups>=2);checks++;
   state='completed';
   const completed=await page.evaluate(()=>BROrayUI.submitQueued('/api/servers/check.cgi','fixture'));
   assert.equal(completed.state,'completed');assert.equal(submissions,1);checks++;
   assert.deepEqual(errors,[]);
   const result={status:'PASS',checks,submissions,lookups,errors,environment:'Production shared client + lost-response HTTP fixture; no router'};
   fs.writeFileSync(path.join(output,'RESULT.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));
   return;
  }
  async function refresh(){
   const done=page.waitForResponse(r=>r.url().endsWith('/operations/status.cgi'));
   await page.click('#bg-refresh');await done;
   await page.waitForFunction(expected=>document.querySelector(expected==='Отменено'?'#bg-history-list':'#bg-list').textContent.includes(expected),
    state==='cancelled'?'Отменено':state==='running'?'Проверка':reason==='automation_paused'?'На паузе':
    reason==='active_connection'?'Отложено ради активного подключения':'В очереди');
  }
  await refresh();
  assert(!(await page.textContent('#bg-list')).includes('Владелец'));checks++;
  assert((await page.textContent('#bg-badge')).includes('В очереди'));checks++;
  reason='active_connection';await refresh();checks++;
  reason='automation_paused';await refresh();checks++;
  reason='awaiting_resource';state='running';await refresh();
  assert.equal(await page.locator('#bg-list .bg-operation').count(),1);
  assert((await page.textContent('#bg-list')).includes('Подтверждён'));checks++;
  state='queued';await refresh();
  await page.locator('#bg-list button').evaluate(el=>{el.click();el.click();});
  await page.waitForFunction(()=>document.querySelector('#bg-history-list').textContent.includes('Отменено'));
  assert.equal(cancelCalls,1);checks++;
  assert.deepEqual(errors,[]);
  await page.locator('#bg-title').scrollIntoViewIfNeeded();
  await page.screenshot({path:path.join(output,'queue-cancelled-mobile.png')});
  const result={status:'PASS',checks,errors,environment:'Production WebUI + fixture HTTP; no router'};
  fs.writeFileSync(path.join(output,'RESULT.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));
 }catch(error){
  let index=0;
  for(const context of browser.contexts())for(const page of context.pages()){
   const name='failure-'+(++index);
   await page.screenshot({path:path.join(output,name+'.png')}).catch(()=>{});
   fs.writeFileSync(path.join(output,name+'.json'),JSON.stringify({url:page.url(),
    text:await page.locator('body').innerText().catch(()=>''),admissions:admitted.map(r=>({path:r.path,id:r.body.id}))},null,2));
  }
  fs.writeFileSync(path.join(output,'RESULT.json'),JSON.stringify({status:'FAIL',checks,errors,error:String(error)},null,2));
  throw error;
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
