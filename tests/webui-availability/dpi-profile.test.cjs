const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const root=path.join(__dirname,'../../webui/static');
const code=fs.readFileSync(path.join(root,'assets/js/common.js'),'utf8');
const revision='a'.repeat(64),key='broray.dpi-profile.v1';
function fixture(initial){
 const store=new Map(initial||[]),calls=[],timers=new Map();let timerID=0,random=0,mode='',state='queued',row=null,blocked=false;
 const storage={getItem(k){if(blocked)throw Error('storage blocked');return store.get(k)??null;},setItem(k,v){if(blocked)throw Error('storage blocked');store.set(k,String(v));},removeItem(k){if(blocked)throw Error('storage blocked');store.delete(k);}};
 const window={location:{origin:'http://router.test',pathname:'/servers.html'}};
 const document={hidden:false};
 const fetch=async(url,options={})=>{
  const body=options.body?JSON.parse(options.body):null;calls.push({url,body,headers:options.headers});
  let payload,status=200;
  if(url==='/api/servers/dpi-profile.cgi'){
   row={ok:true,accepted:true,requestId:'q-'+body.nonce,state};
   if(mode==='deny'){status=409;payload={errorCode:'SETTINGS_CHANGED',error:{message:'changed'}};}
   else if(mode==='lost')throw Error('lost response');
   else payload={...row,requestId:mode.startsWith('wrong')?'q-'+'f'.repeat(32):row.requestId};
  } else if(options.method==='POST'){
   if(mode==='unconfirmed'){status=404;payload={errorCode:'REQUEST_UNCONFIRMED',error:{message:'unknown'}};}
   else payload={ok:true,requestId:'q-'+body.nonce,state, ...(mode==='wrong-all'?{requestId:'q-'+'f'.repeat(32)}:{})};
  } else payload={complete:true,queue:mode==='absent'?[]:[row]};
  return {ok:status===200,status,json:async()=>payload};
 };
 vm.runInNewContext(code,{window,document,sessionStorage:storage,crypto:{getRandomValues(a){a.fill(++random);return a;}},fetch,AbortController,
  setTimeout(fn,ms){const id=++timerID;timers.set(id,{fn,ms});return id;},clearTimeout(id){timers.delete(id);}});
 assert.equal(typeof window.BROrayUI.followDPIProfile,'function','typed DPI profile queue helper is absent');
 assert.equal(typeof window.BROrayUI.readDPIProfileRequest,'function','persisted reserve intent reader is absent');
 return {api:window.BROrayUI,store,calls,document,mode(v){mode=v;},state(v){state=v;if(row)row.state=v;},block(){blocked=true;},posts(){return calls.filter(c=>c.url==='/api/servers/dpi-profile.cgi');},async poll(){const entry=[...timers].find(([,v])=>v.ms===5000);assert.ok(entry,'queue watcher not scheduled');timers.delete(entry[0]);await entry[1].fn();}};
}

