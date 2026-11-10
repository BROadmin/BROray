const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
function ui(){
 const field={value:'previous'}, chooser={disabled:false,value:''}, notices=[];
 const document={readyState:'loading',addEventListener(){},querySelector(){return null;},getElementById(id){return {'server-import-uri':field,'server-import-file':chooser}[id]||null;}};
 const window={};
 const code=fs.readFileSync(path.join(__dirname,'../../webui/static/assets/js/servers-import-preflight.js'),'utf8');
 vm.runInNewContext(code.replace(/\}\)\(\);\s*$/,'window.fixture={importFile,resetPreflight,state};})();'),{window,document,TextEncoder,TextDecoder,BROrayUI:{toast(m){notices.push(m);}}});
 return {...window.fixture,field,chooser,notices};
}
function file(text){const bytes=new TextEncoder().encode(text);return {size:bytes.length,arrayBuffer:async()=>bytes.buffer};}
test('file loading preserves exact JSON text and requires a separate preview',async()=>{
 const v=ui(),raw='{ "protocol":"masque", "test":0, "duplicate":1, "duplicate":2 }';
 await v.importFile(file(raw));assert.equal(v.field.value,raw);assert.equal(v.state.valid,false);assert.equal(v.state.busy,false);assert.deepEqual(v.notices,[]);assert.equal(v.chooser.disabled,false);
});
test('oversized and malformed UTF-8 files never replace the existing field',async()=>{
 for(const f of [file('x'.repeat(65537)),file('"'.repeat(40000)),{size:2,arrayBuffer:async()=>Uint8Array.from([0xc3,0x28]).buffer}]){
  const v=ui();await v.importFile(f);assert.equal(v.field.value,'previous');assert.equal(v.notices.length,1);assert.equal(v.state.busy,false);
 }
});
test('cancelled or superseded read cannot overwrite later input',async()=>{
 const v=ui();let done;const read=v.importFile({size:5,arrayBuffer:()=>new Promise(resolve=>done=resolve)});
 assert.equal(v.state.busy,true);v.resetPreflight(true);v.field.value='later';done(new TextEncoder().encode('older').buffer);await read;
 assert.equal(v.field.value,'later');assert.deepEqual(v.notices,[]);assert.equal(v.chooser.disabled,false);
});
test('read errors do not expose file exception contents',async()=>{
 const v=ui();await v.importFile({size:5,arrayBuffer:async()=>{throw new Error('private-file-name-secret');}});
 assert.deepEqual(v.notices,['Не удалось прочитать файл конфигурации.']);assert.equal(v.field.value,'previous');
});
