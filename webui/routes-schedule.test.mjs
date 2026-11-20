import fs from 'node:fs';
import vm from 'node:vm';
import test from 'node:test';
import assert from 'node:assert/strict';
const code=fs.readFileSync(new URL('./static/assets/js/routes-schedule.js',import.meta.url),'utf8');
const clone=x=>JSON.parse(JSON.stringify(x));
const flush=async()=>{for(let i=0;i<14;i++)await new Promise(setImmediate);};
async function fixture(){
 const nodes=new Map(),events={},requests=[],messages=[];
 let settings={intervalHours:24,autoApply:false,maxRemovalPercent:50,revision:'1'.padStart(64,'0'),settingsValid:true,running:false};
 let serial=1,mode='',failRead=false,holdRead=false,releaseRead,releaseWrite,holdCheck=false,releaseCheck;
 const el=id=>{if(!nodes.has(id))nodes.set(id,{value:'',checked:false,disabled:false,textContent:'',className:'',events:{},
  classList:{toggle(){}},addEventListener(k,f){this.events[k]=f;}});return nodes.get(id);};
 const api=async(url,options={})=>{
  const body=options.body?JSON.parse(options.body):undefined;requests.push({url,...options,body});
  if(options.method==='POST'){
   if(url.endsWith('catalog-check.cgi')) {
    const result=clone({...settings,running:true});
    if(holdCheck)await new Promise(r=>{releaseCheck=r;});
    return result;
   }
   if(mode==='pending')await new Promise(r=>{releaseWrite=r;});
   if(mode==='reject')throw Error('refused');
   if(body.expectedRevision&&body.expectedRevision!==settings.revision)throw Error('changed');
   const {expectedRevision,...patch}=body;settings={...settings,...patch,revision:String(++serial).padStart(64,'0')};
   if(mode==='lost')throw Error('reply lost');
   return clone(settings);
  }
  if(failRead)throw Error('offline');
  const result=clone(settings);if(holdRead){holdRead=false;await new Promise(r=>{releaseRead=r;});}return result;
 };
 const document={readyState:'complete',hidden:false,getElementById:el,addEventListener(k,f){events[k]=f;},dispatchEvent(){}};
 const window={location:{origin:'http://router.test'},setTimeout(){return 1;},clearTimeout(){},BROrayUI:{apiRequest:api,toast(m){messages.push(m);},redirectToLogin(){}}};
 vm.runInNewContext(code,{document,window,CustomEvent:class{constructor(type,detail){this.type=type;this.detail=detail;}}});await flush();
 const dispatch=(id,type)=>el(id).events[type]?.({target:el(id),preventDefault(){}});
 return {el,events,requests,messages,stored:()=>clone(settings),dispatch,
  change(id,value){el(id)[id.endsWith('-auto')?'checked':'value']=value;dispatch(id,'change');},
  draft(id,value){el(id).value=value;dispatch(id,'input');},mode(v){mode=v;},failRead(v){failRead=v;},
  holdRead(){holdRead=true;},releaseRead(){releaseRead();},releaseWrite(){releaseWrite();},
  holdCheck(){holdCheck=true;},releaseCheck(){releaseCheck();},
  remote(patch){settings={...settings,...patch,revision:String(++serial).padStart(64,'0')};}};
}

test('catalog switch writes one field and preserves dirty neighbouring setting',async()=>{
 const f=await fixture();f.draft('routes-schedule-removal','70');f.change('routes-schedule-auto',true);await flush();
 assert.deepEqual(Object.keys(f.requests.find(r=>r.method==='POST').body).sort(),['autoApply','expectedRevision']);
 assert.equal(f.stored().autoApply,true);assert.equal(f.stored().maxRemovalPercent,50);assert.equal(f.el('routes-schedule-removal').value,'70');
 f.change('routes-schedule-auto',false);await flush();assert.equal(f.stored().autoApply,false);
});
test('catalog lost response is read back without repeat write',async()=>{
 const f=await fixture();f.mode('lost');f.change('routes-schedule-auto',true);await flush();
 assert.equal(f.el('routes-schedule-auto').checked,true);assert.equal(f.requests.filter(r=>r.method==='POST').length,1);
});
test('catalog unknown result locks settings until confirmed readback',async()=>{
 const f=await fixture();f.mode('lost');f.failRead(true);f.change('routes-schedule-auto',true);await flush();
 assert.equal(f.el('routes-schedule-auto').disabled,true);
 f.change('routes-schedule-auto',false);await flush();assert.equal(f.requests.filter(r=>r.method==='POST').length,1);
 f.failRead(false);f.events.visibilitychange();await flush();
 assert.equal(f.el('routes-schedule-auto').disabled,false);assert.equal(f.el('routes-schedule-auto').checked,true);
});
test('catalog older poll cannot overwrite completed save',async()=>{
 const f=await fixture();f.holdRead();f.events.visibilitychange();await flush();
 f.change('routes-schedule-auto',true);await flush();f.releaseRead();await flush();assert.equal(f.el('routes-schedule-auto').checked,true);
});
test('catalog pending save blocks duplicates and rejection restores stored state',async()=>{
 const f=await fixture();f.mode('pending');f.change('routes-schedule-auto',true);await flush();
 f.change('routes-schedule-auto',false);await flush();assert.equal(f.requests.filter(r=>r.method==='POST').length,1);
 f.releaseWrite();await flush();assert.equal(f.el('routes-schedule-auto').checked,true);
 f.mode('reject');f.change('routes-schedule-auto',false);await flush();assert.equal(f.el('routes-schedule-auto').checked,true);
});
test('catalog poll preserves draft and refuses changed-field overwrite',async()=>{
 const f=await fixture();f.draft('routes-schedule-removal','70');f.remote({maxRemovalPercent:40});f.events.visibilitychange();await flush();
 assert.equal(f.el('routes-schedule-removal').value,'70');
 f.change('routes-schedule-removal','70');await flush();
 assert.equal(f.requests.filter(r=>r.method==='POST').length,0);assert.equal(f.stored().maxRemovalPercent,40);
});

test('catalog manual-check response cannot undo a later preference save',async()=>{
 const f=await fixture();f.holdCheck();f.dispatch('routes-schedule-check','click');await flush();
 f.change('routes-schedule-auto',true);await flush();f.releaseCheck();await flush();
 assert.equal(f.stored().autoApply,true);assert.equal(f.el('routes-schedule-auto').checked,true);
});
