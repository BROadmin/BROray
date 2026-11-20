const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const code=fs.readFileSync(path.join(__dirname,'../../webui/static/assets/js/servers-dpi.js'),'utf8');
const revision='a'.repeat(64);
const status=(enabled=false)=>({id:'A',enabled,revision,profileCompatible:true,reason:'',paused:false,recoveryRequired:false});
const settle=async()=>{for(let i=0;i<10;i++)await new Promise(r=>setImmediate(r));};
async function fixture(initial=null){
 let pending=initial,data=status(),listener,hold=null,fail=false;
 const calls=[],writes=[];
 function node(){return {children:[],events:{},attributes:{},checked:false,disabled:false,indeterminate:false,textContent:'',appendChild(c){this.children.push(c);},setAttribute(n,v){this.attributes[n]=v;},addEventListener(n,f){this.events[n]=f;},fire(n){return this.events[n]?.();}};}
 const ui={readDPIProfileRequest(){return pending;},async apiRequest(url,opts){calls.push({url,opts});if(hold){const p=hold;hold=null;return p;}if(fail)throw Error('readback failed');return {...data};},followDPIProfile(id,enabled,rev,fn){writes.push({id,enabled,rev});listener=fn;if(id!==null)pending={id,enabled,expectedRevision:rev,nonce:'b'.repeat(32)};fn({state:'queued'});return Promise.resolve({state:'queued'});}};
 const window={BROrayUI:ui},document={createElement:node};vm.runInNewContext(code,{window,document,AbortController,setTimeout,clearTimeout});
 let root=window.BROrayDPI.create({id:'A',active:true});await settle();
 const nodes=()=>({toggle:root.children[0].children[0],note:root.children[1],refresh:root.children[2]});
 return {calls,writes,nodes,async toggle(enabled=true){nodes().toggle.checked=enabled;await nodes().toggle.fire('change');await settle();},async refresh(){await nodes().refresh.fire('click');await settle();},async terminal(state,message){pending=null;await listener({state,message});await settle();},async unknown(){await listener({state:'unknown'});await settle();},data(v){data=v;},fail(){fail=true;},hold(v){hold=v;},rerender(){root=window.BROrayDPI.create({id:'A'});return settle();}};
}
test('DPI toggle writes only its typed field and waits for confirmed terminal readback',async()=>{
 const f=await fixture();assert.equal(f.nodes().toggle.checked,false);await f.toggle();assert.deepEqual(f.writes,[{id:'A',enabled:true,rev:revision}]);assert.equal(f.nodes().toggle.disabled,true);assert.equal(f.calls.some(x=>x.opts?.method==='POST'),false);
 await f.toggle();assert.equal(f.writes.length,1);f.data(status(true));await f.terminal('completed');assert.equal(f.nodes().toggle.checked,true);assert.equal(f.nodes().toggle.disabled,false);
});
test('DPI unknown result performs receipt lookup without another write and readback failure stays blocked',async()=>{
 const f=await fixture();await f.toggle();await f.unknown();assert.equal(f.nodes().toggle.indeterminate,true);await f.refresh();assert.deepEqual(f.writes.at(-1),{id:null,enabled:null,rev:null});f.fail();await f.terminal('completed');assert.equal(f.nodes().toggle.disabled,true);assert.equal(f.nodes().toggle.indeterminate,true);
});
test('DPI rerender preserves pending operation and reload resumes saved receipt',async()=>{
 const f=await fixture();await f.toggle();await f.rerender();assert.equal(f.writes.length,1);assert.equal(f.nodes().toggle.disabled,true);
 const g=await fixture({id:'A',enabled:true,expectedRevision:revision});assert.deepEqual(g.writes,[{id:null,enabled:null,rev:null}]);
});
test('DPI old read cannot overwrite newer state; incompatible and recovery status block writes',async()=>{
 const f=await fixture();let release;f.hold(new Promise(r=>release=r));const old=f.refresh();await settle();f.data(status(true));await f.refresh();release(status(false));await old;assert.equal(f.nodes().toggle.checked,true);
 f.data({...status(),recoveryRequired:true});await f.refresh();await f.toggle();assert.equal(f.writes.length,0);
 f.data({...status(),profileCompatible:false,reason:'custom mask'});await f.refresh();assert.equal(f.nodes().toggle.disabled,true);assert.match(f.nodes().note.textContent,/custom mask/);
});

test('DPI receipt remains observable when its server disappeared from the rendered catalog',async()=>{
 const f=await fixture({id:'removed',enabled:true,expectedRevision:revision});await f.refresh();assert.deepEqual(f.writes,[{id:null,enabled:null,rev:null}]);
});

test('pending DPI choice stays visible across rerender until authoritative terminal readback',async()=>{
 const f=await fixture();await f.toggle();assert.equal(f.nodes().toggle.checked,true);await f.rerender();assert.equal(f.nodes().toggle.checked,true);
 f.data(status(true));await f.terminal('completed');await f.toggle(false);assert.equal(f.nodes().toggle.checked,false);await f.rerender();assert.equal(f.nodes().toggle.checked,false);
 await f.terminal('failed');assert.equal(f.nodes().toggle.checked,true);assert.equal(f.nodes().toggle.disabled,false);
});
test('removed receipt terminal updates the visible observer status',async()=>{
 const f=await fixture({id:'removed',enabled:true,expectedRevision:revision});await f.refresh();await f.terminal('completed');
 assert.equal(f.nodes().toggle.disabled,false);assert.match(f.nodes().note.textContent,/Обход DPI выключен/);assert.equal(f.writes.length,1);
});
test('DPI failure displays safe queue reason together with confirmed saved state',async()=>{
 const f=await fixture();await f.toggle();await f.terminal('failed','Восстановлены предыдущие параметры подключения.');
 assert.match(f.nodes().note.textContent,/Восстановлены предыдущие параметры подключения/);assert.match(f.nodes().note.textContent,/Обход DPI выключен/);
 assert.equal(f.nodes().toggle.checked,false);assert.equal(f.nodes().toggle.disabled,false);
});
