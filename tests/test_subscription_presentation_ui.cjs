/* Full local production page; only the HTTP responses are fixtures. */
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.resolve(__dirname,'../runtime/app/web-new');
const evidenceName=process.argv[2]||'subscription-presentation-ui-20260916';
assert(/^[a-z0-9-]+$/.test(evidenceName));
const out=path.resolve(__dirname,'../../docs/evidence',evidenceName);
fs.mkdirSync(out,{recursive:false});
let duration=null,checks=0;
const item=()=>({id:'test',name:'Проверка отмены загрузки',displayUrl:'https://example.test/подписка',enabled:true,autoUpdateEnabled:false,serversCount:2,updateIntervalMinutes:360,lastUpdateStatus:'error',lastUpdatedAt:'2026-09-15T22:29:33Z',lastError:'Обновление остановлено пользователем. Сохранены последние доступные данные подписки.',lastUpdateResult:{errorCode:'CANCELLED',durationMs:duration,warnings:[]}});
(async()=>{
 const browser=await chromium.launch({headless:true});
 try {
  const page=await browser.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',async route=>{
   const u=new URL(route.request().url());if(u.hostname!=='broray.test')return route.abort();
   if(u.pathname.startsWith('/api/')){
    let data={};
    if(u.pathname.endsWith('/subscriptions/list.cgi'))data=[item()];
    else if(u.pathname.endsWith('/subscriptions/summary.cgi'))data={total:1,enabled:1,serversReceived:2,lastUpdateStatus:'error',lastUpdatedAt:item().lastUpdatedAt};
    else if(u.pathname.includes('session'))data={ok:true,user:'fixture'};
    return route.fulfill({json:{success:true,data}});
   }
   const file=path.resolve(root,'.'+u.pathname);
   if(!file.startsWith(root+path.sep)||!fs.existsSync(file))return route.fulfill({status:404,body:''});
   return route.fulfill({body:fs.readFileSync(file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]||'application/octet-stream'});
  });
  for(const width of [1440,1024,390,360])for(const theme of ['broray','night','day']){
   await page.setViewportSize({width,height:1100});
   for(duration of [null,0,8000]){
    await page.goto('http://broray.test/subscriptions.html');
    await page.locator('.subscription-card').waitFor();
    await page.evaluate(t=>document.documentElement.setAttribute('data-theme',t),theme);
    const text=await page.locator('.subscription-result').textContent();
    assert.equal(text,duration===null?'Код: CANCELLED':`Код: CANCELLED · длительность: ${duration} мс`);
    assert(!(await page.locator('.subscription-card').textContent()).includes('HTTP_ERROR'));
    const overflow=await page.locator('.subscription-card').evaluateAll(cards=>cards.flatMap(c=>[c,...c.querySelectorAll('button,.status-badge,.subscription-result,.subscription-error')]).filter(e=>e.getBoundingClientRect().width>0).filter(e=>{const r=e.getBoundingClientRect();return r.left<0||r.right>innerWidth+1||e.scrollWidth>e.clientWidth+1;}).map(e=>e.className));
    assert.deepEqual(overflow,[],`${width}/${theme}/${duration}`);checks++;
    if(duration===null)await page.screenshot({path:path.join(out,`${width}-${theme}.png`),fullPage:true,animations:'disabled'});
   }
  }
  assert.deepEqual(errors,[]);
  fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({status:'PASS',checks,errors,routerAccessed:false,environment:'Full production page, mocked HTTP responses'},null,2)+'\n');
  console.log(JSON.stringify({status:'PASS',checks}));
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
