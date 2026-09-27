// Real page code; deterministic delayed summary after successful mutation.
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path'),assert=require('assert/strict');
const root=path.resolve(__dirname,'../runtime/app/web-new'),out=process.env.BRORAY_BROWSER_OUTPUT;
fs.mkdirSync(out,{recursive:false});
const id='user-fixture',version={sourceCommit:'fixture',contentSha256:'fixture'};
let installed=false,delaySummary=false,releaseSummary,summaryWaiting=false,preflights=[],removes=0,checks=0;
const errors=[];
function state(){return {id,bundleId:id,metadata:{name:'Route fixture',sourceFileCount:1,canonicalRouteCount:1},downloadedVersion:version,installedVersion:installed?version:null,routerPresence:{available:true,registered:installed,actualInstalled:installed,complete:true,expectedRouteCount:1,presentRouteCount:installed?1:0},operationProgress:{running:false,resumable:false},globalOperation:{active:false}};}
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.BRORAY_CHROMIUM});const page=await browser.newPage();page.on('pageerror',e=>errors.push(e.message));
 try{
  await page.route('**/*',async route=>{
   const u=new URL(route.request().url());if(u.hostname!=='broray.test')return route.abort();
   const reply=data=>route.fulfill({json:{success:true,data}});
   if(u.pathname.endsWith('/custom-summary.cgi')){if(delaySummary){summaryWaiting=true;await new Promise(r=>releaseSummary=r);}return reply({bundles:[state()],totals:{bundleCount:1},globalOperation:{active:false},operation:{active:false}});}
   if(u.pathname.endsWith('/operation-status.cgi'))return reply({active:false,globalOperation:{active:false},progress:{running:false}});
   if(u.pathname.endsWith('/preflight.cgi')){const action=u.searchParams.get('action');preflights.push(action);return reply({ready:true,token:'fixture',operation:action==='delete'?'delete':'install',requestedAction:action,summary:{total:1},checks:{}});}
   if(u.pathname.endsWith('/export.cgi')){installed=true;delaySummary=true;return reply(state());}
   if(u.pathname.endsWith('/custom-remove.cgi')){removes++;return route.fulfill({status:409,json:{success:false,error:{code:'ROUTES_CUSTOM_REMOVE_REQUIRES_DELETE',message:'Still installed'}}});}
   if(u.pathname.startsWith('/api/'))return reply({user:'fixture'});
   const file=path.resolve(root,'.'+u.pathname);if(!file.startsWith(root+path.sep)||!fs.existsSync(file))return route.fulfill({status:404});
   return route.fulfill({body:fs.readFileSync(file),contentType:({'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'})[path.extname(file)]||'application/octet-stream'});
  });
  await page.goto('http://broray.test/routes-custom.html');const card=()=>page.locator('[data-custom-route-card="'+id+'"]');
  await card().locator('[data-custom-action="export"]').click();await page.locator('#confirm-accept').click();
  while(!summaryWaiting)await new Promise(r=>setTimeout(r,10));
  // Reproduce the normal cross-page operation refresh while summary is pending.
  await page.evaluate(()=>document.dispatchEvent(new CustomEvent('broray:routes-operation-state',{detail:{active:false}})));
  assert(await card().locator('[data-custom-action="remove"]').isDisabled(),'Removal must remain disabled until post-mutation summary converges');checks++;
  delaySummary=false;releaseSummary();await card().locator('[data-custom-action="remove"]:not([disabled])').waitFor();
  await card().locator('[data-custom-action="remove"]').click();await page.locator('#confirm-accept').click();
  await page.waitForFunction(()=>document.querySelector('#confirm-eyebrow').textContent==='Предварительная проверка'&&!document.querySelector('#confirm-root').hidden);
  assert.deepEqual(preflights,['export','delete']);assert.equal(removes,0);checks++;
  await page.locator('#confirm-cancel').click();assert.deepEqual(errors,[]);checks++;
  fs.writeFileSync(path.join(out,'RESULT.json'),JSON.stringify({status:'PASS',checks,errors}));console.log(JSON.stringify({status:'PASS',checks}));
 }catch(e){fs.writeFileSync(path.join(out,'RESULT.json'),JSON.stringify({status:'FAIL',checks,error:String(e),errors}));throw e;}
 finally{if(releaseSummary)releaseSummary();await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
