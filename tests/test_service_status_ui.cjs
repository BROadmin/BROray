/* Real production page; HTTP status responses are fixtures, never router state. */
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict'),crypto=require('node:crypto');
const root=path.resolve(process.env.BRORAY_WEB_ROOT||path.resolve(__dirname,'../runtime/app/web-new'));
const out=path.resolve(process.env.BRORAY_UI_TEST_OUTPUT||path.resolve(__dirname,'../../docs/evidence/service-status-ui-20260915-02'));
const scenarios=[
  [{complete:false,running:null,state:'ambiguous'},'Состояние не подтверждено'],
  [{complete:false,running:true,state:'ambiguous'},'Состояние не подтверждено'],
  [{},'Состояние не подтверждено'],
  [{complete:true,running:true,state:'starting'},'Запускается'],
  [{complete:true,running:true,state:'stopping'},'Останавливается'],
  [{complete:true,running:true,state:'running'},'Работает'],
  [{complete:true,running:false,state:'stopped'},null]
];
(async()=>{
  fs.mkdirSync(out,{recursive:false});
  const browser=await chromium.launch({headless:true}),page=await browser.newPage();
  let service={},checks=0,config={},summary={},reason="",holdSummary=null,releaseSummary,failStatus=false;const errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',async route=>{
    const u=new URL(route.request().url());if(u.hostname!=='broray.test')return route.abort();
    if(u.pathname.startsWith('/api/')){
      if(u.pathname.endsWith('/auto-switch-status.cgi')&&failStatus)return route.fulfill({status:503,json:{success:false,error:{code:'FIXTURE_ERROR',message:'Fixture status unavailable'}}});
      if(/\/(auto-switch|quality-refresh)-status\.cgi$/.test(u.pathname))
        return route.fulfill({json:{success:true,data:{config,state:{lastReason:reason},service}}});
      if(u.pathname==='/api/servers/summary.cgi'){if(holdSummary)await holdSummary;return route.fulfill({json:{success:true,data:summary}});}
      return route.fulfill({json:{success:true,data:u.pathname.includes('session')?{user:'fixture'}:{version:'3.1.0',components:[],protocols:[],capabilities:[],running:false}}});
    }
    const file=path.resolve(root,'.'+u.pathname);
    if(!file.startsWith(root+path.sep)||!fs.existsSync(file))return route.fulfill({status:404,body:''});
    return route.fulfill({body:fs.readFileSync(u.pathname.endsWith('/servers-auto-switch.js')&&process.env.BRORAY_AUTOSWITCH_JS?process.env.BRORAY_AUTOSWITCH_JS:file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]||'application/octet-stream'});
  });
  for(const [value,title] of scenarios){
    service=value;await page.goto('http://broray.test/servers.html');
    await page.waitForFunction(expected=>document.getElementById('auto-switch-service')?.textContent===expected,title||'Остановлен');
    assert.equal(await page.textContent('#quality-refresh-service'),title||'Остановлена');checks+=2;
  }

  // A delayed first summary must not allow edits of uninitialized settings.
  config={enabled:true,failureThreshold:7,cooldownMinutes:9,minimumRating:'good',selectionRule:'preferred',preferredServerId:'alpha'};
  summary={servers:[{id:'alpha',name:'Alpha'},{id:'beta',name:'Beta'}]};reason='initial-complete';
  holdSummary=new Promise(resolve=>{releaseSummary=resolve;});
  await page.goto('http://broray.test/servers.html');
  await page.locator('#server-auto-switch-section > summary').click();
  for(const id of ['enabled','rule','preferred','threshold','cooldown','minimum','save'])assert.equal(await page.locator('#auto-switch-'+id).isDisabled(),true,'Initial pending control must be disabled: '+id);
  assert.equal(await page.locator('#auto-switch-form').getAttribute('aria-busy'),'true');
  releaseSummary();holdSummary=null;
  await page.waitForFunction(()=>document.getElementById('auto-switch-reason').textContent==='initial-complete');
  assert.equal(await page.locator('#auto-switch-save').isEnabled(),true);
  assert.equal(await page.locator('#auto-switch-preferred').inputValue(),'alpha');
  assert.equal(await page.locator('#auto-switch-preferred option').count(),3);
  assert.equal(await page.locator('#auto-switch-threshold').inputValue(),'7');checks++;
  // Subsequent status refreshes must preserve edits, including chosen server.
  await page.locator('#auto-switch-threshold').fill('5');await page.locator('#auto-switch-preferred').selectOption('beta');
  reason='dirty-poll-complete';await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));
  await page.waitForFunction(()=>document.getElementById('auto-switch-reason').textContent==='dirty-poll-complete');
  assert.equal(await page.locator('#auto-switch-threshold').inputValue(),'5');assert.equal(await page.locator('#auto-switch-preferred').inputValue(),'beta');checks++;
  // Failed initial status keeps the form blocked; real subsequent success releases it.
  failStatus=true;await page.goto('http://broray.test/servers.html');await page.locator('#server-auto-switch-section > summary').click();
  await page.locator('#toast-root').getByText('Fixture status unavailable',{exact:true}).waitFor();
  assert.equal(await page.locator('#auto-switch-save').isDisabled(),true);assert.equal(await page.locator('#auto-switch-threshold').isDisabled(),true);
  failStatus=false;reason='retry-complete';await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));
  await page.waitForFunction(()=>document.getElementById('auto-switch-reason').textContent==='retry-complete');
  assert.equal(await page.locator('#auto-switch-save').isEnabled(),true);assert.equal(await page.locator('#auto-switch-preferred').inputValue(),'alpha');checks++;

  assert.deepEqual(errors,[]);
  const files=['servers-auto-switch.js','servers-quality-refresh.js'];
  fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({status:'PASS',checks,environment:'Production page with mocked HTTP; no router',sourceSha256:Object.fromEntries(files.map(f=>['app/web-new/assets/js/'+f,crypto.createHash('sha256').update(fs.readFileSync(f==='servers-auto-switch.js'&&process.env.BRORAY_AUTOSWITCH_JS?process.env.BRORAY_AUTOSWITCH_JS:path.join(root,'assets/js',f))).digest('hex')]))},null,2)+'\n');
  console.log(JSON.stringify({status:'PASS',checks}));await browser.close();
})().catch(e=>{console.error(e);process.exit(1);});
