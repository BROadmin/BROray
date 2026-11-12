const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');

function loadCards() {
  function element(tag) {
    const attrs={}, handlers={};
    return {tag,children:[],dataset:{},disabled:false,textContent:'',className:'',
      append(...children){this.children.push(...children);},appendChild(child){this.children.push(child);return child;},
      setAttribute(k,v){attrs[k]=v;},getAttribute(k){return attrs[k];},addEventListener(k,f){handlers[k]=f;},
      click(){if(!this.disabled)handlers.click?.();}};
  }
  const document={getElementById(){return element('div');},createElement:element};
  const window={};
  const ctx=vm.createContext({window,document,BROrayUI:{},console,Date});
  const code=fs.readFileSync(path.join(__dirname,'../../webui/static/assets/js/servers.js'),'utf8');
  const end='    bindEvents();\n    initialize();';
  assert.ok(code.replaceAll('\r','').includes(end));
  vm.runInContext(code.replaceAll('\r','').replace(end,'    window.renderCard = renderServerCard;'),ctx);
  // renderServerCard has taken a presentation group since candidate3.
  return server => window.renderCard(server,{id:server.id,active:server.active===true,members:[server]});
}
function all(node){return [node,...node.children.flatMap(all)];}
test('stored AWG has explicit status and disabled connect/check while details remain usable',()=>{
  const render=loadCards();
  const card=render({id:'awg',name:'AWG',protocol:'amneziawg',quality:{status:'unsupported',freshness:'not-applicable'},connectionSupport:{supported:false,message:'Конфигурация AmneziaWG сохранена. Подключение пока не поддерживается BROray.'}});
  const nodes=all(card);
  for(const label of ['Подключить','Проверить'])assert.equal(nodes.find(n=>n.tag==='button'&&n.textContent===label)?.disabled,true,label);
  assert.equal(nodes.find(n=>n.tag==='button'&&n.textContent==='Подробнее')?.disabled,false);
  assert.ok(nodes.some(n=>n.textContent.includes('Подключение пока не поддерживается')));
});
test('existing supported servers retain working actions',()=>{
  const nodes=all(loadCards()({id:'normal',name:'Normal',protocol:'vless',quality:{}}));
  for(const label of ['Подключить','Проверить','Подробнее'])assert.equal(nodes.find(n=>n.tag==='button'&&n.textContent===label)?.disabled,false,label);
});

test('cloud quality distinguishes tunnel latency and establishment from ICMP ping',()=>{
  const nodes=all(loadCards()({id:'drive',name:'Drive',protocol:'vless',transport:'xdrive',quality:{status:'available',measurementMethod:'https-proxy-warm',ping:120,jitter:12,establishmentMs:850}}));
  const text=nodes.map(n=>n.textContent).join('\n');
  assert.match(text,/Задержка через туннель/);
  assert.match(text,/Установление туннеля/);
  assert.match(text,/850 мс/);
  assert.doesNotMatch(text,/Пинг|ICMP/);
});
test('a core upgrade requirement differs from a stored-only unsupported protocol',()=>{
 const nodes=all(loadCards()({id:'drive',name:'Drive',protocol:'vless',transport:'xdrive',quality:{},connectionSupport:{supported:false,mode:'requires-core',minimumCore:'26.11.9',message:'Обновите Xray до 26.11.9',notice:'XDRIVE: версии клиента и сервера должны совпадать'}}));
 assert.ok(nodes.some(n=>n.textContent==='Обновите Xray'));
 assert.ok(nodes.some(n=>n.textContent.includes('версии клиента и сервера')));
 assert.equal(nodes.find(n=>n.tag==='button'&&n.textContent==='Подключить')?.disabled,true);
});
test('HTTP card keeps TCP-only capability visible without disabling connection',()=>{
  const nodes=all(loadCards()({id:'http',name:'Proxy',protocol:'http',quality:{},connectionSupport:{supported:true,udp:false,message:'Только TCP'}}));
  assert.ok(nodes.some(n=>n.textContent.includes('Только TCP')));
  assert.equal(nodes.find(n=>n.tag==='button'&&n.textContent==='Подключить')?.disabled,false);
});
test('AWG is absent from preferred automatic-switch choices',()=>{
  const select={innerHTML:'',children:[],appendChild(n){this.children.push(n);}};
  const document={readyState:'loading',addEventListener(){},getElementById(){return select;},createElement(){return {};}};
  const window={};
  const ctx=vm.createContext({window,document,console});
  const code=fs.readFileSync(path.join(__dirname,'../../webui/static/assets/js/servers-auto-switch.js'),'utf8');
  vm.runInContext(code.replace('})();','window.fillServers=fillServers;})();'),ctx);
  window.fillServers({servers:[{id:'awg',connectionSupport:{supported:false}},{id:'normal'}]},'');
  assert.deepEqual(select.children.map(x=>x.value),['normal']);
});

function loadPreflight(apiRequest) {
  function element(tag) {
    return {tag,children:[],textContent:'',innerHTML:'',appendChild(n){this.children.push(n);},setAttribute(){}};
  }
  const root=element('section');
  const document={readyState:'loading',addEventListener(){},querySelector(){return null;},
    getElementById(id){return id==='server-import-preflight'?root:null;},createElement:element};
  const window={setTimeout(fn){fn();}};
  const ctx=vm.createContext({window,document,BROrayUI:{apiRequest},console,Promise});
  const code=fs.readFileSync(path.join(__dirname,'../../webui/static/assets/js/servers-import-preflight.js'),'utf8');
  vm.runInContext(code.replace(/\}\)\(\);\s*$/, 'window.testPreflight={parseInput,previewItems,renderPreview,importNext,state};})();'),ctx);
  return {...window.testPreflight,root};
}

test('manual AWG reaches backend preview and save with parse-only warning',async()=>{
  const calls=[];
  const ui=loadPreflight(async(url,options)=>{
    calls.push({url,body:options.body});
    return {success:true,data:options.body.preview?{protocol:'amneziawg',name:'AWG',host:'awg.example',port:443,
      transport:'udp',security:'amneziawg',canonical:'a'.repeat(64),connectionSupport:{supported:false}}:{message:'saved'}};
  });
  const items=ui.parseInput('vpn://synthetic-opaque-profile');
  assert.equal(items[0].error,null,'browser must delegate vpn container validation to backend');
  await ui.previewItems(items);
  assert.equal(items[0].error,null);
  ui.state.items=items;
  ui.renderPreview();
  assert.ok(all(ui.root).some(n=>n.textContent==='Только сохранение'));
  assert.ok(all(ui.root).some(n=>n.textContent.includes('без возможности подключения')));
  await ui.importNext(0);
  assert.equal(items[0].importState,'imported');
  assert.equal(calls.length,2);
  assert.equal(calls[0].body.preview,true);
  assert.equal(calls[1].body.preview,undefined);
  assert.ok(calls.every(c=>c.url==='/api/servers/import.cgi'&&c.body.uri==='vpn://synthetic-opaque-profile'));
});

test('unknown or malformed vpn profile stays rejected when backend refuses it',async()=>{
  const ui=loadPreflight(async()=>{throw {payload:{error:{message:'AmneziaWG: тип контейнера пока не поддерживается'}}};});
  const items=ui.parseInput('vpn://other-protocol');
  assert.equal(items[0].error,null);
  await ui.previewItems(items);
  assert.match(items[0].error,/тип контейнера пока не поддерживается/);
});
