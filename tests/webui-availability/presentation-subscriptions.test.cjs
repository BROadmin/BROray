const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');

// A small DOM fixture; production rendering/event handlers run unchanged.
class Element {
    constructor(tag='div'){this.tagName=tag.toUpperCase();this.children=[];this.attributes={};this.dataset={};this.handlers={};this.className='';this._text='';this._html='';this.value='';this.hidden=false;this.classList={add:n=>{this.className+=' '+n;},remove:n=>{this.className=this.className.split(' ').filter(x=>x!==n).join(' ');},toggle:()=>{}};}
    appendChild(n){this.children.push(n);n.parentNode=this;return n;}
    append(...nodes){nodes.forEach(n=>this.appendChild(n));}
    set textContent(v){this._text=String(v);this.children=[];} get textContent(){return this._text+this.children.map(n=>n.textContent).join('');}
    set innerHTML(v){this._html=v;this.children=[];this._text='';} get innerHTML(){return this._html;}
    setAttribute(k,v){this.attributes[k]=String(v);if(k==='id')this.id=String(v);if(k.startsWith('data-'))this.dataset[k.slice(5).replace(/-([a-z])/g,(_,c)=>c.toUpperCase())]=String(v);}
    getAttribute(k){return this.attributes[k]??null;} hasAttribute(k){return k in this.attributes;} removeAttribute(k){delete this.attributes[k];}
    addEventListener(k,fn){this.handlers[k]=fn;} dispatch(k){return this.handlers[k]?.call(this,{target:this,preventDefault(){}});}
    matches(q){if(q.startsWith('.'))return this.className.split(' ').includes(q.slice(1));if(q.startsWith('['))return this.hasAttribute(q.slice(1,-1))||q==='[data-check-server]'&&!!this.dataset.checkServer;return this.tagName===q.toUpperCase();}
    querySelectorAll(q){return this.children.flatMap(n=>[...(n.matches(q)?[n]:[]),...n.querySelectorAll(q)]);}
    querySelector(q){return this.querySelectorAll(q)[0]||null;}
    contains(n){return n===this||this.children.some(c=>c.contains(n));}
    closest(q){return this.matches(q)?this:this.parentNode?.closest(q);}
    scrollIntoView(){} focus(){} reset(){}
}
function fixture(file,exports){
    const ids=new Map(),root=new Element('html'),calls=[],toasts=[];
    const document={readyState:'loading',addEventListener(){},documentElement:root,createElement(t){const n=new Element(t);n.focus=()=>{document.activeElement=n;};return n;},getElementById(id){const find=n=>n.id===id?n:n.children.map(find).find(Boolean);const existing=find(root);if(existing)return existing;if(!ids.has(id)){const n=new Element();n.id=id;ids.set(id,n);root.append(n);}return ids.get(id);},querySelectorAll:q=>root.querySelectorAll(q)};
    document.querySelector=q=>root.querySelector(q);document.handlers={};document.addEventListener=(k,fn)=>{document.handlers[k]=fn;};
    const window={handlers:{},addEventListener(k,fn){this.handlers[k]=fn;},setTimeout:()=>1,clearTimeout(){},location:{origin:'http://fixture.test'},BROrayDialogs:{confirm:async()=>true}};
    const ui={hasQueuedRequest:()=>false,queueLabel:r=>r.state,toast:(...a)=>toasts.push(a),followQueued:async(url,id,cb)=>{calls.push({url,id});},apiRequest:async(url,options)=>{calls.push({url,options});return {};}};
    window.BROrayUI=ui;
    const context={window,document,BROrayUI:ui,Map,Set,AbortController,fetch:(...a)=>context.fetcher(...a)};
    let code=fs.readFileSync(path.join(__dirname,'../../webui/static/assets/js/',file),'utf8');
    if(file==='servers.js')code=code.replace(/    bindEvents\(\);\s*initialize\(\);\s*(?=\}\)\(\);)/,'');
    code=code.replace(/\}\)\(\);\s*$/,'window.fixture={'+exports+'};})();');
    vm.runInNewContext(code,context);
    return {f:window.fixture,document,window,ui,calls,toasts,context,ids};
}
const tick=()=>new Promise(r=>setImmediate(r));
const row=(id,active=false,source='subscription')=>({id,displayGroupId:'a',name:'NL',active,sourceType:source,subscriptionName:id==='b'?'<JSON source>':id,subscriptionId:id,protocol:'vless',transport:'xhttp',security:'reality',quality:{status:id==='b'?'available':'unavailable',ping:id==='b'?12:99,freshness:'fresh'}});
function serverFixture(){const h=fixture('servers.js','state,renderServers,renderSummary');h.f.state.summary={servers:[row('a'),row('b',true),row('c',false,'manual')]};return h;}
test('one card per group defaults to active and keeps explicit source across render',()=>{
    const h=serverFixture();h.f.renderServers(h.f.state.summary.servers);
    const list=h.document.getElementById('servers-list');assert.equal(list.children.length,1);
    let card=list.children[0];assert.equal(card.getAttribute('data-server-id'),'b');
    let select=card.querySelector('select');assert.ok(select);assert.equal(select.value,'b');
    assert.equal(card.querySelector('label').getAttribute('for'),select.id);
    assert.equal(select.children[1].textContent.includes('<JSON source>'),true);
    select.value='a';select.dispatch('change');
    assert.equal(h.calls.length,0,'selection must not activate');
    assert.equal(h.document.activeElement,list.children[0].querySelector('select'),'source control must retain keyboard focus');
    h.f.renderServers(h.f.state.summary.servers);card=list.children[0];
    assert.equal(card.getAttribute('data-server-id'),'a');assert.match(card.textContent,/Активный источник: <JSON source>/);
    assert.match(card.textContent,/99 мс/);assert.equal(card.querySelector('.server-card-same'),null);
    assert.match(h.document.getElementById('servers-list-description').textContent,/1.*3/);
    h.f.state.summary.servers=[row('a'),row('b'),row('c',true,'manual')];h.f.renderServers(h.f.state.summary.servers);
    assert.equal(list.children[0].getAttribute('data-server-id'),'c','new activation selects exact active member');
});
test('selected variant owns Check Connect Details and Delete actions',async()=>{
    const h=serverFixture();h.f.renderServers(h.f.state.summary.servers);let card=h.document.getElementById('servers-list').children[0];
    const select=card.querySelector('select');assert.ok(select);select.value='a';select.dispatch('change');card=h.document.getElementById('servers-list').children[0];
    card.querySelectorAll('button').find(n=>n.textContent==='Проверить').dispatch('click');
    card.querySelectorAll('button').find(n=>n.textContent==='Подключить').dispatch('click');await tick();
    h.ui.apiRequest=async(url,options)=>{h.calls.push({url,options});if(url.includes('details'))return {id:'a'};return {servers:h.f.state.summary.servers};};
    card.querySelector('.server-details-toggle').dispatch('click');await tick();
    card.querySelectorAll('button').find(n=>n.textContent==='Удалить сервер').dispatch('click');await tick();
    assert.ok(h.calls.some(c=>c.url.includes('/check.')&&c.id==='a'));assert.ok(h.calls.some(c=>c.url.includes('/activate.')&&c.id==='a'));
    assert.ok(h.calls.some(c=>c.url.includes('/details.cgi?id=a')));assert.ok(h.calls.some(c=>c.url.includes('/delete.')&&c.options.body.id==='a'));
});
test('manual deterministic fallback survives missing selection and singleton ids do not merge',()=>{
    const h=serverFixture();h.f.state.summary.servers=[row('b'),row('a'),row('c',false,'manual'),{...row('other'),displayGroupId:'other'}];
    h.f.renderServers(h.f.state.summary.servers);const list=h.document.getElementById('servers-list');assert.equal(list.children.length,2);assert.equal(list.children[0].getAttribute('data-server-id'),'c');
    h.f.state.summary.servers=[row('b'),row('a')];h.f.renderServers(h.f.state.summary.servers);assert.equal(list.children[0].getAttribute('data-server-id'),'a');
});
test('active summary remains exact while queued check follows inspected source across redraw',async()=>{
    const h=serverFixture();const rows=h.f.state.summary.servers;h.f.renderSummary({servers:rows,activeServer:rows[1],total:3});
    const list=h.document.getElementById('servers-list');let select=list.children[0].querySelector('select');select.value='a';select.dispatch('change');
    assert.equal(h.document.getElementById('check-active-server').getAttribute('data-server-id'),'b');assert.match(h.document.getElementById('active-server-meta').textContent,/12 мс/);
    let listener;h.ui.followQueued=async(url,id,cb)=>{h.calls.push({url,id});listener=cb;cb({state:'queued'});};h.ui.hasQueuedRequest=(url,id)=>id==='a';
    h.f.renderServers(rows);const button=list.children[0].querySelectorAll('button').find(n=>n.dataset.checkServer==='a');assert.equal(button.disabled,true);assert.match(button.textContent,/queued/);
    assert.ok(h.calls.every(c=>c.id==='a'));assert.equal(typeof listener,'function');
});