test('DPI profile sends only typed intent, persists before admission and never equates admission with completion',async()=>{
 const f=fixture(),events=[];const result=await f.api.followDPIProfile("server-A",true,revision,r=>events.push(r));
 assert.equal(result.state,'queued');assert.deepEqual(events.map(r=>r.state),['queued']);
 const saved=JSON.parse(f.store.get(key));assert.equal(saved.enabled,true);assert.equal(saved.expectedRevision,revision);assert.match(saved.nonce,/^[0-9a-f]{32}$/);
 assert.deepEqual(f.posts()[0].body,{id:"server-A",enabled:true,expectedRevision:revision,nonce:saved.nonce});
 assert.equal(f.posts()[0].headers['X-BROray-Queue'],'1');assert.equal(f.posts()[0].headers['X-BROray-Request'],'operations');
 assert.equal(f.api.readDPIProfileRequest().nonce,saved.nonce);
});
test('DPI profile lost response looks up exact nonce without another write',async()=>{
 const f=fixture();f.mode('lost');f.state('running');const r=await f.api.followDPIProfile("server-A",true,revision,()=>{});
 assert.equal(r.state,'running');assert.equal(f.posts().length,1);assert.equal(f.calls.at(-1).url,'/api/operations/status.cgi');
 assert.equal(f.calls.at(-1).body.nonce,f.posts()[0].body.nonce);assert.ok(f.store.has(key));
});
test('DPI profile reload resumes receipt without POST and clears only confirmed terminal result',async()=>{
 const f=fixture();await f.api.followDPIProfile("server-A",true,revision,()=>{});const g=fixture(f.store);g.state('completed');
 const r=await g.api.followDPIProfile(null,null,null,()=>{});assert.equal(r.state,'completed');assert.equal(g.posts().length,0);assert.equal(g.store.has(key),false);
});
test('DPI profile unknown poll retains intent and retry resumes same nonce',async()=>{
 const f=fixture(),events=[];await f.api.followDPIProfile("server-A",false,revision,r=>events.push(r.state));const nonce=JSON.parse(f.store.get(key)).nonce;
 f.mode('absent');await f.poll();assert.deepEqual(events,['queued','unknown']);assert.equal(JSON.parse(f.store.get(key)).nonce,nonce);
 f.mode('');f.state('completed');await f.api.followDPIProfile(null,null,null,()=>{});assert.equal(f.posts().length,1);assert.equal(f.store.has(key),false);
});
test('DPI profile duplicate intent coalesces while conflicting intent cannot replace pending request',async()=>{
 const f=fixture();const a=f.api.followDPIProfile("server-A",true,revision,()=>{}),b=f.api.followDPIProfile("server-A",true,revision,()=>{});
 assert.equal(a,b);await a;await assert.rejects(()=>f.api.followDPIProfile("server-A",false,revision,()=>{}));assert.equal(f.posts().length,1);assert.equal(JSON.parse(f.store.get(key)).enabled,true);
});
test('DPI profile cannot admit work when persistent browser storage fails',async()=>{
 const f=fixture();f.block();await assert.rejects(()=>f.api.followDPIProfile("server-A",true,revision,()=>{}));assert.equal(f.calls.length,0);
});
test('DPI profile wrong receipt is reconciled only against the requested nonce',async()=>{
 const f=fixture();f.mode('wrong-post');const r=await f.api.followDPIProfile("server-A",true,revision,()=>{});assert.equal(r.requestId,'q-'+f.posts()[0].body.nonce);assert.equal(f.calls.length,2);
 const g=fixture();g.mode('wrong-all');await assert.rejects(()=>g.api.followDPIProfile("server-A",true,revision,()=>{}));assert.ok(g.store.has(key));assert.equal(g.posts().length,1);
});
test('DPI profile deterministic refusal clears admission receipt and does not retry automatically',async()=>{
 const f=fixture();f.mode('deny');await assert.rejects(()=>f.api.followDPIProfile("server-A",true,revision,()=>{}));assert.equal(f.posts().length,1);assert.equal(f.store.has(key),false);
});
test('DPI profile malformed saved intent cannot be replaced or submitted',async()=>{
 const f=fixture([[key,'{"nonce":"broken"}']]);assert.throws(()=>f.api.readDPIProfileRequest());await assert.rejects(()=>f.api.followDPIProfile(null,null,null,()=>{}));assert.equal(f.calls.length,0);assert.equal(f.store.get(key),'{"nonce":"broken"}');
});
test('DPI profile missing persisted request requires fresh state instead of automatic re-admission',async()=>{
 const f=fixture();await f.api.followDPIProfile("server-A",true,revision,()=>{});const g=fixture(f.store);g.mode('unconfirmed');
 await assert.rejects(()=>g.api.followDPIProfile(null,null,null,()=>{}));assert.equal(g.posts().length,0);assert.equal(g.store.has(key),false);
});
test('DPI profile invalid intent never changes generic queue validation or sends a request',async()=>{
 const f=fixture();for(const [enabled,rev] of [['true',revision],[true,'bad'],[null,revision]])await assert.rejects(()=>f.api.followDPIProfile(enabled,rev,()=>{}));
 await assert.rejects(()=>f.api.submitQueued('/api/servers/dpi-profile.cgi','reserve'));assert.equal(f.calls.length,0);
});
test('reserve UI is included on Servers before its summary producer and keeps bulk deletion control',()=>{
 const html=fs.readFileSync(path.join(root,'servers.html'),'utf8');assert.ok(html.includes('id="server-reserve-section"'),'reserve section is absent');
 assert.ok(html.indexOf('/assets/js/servers-reserve.js')<html.indexOf('/assets/js/servers.js') && html.includes('/assets/js/servers-reserve.js'),'reserve summary consumer must load first');
 assert.ok(html.includes('delete-other-servers') || html.includes('delete-inactive-servers'),'bulk deletion control must remain');
});

test('DPI request for another server cannot replace existing pending intent',async()=>{
 const f=fixture();await f.api.followDPIProfile('server-A',true,revision,()=>{});
 await assert.rejects(()=>f.api.followDPIProfile('server-B',true,revision,()=>{}));assert.equal(f.posts().length,1);assert.equal(f.api.readDPIProfileRequest().id,'server-A');
});
