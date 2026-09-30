// Real page controller, deterministic API interleaving; no physical router.
const fs=require('fs'),path=require('path'),assert=require('assert/strict');
const {chromium}=require('playwright');
const root=path.resolve(__dirname,'../runtime/app/web-new'),out=process.env.BRORAY_BROWSER_OUTPUT;
const mode=process.env.BRORAY_INFO_MODE||'delayed';
fs.mkdirSync(out,{recursive:false});
const info={version:'3.2.0',candidateId:'fixture-exact',webUIBuild:'fixture-ui',releaseId:'fixture-release',installationHealthy:true,versionsConsistent:true,universalUpdaterReady:true,components:[],protocols:[],capabilities:[],links:{},updateAvailable:false,reinstallSupported:true};
(async()=>{
 const browser=await chromium.launch({headless:true}),page=await browser.newPage();
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 let releaseInfo,infoStarted,posts=0,first=true,checks=0;
 const started=new Promise(r=>infoStarted=r);
 try{
  await page.route('**/*',async route=>{
   const u=new URL(route.request().url());assert.equal(u.hostname,'broray.test');
   if(u.pathname==='/api/session.cgi')return route.fulfill({json:{ok:true,user:'fixture'}});
   if(u.pathname==='/api/broray/update-status.cgi')return route.fulfill({json:{ok:true,operationId:null,state:'idle',running:false}});
   if(u.pathname==='/api/broray/info.cgi'){
    if(first){first=false;infoStarted();await new Promise(r=>releaseInfo=r);if(mode==='failed')return route.fulfill({status:503,json:{ok:false,error:{message:'Controlled info failure'}}});}
    return route.fulfill({json:{success:true,data:info}});
   }
   if(u.pathname==='/api/broray/update-check.cgi'){posts++;return route.fulfill({json:{ok:true,updateAvailable:false,availableVersion:'3.2.0',candidateId:'fixture-exact',candidateRelation:'same'}});}
   if(u.pathname.startsWith('/api/'))throw Error('Unexpected API '+u.pathname);
   const f=path.resolve(root,'.'+u.pathname);assert(f.startsWith(root+path.sep));
   let body=fs.readFileSync(f);
   if(u.pathname==='/broray.html')body=Buffer.from(body.toString().replace(/<script\b[^>]*src="([^"]+)"[^>]*><\/script>/g,(all,src)=>src.startsWith('/assets/js/broray.js?')?all:''));
   return route.fulfill({body,contentType:({'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'})[path.extname(f)]||'application/octet-stream'});
  });
  await page.goto('http://broray.test/broray.html');await started;
  await page.locator('#app').waitFor({state:'visible'});
  assert(await page.locator('#check-update').isDisabled(),'Update check must wait for installation identity, not render an empty model');checks++;
  await page.locator('#check-update').dispatchEvent('click');
  assert.equal(posts,0,'A premature handler call must not send an update request');checks++;
  assert.equal((await page.locator('#installation-status').innerText()).trim(),'Проверка…');checks++;
  releaseInfo();
  if(mode==='failed'){
   await page.waitForFunction(()=>document.querySelector('#page-error').textContent.includes('Controlled info failure'));
   assert(await page.locator('#check-update').isDisabled());checks++;
   assert.equal(posts,0);checks++;
   await page.reload();
  }
  await page.waitForFunction(()=>document.querySelector('#current-version').textContent==='3.2.0');
  assert(await page.locator('#check-update').isEnabled());checks++;
  await page.locator('#check-update').click();
  await page.waitForFunction(()=>!document.querySelector('#check-update').disabled);
  assert.equal(posts,1);checks++;
  assert.equal((await page.locator('#current-version').innerText()).trim(),'3.2.0');checks++;
  assert.equal((await page.locator('#installation-status').innerText()).trim(),'Установлено');checks++;
  assert.equal((await page.locator('#webui-build').innerText()).trim(),'fixture-ui');checks++;
  assert.deepEqual(errors,[]);checks++;
  fs.writeFileSync(path.join(out,'RESULT.json'),JSON.stringify({status:'PASS',mode,checks,posts,errors}));
  console.log(JSON.stringify({status:'PASS',mode,checks,posts}));
 }catch(e){await page.screenshot({path:path.join(out,'failure.png')});fs.writeFileSync(path.join(out,'RESULT.json'),JSON.stringify({status:'FAIL',mode,checks,posts,error:String(e),errors}));throw e;}
 finally{if(releaseInfo)releaseInfo();await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
