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
test('JSON arrays and Base64 envelopes reach preview and save without decoding or reserializing',async()=>{
 const array='[\n {"protocol":"vless","settings":{"port":443,"port":8443}}\n]';
 const encoded=Buffer.from(array).toString('base64');
 for(const raw of [array, encoded.slice(0,12)+'\r\n\t '+encoded.slice(12)]){
  const calls=[];
  const ui=load(async(url,options)=>{calls.push(options.body);return {success:true,data:options.body.preview?{protocol:'vless',name:'Synthetic',canonical:'a'.repeat(64),connectionSupport:{supported:true}}:{message:'saved'}};});
  const items=ui.parseInput(raw);assert.equal(items.length,1);assert.equal(items[0].error,null);assert.equal(items[0].raw,raw);
  await ui.previewItems(items);ui.state.items=items;await ui.importNext(0);
  assert.equal(calls.length,2);assert.ok(calls.every(c=>c.uri===raw));
 }
});
test('native JSON stays one private input and explains the required core before saving',async()=>{
 const raw=JSON.stringify({protocol:'masque',settings:{address:'example.test',port:443},streamSettings:{masqueSettings:{pass:'synthetic-private'}}},null,2);
 const calls=[];
 const ui=load(async(url,options)=>{calls.push(options.body);return {success:true,data:options.body.preview?{protocol:'masque',name:'Native',transport:'masque',canonical:'a'.repeat(64),connectionSupport:{supported:false,mode:'requires-core',minimumCore:'26.11.9',message:'Обновите Xray до 26.11.9',notice:'Проверенный импорт JSON'}}:{message:'saved'}};});
 const items=ui.parseInput(raw);assert.equal(items.length,1);assert.equal(items[0].error,null);
 await ui.previewItems(items);ui.state.items=items;ui.renderPreview();
 const text=all(ui.root).map(n=>n.textContent).join('\n');
 assert.match(text,/26\.11\.9/);assert.doesNotMatch(text,/Только сохранение|synthetic-private/);
 await ui.importNext(0);assert.equal(items[0].importState,'imported');assert.ok(calls.every(c=>c.uri===raw));
});
test('Clash YAML reaches preview and save as one exact private document',async()=>{
 for(const raw of ['# synthetic export\r\nproxies:\r\n  - name: Synthetic\r\n    type: vless\r\n    server: edge.example.test\r\n    port: 443\r\n', 'name: Synthetic\ntype: tuic\nserver: edge.example.test\nport: 443\npassword: synthetic-private\n']){
  const calls=[];
  const ui=load(async(url,options)=>{calls.push(options.body);return {success:true,data:options.body.preview?{protocol:'tuic',name:'Synthetic',canonical:'a'.repeat(64),connectionSupport:{supported:false,message:'Сохранено; подключение недоступно'}}:{message:'saved'}};});
  const items=ui.parseInput(raw);assert.equal(items.length,1);assert.equal(items[0].error,null);assert.equal(items[0].raw,raw);
  await ui.previewItems(items);ui.state.items=items;ui.renderPreview();
  assert.ok(!all(ui.root).some(n=>n.textContent.includes('synthetic-private')));
  await ui.importNext(0);assert.equal(calls.length,2);assert.ok(calls.every(c=>c.uri===raw));
 }
});
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