const metadata=(count=3,revision='1',id='A')=>({id,name:id,enabled:true,serversCount:count,lastUpdateStatus:'success',lastUpdatedAt:revision,updatedAt:revision,lastUpdateResult:{accepted:count},updateOperation:{id:'op-'+revision}});
const nodes=(names)=>({items:names.map(name=>({name,enabled:true,network:name.includes('XDRIVE')?'xdrive':'xhttp'})),total:names.length});
const response=body=>({status:200,ok:true,json:async()=>({success:true,data:body})});
function subscriptionFixture(){
    const h=fixture('subscriptions.js','state,loadAll,toggleServers,refreshSubscription,renderNodes,removeSubscription,bindEvents');
    h.catalog=[metadata()];h.nodes=nodes(['NL','FIN','PL']);h.gets=0;
    h.context.fetcher=async(url)=>{if(url.includes('/list.'))return response(h.catalog);if(url.includes('/summary.'))return response({});if(url.includes('/servers.')){h.gets++;return response(h.nodes);}if(url.includes('/delete.')){h.catalog=[];return response({});}throw Error(url);};
    return h;
}
test('opened subscription refreshes 3 to 4 XDRIVE and same-count replacement without collapse or repeated reads',async()=>{
    const h=subscriptionFixture();await h.f.loadAll(true);await h.f.toggleServers('A');assert.equal(h.gets,1);
    h.nodes=nodes(['NL','FIN','PL','NL XDRIVE GOOGLE']);h.catalog=[metadata(4,'2')];await h.f.loadAll(true);await tick();
    assert.match(h.f.renderNodes('A'),/NL XDRIVE GOOGLE/);assert.equal(h.f.state.openServersId,'A');assert.equal(h.gets,2);
    h.nodes=nodes(['NL','FIN','PL','replacement']);h.catalog=[metadata(4,'3')];await h.f.loadAll(true);await tick();assert.match(h.f.renderNodes('A'),/replacement/);assert.equal(h.gets,3);
    await h.f.loadAll(true);await h.f.loadAll(true);await tick();assert.equal(h.gets,3,'unchanged metadata must not refetch nodes');
});
test('terminal refresh invalidates opened nodes even with unchanged catalog metadata',async()=>{
    const h=subscriptionFixture();await h.f.loadAll(true);await h.f.toggleServers('A');h.nodes=nodes(['new']);
    h.ui.followQueued=async(url,id,cb)=>cb({state:'completed'});await h.f.refreshSubscription('A',null);await tick();await tick();assert.match(h.f.renderNodes('A'),/new/);assert.equal(h.gets,2);
});
test('old response cannot overwrite a newer catalog and switching subscriptions remains stable',async()=>{
    const h=subscriptionFixture();let finishOld;const normal=h.context.fetcher;let first=true;
    h.context.fetcher=async(url)=>{if(url.includes('/servers.')&&first){first=false;h.gets++;return new Promise(r=>finishOld=()=>r(response(nodes(['old']))));}return normal(url);};
    await h.f.loadAll(true);const old=h.f.toggleServers('A');h.nodes=nodes(['new']);h.catalog=[metadata(1,'2'),metadata(1,'2','B')];await h.f.loadAll(true);await tick();finishOld();await old;
    assert.match(h.f.renderNodes('A'),/new/);assert.doesNotMatch(h.f.renderNodes('A'),/>old</);
    await h.f.toggleServers('B');assert.equal(h.f.state.openServersId,'B');assert.match(h.f.renderNodes('B'),/new/);
});
test('node request error retains last good data and is not shown as true empty or retried by polling',async()=>{
    const h=subscriptionFixture();await h.f.loadAll(true);await h.f.toggleServers('A');const normal=h.context.fetcher;
    h.context.fetcher=async(url)=>{if(url.includes('/servers.')){h.gets++;throw Error('offline');}return normal(url);};h.catalog=[metadata(3,'2')];await h.f.loadAll(true);await tick();
    assert.match(h.f.renderNodes('A'),/>NL</);assert.match(h.f.renderNodes('A'),/Не удалось/);assert.doesNotMatch(h.f.renderNodes('A'),/Серверы отсутствуют/);
    await h.f.loadAll(true);await tick();assert.equal(h.gets,2);
});
test('disable reloads nodes and remove rejects in-flight response',async()=>{
    const h=subscriptionFixture();await h.f.loadAll(true);await h.f.toggleServers('A');h.catalog=[{...metadata(),enabled:false}];h.nodes=nodes([]);await h.f.loadAll(true);await tick();assert.equal(h.gets,2);
    let finish;const normal=h.context.fetcher;h.context.fetcher=async(url)=>url.includes('/servers.')?new Promise(r=>finish=()=>r(response(nodes(['removed'])))):normal(url);
    h.catalog=[metadata(1,'3')];await h.f.loadAll(true);await tick();await h.f.removeSubscription(h.catalog[0]);finish();await tick();
    assert.equal(h.f.state.serverCache.has('A'),false);assert.equal(h.f.state.openServersId,null);
});
test('returning to page refreshes an external revision with one coalesced catalog read',async()=>{
    const h=subscriptionFixture();await h.f.loadAll(true);await h.f.toggleServers('A');h.f.bindEvents();
    h.nodes=nodes(['NL','FIN','PL','NL XDRIVE GOOGLE']);h.catalog=[metadata(4,'external')];let lists=0;const normal=h.context.fetcher;
    h.context.fetcher=async(url)=>{if(url.includes('/list.'))lists++;return normal(url);};
    assert.equal(typeof h.window.handlers.focus,'function');h.window.handlers.focus();h.document.handlers.visibilitychange();await tick();await tick();
    assert.equal(lists,1);assert.equal(h.gets,2);assert.match(h.f.renderNodes('A'),/NL XDRIVE GOOGLE/);
});
test('first node request error renders an error, then explicit reopen retries successfully',async()=>{
    const h=subscriptionFixture();await h.f.loadAll(true);const normal=h.context.fetcher;h.context.fetcher=async(url)=>{if(url.includes('/servers.'))throw Error('offline');return normal(url);};
    await h.f.toggleServers('A');assert.match(h.f.renderNodes('A'),/Не удалось/);assert.doesNotMatch(h.f.renderNodes('A'),/Серверы отсутствуют/);assert.equal(h.f.state.serverCache.has('A'),false);
    h.context.fetcher=normal;await h.f.toggleServers('A');await h.f.toggleServers('A');assert.match(h.f.renderNodes('A'),/>NL</);
});
test('switching A to B during node request keeps B open when A completes',async()=>{
    const h=subscriptionFixture();h.catalog=[metadata(),metadata(1,'1','B')];await h.f.loadAll(true);let finish;const normal=h.context.fetcher;
    h.context.fetcher=async(url)=>url.endsWith('id=A')?new Promise(r=>finish=()=>r(response(nodes(['A result'])))):normal(url);
    const pending=h.f.toggleServers('A');h.nodes=nodes(['B result']);await h.f.toggleServers('B');finish();await pending;
    assert.equal(h.f.state.openServersId,'B');assert.match(h.f.renderNodes('B'),/B result/);assert.equal(h.f.renderNodes('A'),'');
});
test('older catalog request cannot restore removed subscription',async()=>{
    const h=subscriptionFixture();await h.f.loadAll(true);let finish;const normal=h.context.fetcher;let old=true;
    h.context.fetcher=async(url)=>{if(url.includes('/list.')&&old){old=false;return new Promise(r=>finish=()=>r(response([metadata()])));}return normal(url);};
    const pending=h.f.loadAll(true);h.catalog=[];await h.f.loadAll(true);finish();await pending;assert.equal(h.f.state.subscriptions.length,0);
});
test('affected page assets bypass cached candidate5 scripts',()=>{
    for(const name of ['servers','subscriptions'])assert.match(fs.readFileSync(path.join(__dirname,'../../webui/static/'+name+'.html'),'utf8'),new RegExp('/assets/js/'+name+'\\.js\\?v=WebUI-3\\.5\\.7-c6'));
});

 test('update warnings are a collapsed disclosure with escaped messages',()=>{
 const h=fixture('subscriptions.js','renderWarnings');
 const html=h.f.renderWarnings({lastUpdateResult:{warnings:['<private>','second']}});
 assert.match(html, /^<details\b/); assert.match(html, /<summary>Предупреждения обновления<\/summary>/);
 assert.doesNotMatch(html, /<details[^>]*\bopen(?:[\s=>])/);assert.match(html,/&lt;private&gt;/);
 assert.equal(h.f.renderWarnings({lastUpdateResult:{warnings:[]}}),'');
 });
