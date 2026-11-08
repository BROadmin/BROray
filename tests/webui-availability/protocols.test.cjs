const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
function load(apiRequest){
 function element(tag){return {tag,children:[],textContent:'',innerHTML:'',appendChild(n){this.children.push(n);},setAttribute(){}};}
 const root=element('section');
 const document={readyState:'loading',addEventListener(){},querySelector(){return null;},getElementById(id){return id==='server-import-preflight'?root:null;},createElement:element};
 const window={setTimeout(fn){fn();}};
 const code=fs.readFileSync(path.join(__dirname,'../../webui/static/assets/js/servers-import-preflight.js'),'utf8');
 vm.runInNewContext(code.replace(/\}\)\(\);\s*$/,'window.test={parseInput,previewItems,renderPreview,importNext,state};})();'),{window,document,BROrayUI:{apiRequest},console,Promise});
 return {...window.test,root};
}
function all(n){return [n,...n.children.flatMap(all)];}
for(const raw of ['socks5://user:secret@example.com:1080','http://example.com:8080','https://example.com',';comment\n[Interface]\nPrivateKey = synthetic\n[Peer]\nPublicKey = synthetic']){
 test('new profile reaches preview and save: '+raw.split(':')[0].slice(0,10),async()=>{
  const calls=[];const ui=load(async(url,options)=>{calls.push(options.body);return {success:true,data:options.body.preview?{protocol:'http',name:'Test',canonical:'a'.repeat(64),importWarnings:['Тестовое предупреждение'],connectionSupport:{supported:true,udp:false,message:'Только TCP'}}:{message:'saved'}};});
  const items=ui.parseInput(raw);assert.equal(items.length,1);assert.equal(items[0].error,null);
  await ui.previewItems(items);ui.state.items=items;ui.renderPreview();
  assert.ok(all(ui.root).some(n=>n.textContent.includes('Тестовое предупреждение')));
  assert.ok(all(ui.root).some(n=>n.textContent.includes('Только TCP')));
  assert.ok(!all(ui.root).some(n=>n.textContent.includes('synthetic')||n.textContent.includes('secret')));
  await ui.importNext(0);assert.equal(items[0].importState,'imported');assert.equal(calls.length,2);assert.ok(calls.every(c=>c.uri===raw));
 });
}
