const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const file=path.join(__dirname,'../../webui/static/assets/js/servers-reserve.js');
const rev='a'.repeat(64),rev2='b'.repeat(64);
const settings=(enabled=false)=>({schemaVersion:1,enabled,primaryServerId:'a',backupServerIds:['b'],revision:rev,configured:true});
const status=(enabled=false,autoSwitchEnabled=false)=>({settings:settings(enabled),autoSwitchEnabled,activeServerId:'a',paused:false,recoveryRequired:false,checking:false,state:null});
const settle=async()=>{for(let i=0;i<10;i++)await new Promise(r=>setImmediate(r));};
async function fixture(saved=null){
 const nodes=new Map(),calls=[],writes=[],handlers={},selectionCalls=[],confirmations=[];let data=status(),fail=false,listener=null,pending=saved,hold=null,confirm=true,onConfirm=null;
 function makeNode(id,tagName){return {id,tagName,value:'',checked:false,disabled:false,indeterminate:false,content:'',children:[],attributes:{},events:{},
 get textContent(){return this.children.length?this.children.map(child=>child.textContent).join(''):this.content;},
 set textContent(value){this.content=value;this.children=[];},
 addEventListener(n,fn){this.events[n]=fn;},setAttribute(n,v){this.attributes[n]=v;},replaceChildren(...children){this.children=children;},appendChild(c){this.children.push(c);},
 async fire(n){return this.events[n]?.({preventDefault(){},target:this});}};}
 function node(id){if(!nodes.has(id))nodes.set(id,makeNode(id));return nodes.get(id);}
 const document={readyState:'complete',hidden:false,getElementById:node,createElement(tagName){return makeNode('',tagName);},addEventListener(n,f){handlers[n]=f;}};
 const ui={toast(){},queueLabel(row){return row.state;},async followQueued(url,id,fn,selection){selectionCalls.push({url,id,selection});fn({state:'queued'});},async apiRequest(url,opts){calls.push({url,opts});if(opts?.method==='POST')return {ok:true};if(hold){const h=hold;hold=null;return h;}if(fail)throw Error('readback failed');return JSON.parse(JSON.stringify(data));},
 readReserveModeRequest(){return pending;},followReserveMode(enabled,revision,fn){writes.push({enabled,revision});listener=fn;if(enabled!==null)pending={enabled,expectedRevision:revision,nonce:'c'.repeat(32)};fn({state:'queued'});return Promise.resolve({state:'queued'});}};
 const window={BROrayUI:ui,BROrayDialogs:{async confirm(options){confirmations.push(options);if(onConfirm)onConfirm();return confirm;}},location:{origin:'http://router.test'}};
 vm.runInNewContext(fs.existsSync(file)?fs.readFileSync(file,'utf8'):'',{window,document,BROrayUI:ui,AbortController,setTimeout,clearTimeout,setInterval(){return 1;},clearInterval(){}});
 assert.equal(typeof window.BROrayReserve?.updateSummary,'function','reserve panel consumer is absent');
 window.BROrayReserve.updateSummary({servers:[{id:'a',name:'Main'},{id:'b',name:'Backup'},{id:'c',name:'Third'}],activeServer:{id:'a'}});await settle();
 return {node,calls,writes,selectionCalls,confirmations,open(id){window.BROrayReserve.openSelection(id);},cancelConfirmation(){confirm=false;},whileConfirm(fn){onConfirm=fn;},update(servers){window.BROrayReserve.updateSummary({servers}, row=>row.sourceLabel);},async refresh(){await node('reserve-refresh').fire('click');await settle();},data(v){data=v;},fail(){fail=true;},hold(p){hold=p;},async terminal(state){pending=null;listener({state});await settle();},async unknown(){listener({state:'unknown'});await settle();}};
}
test('unified card shows four confirmed modes, roles and separately retained toggles',async()=>{
 const f=await fixture();
 for(const [reserve,auto,mode] of [[true,true,'Резервирование + автовыбор'],[false,false,'Ручной режим'],[false,true,'Автовыбор'],[true,false,'Резервирование']]){
  f.data(status(reserve,auto));await f.refresh();
  assert.equal(f.node('server-automation-mode').textContent,mode);
  assert.equal(f.node('reserve-enabled').checked,reserve);
  assert.match(f.node('reserve-active').textContent,reserve?/Main.*основной/i:/Main.*обычное/i);
  assert.equal(f.node('reserve-status').hidden,!reserve);
  assert.doesNotMatch(f.node('reserve-status').textContent,/Резервирование включено/i);
 }
 const pending=status(true,true);pending.activeServerId='b';f.data(pending);await f.refresh();
 assert.match(f.node('reserve-active').textContent,/Backup.*первый резерв/i);
 pending.activeServerId='c';pending.settings.emergency={serverId:'c'};f.data(pending);await f.refresh();
 assert.match(f.node('reserve-active').textContent,/Third.*временный/i);
 pending.paused=true;f.data(pending);await f.refresh();
 assert.equal(f.node('server-automation-mode').textContent,'Резервирование + автовыбор · на паузе');
 assert.match(f.node('server-automation-mode').className,/\bstatus-warning\b/);
 pending.recoveryRequired=true;f.data(pending);await f.refresh();
 assert.equal(f.node('server-automation-mode').textContent,'Требуется восстановление');
 assert.match(f.node('server-automation-mode').className,/\bstatus-warning\b/);
 f.data(status(false,null));await f.refresh();assert.match(f.node('server-automation-mode').textContent,/не подтверждён/i);
 assert.equal(f.writes.length,0);
});
test('reserve exit previews automatic or manual mode and refuses a changed state while confirming',async()=>{
 for(const auto of [false,true]){
  const f=await fixture();f.data(status(true,auto));await f.refresh();
  f.node('reserve-enabled').checked=false;await f.node('reserve-enabled').fire('change');
  assert.match(f.confirmations.at(-1)?.message||'',auto?/автовыбор/i:/ручной режим/i);
  assert.deepEqual(f.writes,[{enabled:false,revision:rev}]);
 }
 const f=await fixture();f.data(status(true,true));await f.refresh();f.open('c');
 f.whileConfirm(()=>f.data(status(true,false)));
 await f.node('reserve-selection-ordinary').fire('click');
 assert.match(f.confirmations.at(-1)?.message||'',/автовыбор/i);
 assert.equal(f.selectionCalls.length,0,'stale next-mode confirmation cannot be sent');
});
test('the automation form and reserve toggle are mounted in one Servers card',()=>{
 const html=fs.readFileSync(path.join(__dirname,'../../webui/static/servers.html'),'utf8');
 const start=html.indexOf('<details id="server-reserve-section"');
 const card=html.slice(start,html.indexOf('</details>',start));
 assert.match(card,/Автовыбор и резервирование/);
 assert.match(card,/id="auto-switch-enabled"/);
 assert.match(card,/Автовыбор без резервирования/);
 assert.match(card,/id="reserve-enabled"/);
 assert.match(card,/id="server-automation-mode"/);
 assert.doesNotMatch(card,/id="reserve-mode-label"|Резервирование: /);
 assert.match(card,/<p>Настройки автовыбора<\/p>/);
});
test('connect now delegates the exact member to normal activation without a roster write',async()=>{
 const f=await fixture();f.data(status(true,true));await f.refresh();f.open('b');
 assert.equal(f.node('reserve-selection-now').hidden,false);
 assert.equal(f.node('reserve-selection-now').disabled,false);
 await f.node('reserve-selection-now').fire('click');
 assert.deepEqual(JSON.parse(JSON.stringify(f.selectionCalls)),[{url:'/api/servers/activate.cgi',id:'b'}]);
 assert.match(f.confirmations.at(-1).message,/Состав и порядок серверов сохранятся/);
 assert.equal(f.calls.some(row=>row.opts?.method==='POST'),false);
 const outsider=await fixture();outsider.data(status(true,true));await outsider.refresh();outsider.open('c');
 assert.equal(outsider.node('reserve-selection-now').hidden,true);
 await outsider.node('reserve-selection-now').fire('click');
 assert.equal(outsider.selectionCalls.length,0);
});
test('ordinary toggle binds the static card and refreshes the mode only on confirmed changes',()=>{
 const nodes=new Map(),listeners=[];
 function node(id){if(!nodes.has(id))nodes.set(id,{id,attributes:{},setAttribute(k,v){this.attributes[k]=v;},
    getAttribute(k){return this.attributes[k];},addEventListener(event){listeners.push(id+':'+event);}});
    return nodes.get(id);}
 const document={readyState:'loading',getElementById:node,addEventListener(){}};
 let refreshes=0;
 const window={BROrayReserve:{refreshAutomationMode(){refreshes++;}}};
 const source=fs.readFileSync(path.join(__dirname,'../../webui/static/assets/js/servers-auto-switch.js'),'utf8');
 vm.runInNewContext(source.replace(/\}\)\(\);\s*$/,'window.fixture={createSection,confirmEnabled};})();'),{window,document});
 window.fixture.createSection();window.fixture.createSection();
 assert.equal(listeners.filter(item=>item==='auto-switch-enabled:change').length,1);
 window.fixture.confirmEnabled({value:'true',revision:rev});
 window.fixture.confirmEnabled({value:true,revision:rev2});
 assert.equal(node('auto-switch-enabled').checked,true);
 assert.equal(refreshes,1);
 window.fixture.confirmEnabled({value:false,revision:rev});
 assert.equal(node('auto-switch-enabled').checked,false);
 assert.equal(refreshes,2);
 assert.throws(()=>window.fixture.confirmEnabled({value:'invalid',revision:rev}));
 assert.equal(refreshes,2);
});
test('confirmed outside promotion requires a roster choice and explicit confirmation',async()=>{
 const f=await fixture(); const next=status(true);next.settings.backupServerIds=['b','c'];
 f.update([{id:'a',name:'Main'},{id:'b',name:'First'},{id:'c',name:'Second'},{id:'d',name:'Outside'}]);
 f.data(next);await f.refresh();f.open('d');
 assert.equal(f.node('reserve-selection-panel').hidden,false);
 assert.equal(f.node('reserve-selection-remove-control').hidden,false);
 assert.equal(f.node('reserve-selection-promote').disabled,true);
 assert.match(f.node('reserve-selection-preview').textContent,/какой сервер исключить/);
 await f.node('reserve-selection-promote').fire('click');assert.equal(f.selectionCalls.length,0);
 const remove=f.node('reserve-selection-remove');remove.value='b';await remove.fire('change');
 assert.equal(f.node('reserve-selection-promote').disabled,false);
 const [heading,roster]=f.node('reserve-selection-preview').children;
 assert.equal(heading?.textContent,'Состав после смены основного');
 assert.equal(roster?.tagName,'dl');
 assert.deepEqual(roster.children.map(row=>row.children.map(child=>[child.tagName,child.textContent])),[
  [['dt','Основной сервер'],['dd','Outside']],
  [['dt','Первый резерв'],['dd','Main']],
  [['dt','Второй резерв'],['dd','Second']],
 ]);
 await f.node('reserve-selection-promote').fire('click');
 assert.equal(f.node('reserve-selection-preview').textContent,'queued');
 assert.deepEqual(JSON.parse(JSON.stringify(f.selectionCalls)),[{url:'/api/servers/reserve-selection.cgi',id:'d',selection:{mode:'promote',expectedRevision:rev,removeServerId:'b'}}]);
});
test('cancelled exit sends no intent and an ordinary exit needs no roster removal',async()=>{
 const f=await fixture();f.data(status(true));await f.refresh();f.open('c');
 f.cancelConfirmation();await f.node('reserve-selection-ordinary').fire('click');assert.equal(f.selectionCalls.length,0);
 const g=await fixture();g.data(status(true));await g.refresh();g.open('c');
 await g.node('reserve-selection-ordinary').fire('click');
 assert.deepEqual(JSON.parse(JSON.stringify(g.selectionCalls)),[{url:'/api/servers/reserve-selection.cgi',id:'c',selection:{mode:'ordinary',expectedRevision:rev,removeServerId:''}}]);
});
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
 assert.equal(f.node('server-automation-mode').textContent,'Ожидание операции');
 assert.equal(f.node('reserve-status').hidden,false);
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
 assert.equal(f.node('server-automation-mode').textContent,'Резервирование · требуется восстановление');
 assert.equal(f.node('reserve-enabled').disabled,true);
 assert.equal(f.node('reserve-repair').hidden,false);
 await f.node('reserve-repair').fire('click');await settle();
 assert.deepEqual(f.writes,[{enabled:true,revision:rev}]);
 f.data(status(true));await f.terminal('completed');
 assert.equal(f.node('reserve-repair').hidden,true);
 assert.equal(f.node('server-automation-mode').textContent,'Резервирование');
 assert.match(f.node('server-automation-mode').className,/\bstatus-success\b/);
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
