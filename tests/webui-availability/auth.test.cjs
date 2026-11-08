const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const scripts=path.resolve(__dirname,'../../webui/static/assets/js');
const flush=()=>new Promise(resolve=>setImmediate(resolve));
function harness(fetch) {
  const elements=new Map(), timers=new Map(), redirects=[],toasts=[];
  let seq=0;
  function element(id='') {
    if(elements.has(id))return elements.get(id);
    const handlers={};
    const el={id,hidden:true,disabled:false,value:'',type:'password',textContent:'',dataset:{},childNodes:[],children:[],
      classList:{add(){},remove(){},toggle(){},contains(){return false;}},
      addEventListener(name,fn){handlers[name]=fn;},dispatch(name){return handlers[name]?.({preventDefault(){},currentTarget:el});},
      querySelector(){return null;},querySelectorAll(){return [];},getAttribute(){return null;},setAttribute(){},
      appendChild(child){el.children.push(child);},append(child){el.children.push(child);},focus(){},remove(){}};
    if(id)elements.set(id,el);return el;
  }
  const document={getElementById:element,querySelectorAll(){return[];},querySelector(){return null;},createElement(){return element();},addEventListener(){},body:element('body')};
  const window={document,location:{replace(url){redirects.push(url);},origin:'http://localhost'},
    setTimeout(fn,ms){timers.set(++seq,{fn,ms});return seq;},clearTimeout(id){timers.delete(id);},setInterval(){},addEventListener(){}};
  const ctx=vm.createContext({window,document,fetch,AbortController,setTimeout:window.setTimeout,clearTimeout:window.clearTimeout,console,Map,URLSearchParams});
  function load(file){vm.runInContext(fs.readFileSync(path.join(scripts,file),'utf8'),ctx,{filename:file});ctx.BROrayUI=window.BROrayUI;}
  load('common.js');
  return {window,document,element,load,timers,redirects,toasts,ctx};
}
for(const phase of ['headers','body'])test(`login recovers from hanging ${phase} without replay`,async()=>{
  let logins=0,signal;
  const h=harness(async(url,opts)=>{
    if(url.endsWith('session.cgi'))return{ok:false,status:401,json:async()=>({error:'SESSION_REQUIRED'})};
    logins++;signal=opts.signal;
    const stalled=()=>new Promise((resolve,reject)=>signal?.addEventListener('abort',()=>reject(new DOMException('aborted','AbortError')),{once:true}));
    return phase==='headers'?stalled():{ok:true,status:200,json:stalled};
  });
  h.load('login.js');await flush();
  h.element('login').value='synthetic';h.element('password').value='synthetic';
  const action=h.element('login-form').dispatch('submit');await flush();
  assert.equal(h.element('login-submit').disabled,true);
  assert.ok(h.timers.size>0,'login has no bounded deadline');
  for(const t of [...h.timers.values()])t.fn();
  await action;
  assert.equal(signal.aborted,true);
  assert.equal(h.element('login-submit').disabled,false);
  assert.equal(h.element('password').value,'');
  assert.equal(h.element('login-error').hidden,false);
  assert.equal(h.element('login-error').dataset.errorCode,'REQUEST_TIMEOUT');
  assert.equal(logins,1);assert.deepEqual(h.redirects,[]);
  assert.equal(h.timers.size,0);
});
test('slow non-auth operations retain their existing duration contract',async()=>{
  const h=harness(async()=>{
    assert.equal(h.timers.size,0,'non-auth operation received the authentication deadline');
    return {ok:true,status:200,json:async()=>({success:true})};
  });
  await h.window.BROrayUI.apiRequest('/api/routes/export.cgi',{method:'POST',body:{}});
  assert.equal(h.timers.size,0);
});
test('Xray initial session transport failure is not a logout and offers retry',async()=>{
  const h=harness(async()=>{throw new Error('temporary cloud failure');});
  h.window.BROrayUI.toast=(...x)=>h.toasts.push(x);
  h.load('xray.js');await flush();
  assert.deepEqual(h.redirects,[],'network error redirected a valid session to login');
  assert.equal(h.element('page-loader').hidden,false);
  assert.ok(h.element('page-loader').children.some(x=>x.textContent==='Повторить'),'missing recovery action');
});
test('Xray initial expired session still redirects to login',async()=>{
  const h=harness(async()=>({ok:false,status:401,json:async()=>({error:'SESSION_REQUIRED'})}));
  h.load('xray.js');await flush();
  assert.ok(h.redirects.length>0);
});
test('Keenetic initial session transport failure is not a logout and offers retry',async()=>{
  const h=harness(async()=>{throw new Error('temporary cloud failure');});
  h.window.BROrayUI.toast=(...x)=>h.toasts.push(x);
  h.load('keenetic.js');await flush();
  assert.deepEqual(h.redirects,[],'network error redirected a valid session to login');
  assert.equal(h.element('page-loader').hidden,false);
  assert.ok(h.element('page-loader').children.some(x=>x.textContent==='Повторить'),'missing recovery action');
});
test('BROray page authentication has a deadline too',async()=>{
  const h=harness(async(url,opts)=>new Promise((resolve,reject)=>opts.signal?.addEventListener('abort',()=>reject(new DOMException('aborted','AbortError')),{once:true})));
  h.load('broray.js');await flush();
  assert.ok(h.timers.size>0,'BROray page session fetch has no bounded deadline');
  for(const timer of [...h.timers.values()])timer.fn();
  await flush();
  assert.deepEqual(h.redirects,[]);
});