test('saved server warning remains visible after import and reload',()=>{
 const h=serverFixture();h.f.state.summary.servers.forEach(r=>r.importWarnings=['Synthetic warning <value>']);h.f.renderServers(h.f.state.summary.servers);
 const card=h.document.getElementById('servers-list').children[0];assert.match(card.textContent,/Synthetic warning <value>/);assert.ok(card.querySelector('details'));assert.equal(card.querySelector('details').hasAttribute('open'),false);
});
test('DNS unsaved selection does not reuse saved matchesSelection',()=>{
 const h=fixture('dns.js','selectionMatches,setFixture:function(s,p){status=s;selected=p;}');
 const a={id:'a',address:'192.0.2.1',port:853,sni:'a.example',present:true};const b={id:'b',address:'192.0.2.2',port:853,sni:'b.example',present:true};
 h.f.setFixture({servers:[a,b],selectedIds:['a','b'],managed:[a,b],matchesSelection:true},{a:true,b:false});assert.equal(h.f.selectionMatches(),false);
});
test('additional JSON and Clash switches reset off, load independently, and submit booleans',async()=>{
 const h=fixture('subscriptions.js','state,resetForm,openEdit,saveForm');let saved;
 h.f.resetForm();assert.equal(h.document.getElementById('subscription-fetch-json').checked,false);assert.equal(h.document.getElementById('subscription-fetch-clash').checked,false);
 h.context.fetcher=async(url,opt)=>{if(url.includes('/details.'))return response({id:'test',name:'Synthetic',url:'https://example.test/sub',fetchJSON:true,fetchClash:false});if(url.includes('/update.')){saved=JSON.parse(opt.body);return response({});}if(url.includes('/list.'))return response([]);return response({});};
 await h.f.openEdit('test');assert.equal(h.document.getElementById('subscription-fetch-json').checked,true);assert.equal(h.document.getElementById('subscription-fetch-clash').checked,false);
 h.document.getElementById('subscription-fetch-json').checked=false;h.document.getElementById('subscription-fetch-clash').checked=true;await h.f.saveForm({preventDefault(){}});
 assert.equal(saved.fetchJSON,false);assert.equal(saved.fetchClash,true);
 const html=fs.readFileSync(path.join(__dirname,'../../webui/static/subscriptions.html'),'utf8');for(const id of ['subscription-fetch-json','subscription-fetch-clash']){assert.match(html,new RegExp('id="'+id+'"[^>]*type="checkbox"'));assert.doesNotMatch(html,new RegExp('id="'+id+'"[^>]*checked'));}
});
