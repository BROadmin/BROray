/* Actual production form and JavaScript; HTTP responses are deterministic fixtures. */
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.resolve(__dirname,'../runtime/app/web-new');
const name=process.argv[2]||'field-subscription-ui-20260916';
assert(/^[a-z0-9-]+$/.test(name));
const out=path.resolve(__dirname,'../../docs/evidence',name);fs.mkdirSync(out,{recursive:false});
(async()=>{
 const browser=await chromium.launch({headless:true});
 const results=[];
 try {
  for(const scenario of ['success','parse-error','http-error','invalid-json','network-error','pending']){
   const page=await browser.newPage();const errors=[];let posts=0,records=[],release;
   page.on('pageerror',e=>errors.push(e.message));
   await page.route('**/*',async route=>{
    const req=route.request(),u=new URL(req.url());if(u.hostname!=='broray.test')return route.abort();
    if(u.pathname.startsWith('/api/')){
     if(u.pathname.endsWith('/subscriptions/create.cgi')){
      posts++;assert.equal(req.method(),'POST');const body=req.postDataJSON();
      assert.equal(body.url,'https://example.invalid/sub/test');
      if(scenario==='network-error')return route.abort('connectionreset');
      if(scenario==='invalid-json')return route.fulfill({status:503,contentType:'text/html',body:'Unavailable'});
      if(scenario==='pending')await new Promise(resolve=>{release=resolve;});
      if(scenario==='parse-error'||scenario==='http-error')return route.fulfill({status:scenario==='parse-error'?422:502,json:{success:false,error:{code:scenario==='parse-error'?'PARSE_ERROR':'HTTP_ERROR',message:scenario==='parse-error'?'Содержимое подписки не удалось распознать.':'Не удалось загрузить подписку.'}}});
      records=[{id:'test',name:body.name,displayUrl:body.url,enabled:true,autoUpdateEnabled:false,serversCount:1,lastUpdateStatus:'success',updateIntervalMinutes:360}];
      return route.fulfill({status:201,json:{success:true,data:records[0]}});
     }
     let data={};
     if(u.pathname.endsWith('/subscriptions/list.cgi'))data=records;
     else if(u.pathname.endsWith('/subscriptions/summary.cgi'))data={total:records.length,enabled:records.length,serversReceived:records.length};
     else if(u.pathname.includes('session'))data={ok:true,user:'fixture'};
     return route.fulfill({json:{success:true,data}});
    }
    const file=path.resolve(root,'.'+u.pathname);
    if(!file.startsWith(root+path.sep)||!fs.existsSync(file))return route.fulfill({status:404,body:''});
    return route.fulfill({body:fs.readFileSync(file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]||'application/octet-stream'});
   });
   await page.goto('http://broray.test/subscriptions.html');
   await page.locator('#add-subscription').click();
   await page.locator('#subscription-name').fill('Проверка URL');
   await page.locator('#subscription-url').fill('https://example.invalid/sub/test');
   await page.locator('#subscription-submit').click();
   if(scenario==='pending'){
    await page.waitForFunction(()=>document.querySelector('#subscription-submit').disabled);
    assert((await page.locator('#subscription-submit').textContent()).includes('Сохранение'));
    while(!release)await new Promise(resolve=>setTimeout(resolve,10));release();
   }
   const toast=page.locator('#toast-root .toast').last();await toast.waitFor();
   const message=toast.locator('.toast-message');assert(await message.isVisible());
   const text=await message.textContent();assert(text.length>0);assert.equal(posts,1);
   if(['success','pending'].includes(scenario)){
    assert.equal(await toast.locator('.toast-kind').textContent(),'Успех: ');
    assert(await toast.locator('.toast-note').isHidden());
    assert.equal(text,'Подписка добавлена.');await page.locator('.subscription-card').waitFor();
    assert(!(await page.locator('#subscription-form-panel').isVisible()));
   }else{
    assert.equal(await toast.locator('.toast-kind').textContent(),'Ошибка: ');
    assert((await toast.getAttribute('class')).includes('toast-error'));
    assert(await page.locator('#subscription-form-panel').isVisible());
    assert.equal(await page.locator('#subscription-url').inputValue(),'https://example.invalid/sub/test');
   }
   await page.waitForFunction(()=>!document.querySelector('#subscription-submit').disabled);
   assert.deepEqual(errors,[]);results.push({scenario,status:'PASS',feedback:text});
   await page.screenshot({path:path.join(out,scenario+'.png'),fullPage:true});await page.close();
  }
  fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({status:'PASS',results,routerAccessed:false,environment:'Actual UI with HTTP fixtures; customer URL not reproduced'},null,2)+'\n');
  console.log(JSON.stringify({status:'PASS',checks:results.length}));
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
