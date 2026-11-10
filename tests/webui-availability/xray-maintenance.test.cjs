const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const tick=()=>new Promise(r=>setImmediate(r));
function harness(accept=true){
 const nodes=new Map(),calls=[],dialogs=[];
 function element(){const listeners={};const children=[];return {listeners,children,childNodes:children,dataset:{},disabled:false,checked:false,hidden:false,textContent:'',value:'',classList:{add(){},remove(){},toggle(){},contains(){return false}},querySelector(){return null},setAttribute(){},removeAttribute(){},getAttribute(){return null},appendChild(c){children.push(c)},addEventListener(k,f){listeners[k]=f},remove(){}};}
 const document={getElementById(id){if(!nodes.has(id))nodes.set(id,element());return nodes.get(id)},createElement:element};
 const window={setInterval(){},setTimeout(){return 1},clearTimeout(){},BROrayUI:{apiRequest:async(url,opt)=>{calls.push({url,body:opt.body});if(url.includes('operation-status'))return {data:{operationRunning:false}};if(url.includes('status.cgi'))return {data:{installed:true,version:'26.11.9',configExists:true,configValid:true}};return {success:true,data:{accepted:true,operationId:'fixture-op'}}},toast(){}},BROrayDialogs:{confirm:async d=>{dialogs.push(d);return accept}}};
 let source=fs.readFileSync(path.join(__dirname,'../../webui/static/assets/js/xray.js'),'utf8');
 source=source.replace('    bindEvents();\n    initialize();',`    bindEvents(); window.fixture={renderStatus,installRelease,applyControlState,spaceReady,setState:function(s,u,rows){latestStatus=s;Object.assign(latestUpdate,u);releaseCatalog=rows;catalogCurrentVersion=s.version||"";selectedTag=rows[0].tagName;}};`);
 vm.runInNewContext(source,{window,document,setTimeout(){return 1},clearTimeout(){},console});
 const row={tagName:'v26.11.9',version:'26.11.9',available:true,installed:true,archiveSha256:'a'.repeat(64),prerelease:false,asset:{size:10},compatibility:{status:'compatible'}};
 window.fixture.setState({installed:true,version:'26.11.9',configExists:true,configValid:true},{checked:true,storageOk:true,reinstallAllowed:true,ramVerified:true,freeBytes:1e9,requiredBytes:1,lowSpaceAllowed:true,lowSpaceRequiredBytes:1,optFreeBytes:1e9,optRequiredBytes:1},[row]);
 return {api:window.fixture,row,nodes,calls,dialogs,document};
}
test('low-space confirmation describes volatile rollback before sending consent',async()=>{
 const h=harness(false);h.document.getElementById('xray-low-space').checked=true;
 h.api.installRelease(h.row,h.document.getElementById('xray-update-install'));await tick();
 assert.equal(h.dialogs.length,1);assert.match(h.dialogs[0].message,/питани/);assert.match(h.dialogs[0].message,/RAM/);assert.equal(h.calls.length,0);
});
test('low-space request contains explicit consent and is sent once',async()=>{
 const h=harness();h.document.getElementById('xray-low-space').checked=true;
 h.api.installRelease(h.row,h.document.getElementById('xray-update-install'));await tick();
 const sent=h.calls.filter(x=>x.url.endsWith('/install.cgi'));assert.equal(sent.length,1);assert.equal(sent[0].body.allowLowSpace,true);
});
test('core removal has its own confirmation and preserves-data explanation',async()=>{
 const h=harness(false);const button=h.document.getElementById('xray-uninstall');
 assert.equal(typeof button.listeners.click,'function');button.listeners.click({currentTarget:button});await tick();
 assert.equal(h.dialogs.length,1);assert.match(h.dialogs[0].message,/сохран/);assert.equal(h.calls.length,0);
});
test('space budget follows selected release instead of an oversized latest release',()=>{
 const h=harness();h.row.asset.size=10*1048576;
 h.api.setState({installed:true,version:'26.11.9',binarySizeBytes:40*1048576},{checked:true,storageOk:false,reinstallAllowed:false,ramVerified:true,freeBytes:80*1048576,requiredBytes:120*1048576,optFreeBytes:48*1048576,optRequiredBytes:100*1048576},[h.row]);
 assert.equal(h.api.spaceReady(h.row),true);
});

test('running maintenance is not described as interrupted and completion restores controls',()=>{
 const h=harness();
 for(const state of ['installing','removing']){
  h.api.renderStatus({installed:true,version:'26.11.9',configExists:true,configValid:true,running:true,maintenanceStatus:state});
  const note=h.document.getElementById('xray-maintenance-note');
  assert.equal(note.hidden,false);assert.doesNotMatch(note.textContent,/прерван|утрат/);assert.match(note.textContent,/Выполняется/);
  assert.equal(h.document.getElementById('xray-uninstall').disabled,true);
 }
 h.api.renderStatus({installed:true,version:'26.11.9',configExists:true,configValid:true,running:true,maintenanceStatus:'ready'});
 assert.equal(h.document.getElementById('xray-maintenance-note').hidden,true);
 assert.equal(h.document.getElementById('xray-uninstall').disabled,false);
 h.api.renderStatus({installed:false,maintenanceStatus:'interrupted'});
 assert.match(h.document.getElementById('xray-maintenance-note').textContent,/прерван/);
});
