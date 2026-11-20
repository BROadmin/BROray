const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const file=path.join(__dirname,'../../webui/static/assets/js/servers-reserve.js');
const rev='a'.repeat(64),rev2='b'.repeat(64);
const settings=(enabled=false)=>({schemaVersion:1,enabled,primaryServerId:'a',backupServerIds:['b'],revision:rev,configured:true});
const status=(enabled=false)=>({settings:settings(enabled),activeServerId:'a',paused:false,recoveryRequired:false,checking:false,state:null});
const settle=async()=>{for(let i=0;i<10;i++)await new Promise(r=>setImmediate(r));};
async function fixture(saved=null){
 const nodes=new Map(),calls=[],writes=[],handlers={};let data=status(),fail=false,listener=null,pending=saved,hold=null;
 function node(id){if(!nodes.has(id))nodes.set(id,{id,value:'',checked:false,disabled:false,indeterminate:false,textContent:'',children:[],attributes:{},events:{},
 addEventListener(n,fn){this.events[n]=fn;},setAttribute(n,v){this.attributes[n]=v;},replaceChildren(...children){this.children=children;},appendChild(c){this.children.push(c);},
 async fire(n){return this.events[n]?.({preventDefault(){},target:this});}});return nodes.get(id);}
 const document={readyState:'complete',hidden:false,getElementById:node,createElement(){return {value:'',textContent:''};},addEventListener(n,f){handlers[n]=f;}};
 const ui={async apiRequest(url,opts){calls.push({url,opts});if(opts?.method==='POST')return {ok:true};if(hold){const h=hold;hold=null;return h;}if(fail)throw Error('readback failed');return JSON.parse(JSON.stringify(data));},
 readReserveModeRequest(){return pending;},followReserveMode(enabled,revision,fn){writes.push({enabled,revision});listener=fn;if(enabled!==null)pending={enabled,expectedRevision:revision,nonce:'c'.repeat(32)};fn({state:'queued'});return Promise.resolve({state:'queued'});}};
 const window={BROrayUI:ui,location:{origin:'http://router.test'}};
 vm.runInNewContext(fs.existsSync(file)?fs.readFileSync(file,'utf8'):'',{window,document,BROrayUI:ui,AbortController,setTimeout,clearTimeout,setInterval(){return 1;},clearInterval(){}});
 assert.equal(typeof window.BROrayReserve?.updateSummary,'function','reserve panel consumer is absent');
 window.BROrayReserve.updateSummary({servers:[{id:'a',name:'Main'},{id:'b',name:'Backup'},{id:'c',name:'Third'}],activeServer:{id:'a'}});await settle();
 return {node,calls,writes,update(servers){window.BROrayReserve.updateSummary({servers}, row=>row.sourceLabel);},async refresh(){await node('reserve-refresh').fire('click');await settle();},data(v){data=v;},fail(){fail=true;},hold(p){hold=p;},async terminal(state){pending=null;listener({state});await settle();},async unknown(){listener({state:'unknown'});await settle();}};
}
test('reserve panel loads disabled policy and saves only roster CAS without enabling mode',async()=>{
 const f=await fixture();assert.equal(f.node('reserve-enabled').checked,false);assert.equal(f.writes.length,0);
 f.node('reserve-backup-2').value='c';await f.node('reserve-backup-2').fire('change');await f.node('reserve-roster-form').fire('submit');await settle();
 const write=f.calls.find(c=>c.opts?.method==='POST');assert.equal(write.url,'/api/servers/reserve-settings.cgi');assert.deepEqual(JSON.parse(JSON.stringify(write.opts.body)),{expectedRevision:rev,primaryServerId:'a',backupServerIds:['b','c']});assert.equal(write.opts.headers['X-BROray-Request'],'operations');assert.equal(f.writes.length,0);
});
test('reserve panel preserves draft and its original revision across polling and rejects duplicate roster',async()=>{
 const f=await fixture();f.node('reserve-backup-2').value='c';await f.node('reserve-backup-2').fire('change');const next=status();next.settings.revision=rev2;next.settings.primaryServerId='b';next.settings.backupServerIds=['a'];f.data(next);await f.refresh();assert.equal(f.node('reserve-primary').value,'a');
 await f.node('reserve-roster-form').fire('submit');await settle();assert.equal(f.calls.find(c=>c.opts?.method==='POST').opts.body.expectedRevision,rev);
 const g=await fixture();g.node('reserve-backup-1').value='a';await g.node('reserve-backup-1').fire('change');await g.node('reserve-roster-form').fire('submit');assert.equal(g.calls.filter(c=>c.opts?.method==='POST').length,0);
});
test('reserve panel mode admission waits for terminal and readback; enabling never posts roster',async()=>{
 const f=await fixture();f.node('reserve-enabled').checked=true;await f.node('reserve-enabled').fire('change');await settle();assert.deepEqual(f.writes,[{enabled:true,revision:rev}]);assert.equal(f.node('reserve-enabled').disabled,true);assert.equal(f.calls.filter(c=>c.opts?.method==='POST').length,0);
 f.data(status(true));await f.terminal('completed');assert.equal(f.node('reserve-enabled').checked,true);assert.equal(f.node('reserve-enabled').disabled,false);assert.equal(f.node('reserve-primary').disabled,true);
});
test('reserve panel refuses enabling dirty roster but allows independent disabling without saving it',async()=>{
 const f=await fixture();f.node('reserve-backup-2').value='c';await f.node('reserve-backup-2').fire('change');f.node('reserve-enabled').checked=true;await f.node('reserve-enabled').fire('change');assert.equal(f.writes.length,0);
 const next=status(true);f.data(next);await f.refresh();f.node('reserve-enabled').checked=false;await f.node('reserve-enabled').fire('change');await settle();assert.deepEqual(f.writes,[{enabled:false,revision:rev}]);assert.equal(f.calls.filter(c=>c.opts?.method==='POST').length,0);
});
test('reserve panel unknown outcome resumes persisted receipt and terminal readback failure stays blocked',async()=>{
 const f=await fixture();f.node('reserve-enabled').checked=true;await f.node('reserve-enabled').fire('change');await f.unknown();assert.equal(f.node('reserve-enabled').disabled,true);await f.refresh();assert.deepEqual(f.writes.at(-1),{enabled:null,revision:null});
 f.fail();await f.terminal('completed');assert.equal(f.node('reserve-enabled').disabled,true);assert.equal(f.node('reserve-enabled').indeterminate,true);
});
test('reserve panel reload resumes pending mode without a new mode admission',async()=>{
 const f=await fixture({enabled:true,expectedRevision:rev,nonce:'d'.repeat(32)});assert.deepEqual(f.writes,[{enabled:null,revision:null}]);assert.equal(f.node('reserve-enabled').disabled,true);
});
test('reserve panel recovery flag and failed initial read never fabricate writable defaults',async()=>{
 const f=await fixture();const next=status();next.recoveryRequired=true;f.data(next);await f.refresh();assert.equal(f.node('reserve-enabled').disabled,true);assert.equal(f.node('reserve-save').disabled,true);
});
test('reserve panel offers guarded recovery for enabled reserve without a diagnostic inbound',async()=>{
 const f=await fixture();const missing=status(true);missing.runtimeRepairRequired=true;
 f.data(missing);await f.refresh();
 assert.equal(f.node('reserve-mode-label').textContent,'Требуется восстановление');
 assert.equal(f.node('reserve-enabled').disabled,true);
 assert.equal(f.node('reserve-repair').hidden,false);
 await f.node('reserve-repair').fire('click');await settle();
 assert.deepEqual(f.writes,[{enabled:true,revision:rev}]);
 f.data(status(true));await f.terminal('completed');
 assert.equal(f.node('reserve-repair').hidden,true);
 assert.equal(f.node('reserve-mode-label').textContent,'Включено');
});
test('reserve panel ignores older status completion after a newer read and does not discard draft on cancel failure',async()=>{
 const f=await fixture();let release;f.hold(new Promise(r=>release=r));const old=f.node('reserve-refresh').fire('click');await settle();f.data(status(true));await f.refresh();release(status(false));await old;await settle();assert.equal(f.node('reserve-enabled').checked,true);
 const g=await fixture();g.node('reserve-backup-2').value='c';await g.node('reserve-backup-2').fire('change');g.fail();await g.node('reserve-cancel').fire('click');await settle();assert.equal(g.node('reserve-backup-2').value,'c');
});

