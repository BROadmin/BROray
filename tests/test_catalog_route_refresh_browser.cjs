// Reproduce a summary sampled before verify completing after its successful reply.
// These are local Chromium fixtures, not physical router acceptance.
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path'),assert=require('assert/strict');
const root=path.resolve(__dirname,'../runtime/app/web-new'),out=process.env.BRORAY_BROWSER_OUTPUT;
fs.mkdirSync(out,{recursive:false});
const mode=process.env.BRORAY_RACE_MODE||'after';
const firstDownload=mode==='download';
const id='tiktok',version={sourceCommit:'fixture',contentSha256:'fixture',sourceFileCount:1};
let verified=false,summaries=0,verifies=0,releaseOld,releaseVerify,oldStarted,verifyStarted;
const oldReady=new Promise(r=>oldStarted=r),verifyReady=new Promise(r=>verifyStarted=r),errors=[];
function state(){return {id,bundleId:id,status:'downloaded',routeCount:104,metadata:{name:'TikTok',sourceFileCount:1},availableVersion:firstDownload&&!verified?null:version,downloadedVersion:firstDownload&&!verified?null:version,installedVersion:null,routerPresence:{available:true,registered:false,actualInstalled:false},verifyResult:verified&&!firstDownload?{success:true,contentSha256:'fixture',local:{valid:true,routeCount:104},keenetic:{available:true,status:'not_installed',canApply:true}}:null,operationProgress:{running:false,resumable:false},globalOperation:{active:false}};}
function summary(){return {bundles:[state()],totals:{bundleCount:10},globalOperation:{active:false},operation:{active:false}};}
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.BRORAY_CHROMIUM}),page=await browser.newPage();
 page.on('pageerror',e=>errors.push(e.message));let checks=0;
 try{
  await page.route('**/*',async route=>{
   const u=new URL(route.request().url());if(u.hostname!=='broray.test')return route.abort();
   const reply=data=>route.fulfill({json:{success:true,data}});
   if(u.pathname.endsWith('/catalog-summary.cgi')){
    const n=++summaries,snapshot=summary();
    if(n===2){snapshot.totals.attentionCount=9;oldStarted();await new Promise(r=>releaseOld=r);if(mode==='failed-old')return route.fulfill({status:503,json:{success:false,error:{code:'FIXTURE_OLD_READ_FAILED',message:'Controlled previous read failure'}}});}
    return reply(snapshot);
   }
   if(u.pathname.endsWith('/check.cgi'))return reply({...state(),availableVersion:version});
   if(u.pathname.endsWith(firstDownload?'/download.cgi':'/verify.cgi')){verifies++;verifyStarted();await new Promise(r=>releaseVerify=r);verified=true;return reply(state());}
   if(u.pathname.endsWith('/operation-status.cgi'))return reply({active:false,globalOperation:{active:false},progress:{running:false}});
   if(u.pathname.startsWith('/api/'))return reply({user:'fixture'});
   const file=path.resolve(root,'.'+u.pathname);if(!file.startsWith(root+path.sep)||!fs.existsSync(file))return route.fulfill({status:404});
   return route.fulfill({body:fs.readFileSync(file),contentType:({'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'})[path.extname(file)]||'application/octet-stream'});
  });
  await page.goto('http://broray.test/routes-import.html');
  const card=page.locator('[data-route-card][data-bundle-id="'+id+'"]');
  await card.locator('[data-action="'+(firstDownload?'download':'verify')+'"]').click();await verifyReady;
  await page.evaluate(()=>document.dispatchEvent(new CustomEvent('broray:routes-operation-finished',{detail:{bundleId:'tiktok'}})));
  await oldReady;
  if(mode==='before'){
   releaseOld();await page.waitForFunction(()=>document.querySelector('#routes-attention-count').textContent==='9');
   assert(await card.locator('[data-action="verify"]').isDisabled(),'A snapshot must not clear a pending mutation busy state');checks++;
  }
  const fresh=page.waitForRequest(r=>r.url().includes('/catalog-summary.cgi')&&summaries>=2,{timeout:5000});
  const response=page.waitForResponse(r=>r.url().includes(firstDownload?'/download.cgi':'/verify.cgi'));releaseVerify();await response;
  await page.waitForFunction(text=>Array.from(document.querySelectorAll('.toast-message')).some(x=>x.textContent.includes(text)),firstDownload?'скачаны':'подготовлен к установке');
  if(mode!=='before')releaseOld();
  await fresh;
  await card.locator('[data-action="'+(firstDownload?'verify':'export')+'"]:visible:not([disabled])').waitFor({timeout:5000});checks++;
  assert(summaries>=3,'Mutation completion must request a snapshot started after that mutation');checks++;
  assert.equal(verifies,1,'Read refresh must never repeat the mutation');checks++;
  assert.deepEqual(errors,[]);checks++;
  fs.writeFileSync(path.join(out,'RESULT.json'),JSON.stringify({status:'PASS',checks,summaries,verifies,errors}));console.log(JSON.stringify({status:'PASS',checks,summaries,verifies}));
 }catch(e){await page.screenshot({path:path.join(out,'failure.png')});fs.writeFileSync(path.join(out,'RESULT.json'),JSON.stringify({status:'FAIL',checks,summaries,verifies,error:String(e),errors}));throw e;}
 finally{if(releaseOld)releaseOld();if(releaseVerify)releaseVerify();await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
