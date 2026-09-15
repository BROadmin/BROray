/* Real production page; HTTP status responses are fixtures, never router state. */
const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict'),crypto=require('node:crypto');
const root=path.resolve(__dirname,'../runtime/app/web-new');
const out=path.resolve(__dirname,'../../docs/evidence/service-status-ui-20260915-02');
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
  let service={},checks=0;const errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',async route=>{
    const u=new URL(route.request().url());if(u.hostname!=='broray.test')return route.abort();
    if(u.pathname.startsWith('/api/')){
      if(/\/(auto-switch|quality-refresh)-status\.cgi$/.test(u.pathname))
        return route.fulfill({json:{success:true,data:{config:{},state:{},service}}});
      return route.fulfill({json:{success:true,data:u.pathname.includes('session')?{user:'fixture'}:{version:'3.1.0',components:[],protocols:[],capabilities:[],running:false}}});
    }
    const file=path.resolve(root,'.'+u.pathname);
    if(!file.startsWith(root+path.sep)||!fs.existsSync(file))return route.fulfill({status:404,body:''});
    return route.fulfill({body:fs.readFileSync(file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]||'application/octet-stream'});
  });
  for(const [value,title] of scenarios){
    service=value;await page.goto('http://broray.test/servers.html');
    await page.waitForFunction(expected=>document.getElementById('auto-switch-service')?.textContent===expected,title||'Остановлен');
    assert.equal(await page.textContent('#quality-refresh-service'),title||'Остановлена');checks+=2;
  }
  assert.deepEqual(errors,[]);
  const files=['servers-auto-switch.js','servers-quality-refresh.js'];
  fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({status:'PASS',checks,environment:'Production page with mocked HTTP; no router',sourceSha256:Object.fromEntries(files.map(f=>['app/web-new/assets/js/'+f,crypto.createHash('sha256').update(fs.readFileSync(path.join(root,'assets/js',f))).digest('hex')]))},null,2)+'\n');
  console.log(JSON.stringify({status:'PASS',checks}));await browser.close();
})().catch(e=>{console.error(e);process.exit(1);});