test('reserve panel groups main choices and saves the exact selected configuration', async () => {
 const f = await fixture();
 const original = status();
 original.settings.primaryServerId = 'main-json';
 f.data(original);
 f.update([
  {id:'main-uri', displayGroupId:'main', name:'Main', sourceLabel:'Provider · URI'},
  {id:'main-json', displayGroupId:'main', name:'Main', sourceLabel:'Provider · JSON'},
  {id:'b', name:'Backup'}, {id:'c', name:'Third'}
 ]);
 await f.refresh();
 const primary = f.node('reserve-primary');
 assert.deepEqual(primary.children.map(option=>option.value), ['', 'main', 'b', 'c']);
 assert.equal(primary.value, 'main');
 const source = f.node('reserve-primary-source');
 assert.equal(source.value, 'main-json');
 assert.deepEqual(source.children.map(option=>option.textContent).sort(), ['Provider · JSON','Provider · URI']);
 assert.equal(f.node('reserve-backup-1').children.find(option=>option.value==='main').disabled, true);
 f.node('reserve-backup-2').value='main';
 await f.node('reserve-backup-2').fire('change');
 await f.node('reserve-roster-form').fire('submit');
 assert.equal(f.calls.some(c=>c.opts?.method==='POST'), false);
 f.node('reserve-backup-2').value='c';
 await f.node('reserve-backup-2').fire('change');
 await f.node('reserve-roster-form').fire('submit');
 assert.equal(f.calls.find(c=>c.opts?.method==='POST').opts.body.primaryServerId, 'main-json');
});
