const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');

// A small DOM fixture; production rendering/event handlers run unchanged.
class Element {
    constructor(tag='div'){this.tagName=tag.toUpperCase();this.children=[];this.attributes={};this.dataset={};this.handlers={};this.className='';this._text='';this._html='';this.value='';this.hidden=false;this.classList={add:n=>{this.className+=' '+n;},remove:n=>{this.className=this.className.split(' ').filter(x=>x!==n).join(' ');},toggle:()=>{}};}
    appendChild(n){return this.insertBefore(n,null);}
    get firstElementChild(){return this.children[0]||null;}
    get nextElementSibling(){return this.parentNode?.children[this.parentNode.children.indexOf(this)+1]||null;}
    insertBefore(n,before){if(n===before)return n;if(n.parentNode)n.parentNode.removeChild(n);const i=before===null?this.children.length:this.children.indexOf(before);if(i<0)throw Error('missing reference node');this.children.splice(i,0,n);n.parentNode=this;return n;}
    removeChild(n){const i=this.children.indexOf(n);if(i>=0){this.children.splice(i,1);n.parentNode=null;}return n;}
    replaceChildren(...nodes){this.children.slice().forEach(n=>this.removeChild(n));this.append(...nodes);}
    append(...nodes){nodes.forEach(n=>this.appendChild(n));}
    set textContent(v){this._text=String(v);this.replaceChildren();} get textContent(){return this._text+this.children.map(n=>n.textContent).join('');}
    set innerHTML(v){this._html=v;this.replaceChildren();this._text='';} get innerHTML(){return this._html;}
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
    if(file==='home.js')code=code.replace(/    initialize\(\);\s*(?=\}\)\(\);)/,'');
    code=code.replace(/\}\)\(\);\s*$/,'window.fixture={'+exports+'};})();');
    vm.runInNewContext(code,context);
    return {f:window.fixture,document,window,ui,calls,toasts,context,ids};
}
const tick=()=>new Promise(r=>setImmediate(r));
const row=(id,active=false,source='subscription')=>({id,displayGroupId:'a',name:'NL',active,sourceType:source,subscriptionName:id==='b'?'<JSON source>':id,subscriptionId:id,protocol:'vless',transport:'xhttp',security:'reality',quality:{status:id==='b'?'available':'unavailable',ping:id==='b'?12:99,freshness:'fresh'}});
function serverFixture(){const h=fixture('servers.js','state,renderServers,renderSummary');h.f.state.summary={servers:[row('a'),row('b',true),row('c',false,'manual')]};return h;}

test('configuration warnings appear only after opening server Details',async()=>{
    const h=serverFixture();
    h.f.state.summary.servers.forEach(s=>{s.importWarnings=['configuration-warning-fixture'];});
    h.f.renderServers(h.f.state.summary.servers);
    const card=h.document.getElementById('servers-list').children[0];
    assert.equal(!!card.querySelector('.server-import-warnings'),false,'collapsed card must not duplicate configuration warnings');
    h.ui.apiRequest=async()=>({id:'b',importWarnings:['configuration-warning-fixture']});
    card.querySelector('.server-details-toggle').dispatch('click');await tick();
    const warnings=card.querySelector('.server-import-warnings');
    assert.ok(warnings,'Details must retain warnings');
    assert.match(warnings.textContent,/configuration-warning-fixture/);
});
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
test('sort and quality refresh reuse cards, open details, and a focused control',async()=>{
    const h=serverFixture(), list=h.document.getElementById('servers-list');
    const rows=[{...row('first'),name:'Б',displayGroupId:'first'},{...row('second'),name:'А',displayGroupId:'second'}];
    h.f.state.summary={servers:rows};
    h.f.renderServers(rows);
    const [first,second]=list.children;
    const toggle=first.querySelector('.server-details-toggle');
    h.ui.apiRequest=async()=>({id:'first',importWarnings:['fixture']});
    toggle.dispatch('click');await tick();
    toggle.focus();
    h.document.getElementById('servers-sort-order').value='name';
    h.f.renderServers(rows);
    assert.equal(list.children[0],second);
    assert.equal(list.children[1],first);
    assert.equal(first.querySelector('.server-details-toggle'),toggle);
    assert.equal(h.document.activeElement,toggle);
    assert.equal(toggle.getAttribute('aria-expanded'),'true');
    const header=first.querySelector('.server-card-header');
    const next=[{...rows[0],quality:{...rows[0].quality,ping:11}},rows[1]];
    h.f.state.summary={servers:next};
    h.f.renderServers(next);
    assert.equal(list.children[1],first);
    assert.equal(first.querySelector('.server-card-header'),header);
    assert.match(first.querySelector('.server-card-metrics').textContent,/11 мс/);
    assert.equal(first.querySelector('.server-details-toggle'),toggle);
});
test('a late Details response stays with its original profile',async()=>{
    const h=serverFixture();
    const rows=h.f.state.summary.servers;
    h.f.renderServers(rows);
    const card=h.document.getElementById('servers-list').children[0];
    let finish;
    h.ui.apiRequest=()=>new Promise(resolve=>{finish=resolve;});
    card.querySelector('.server-details-toggle').dispatch('click');
    const select=card.querySelector('select');
    select.value='a';
    select.dispatch('change');
    finish({id:'b',importWarnings:['old profile warning']});
    await tick();
    assert.equal(h.document.getElementById('servers-list').children[0],card);
    assert.equal(card.getAttribute('data-server-id'),'a');
    assert.equal(card.querySelector('.server-details-toggle').getAttribute('aria-expanded'),'false');
    assert.doesNotMatch(card.textContent,/old profile warning/);
});
test('a queued check from the previous active profile cannot disable the new active control',()=>{
    const h=fixture('servers.js','renderSummary,checkServer');
    const rows=[row('a'),row('b',true)];
    h.f.renderSummary({servers:rows,activeServer:rows[1]});
    const active=h.document.getElementById('check-active-server');
    let previousCheck;
    h.ui.followQueued=async(url,id,listener)=>{previousCheck=listener;};
    h.f.checkServer('b',active);
    assert.equal(active.disabled,true);
    const next=[row('a',true),row('b')];
    h.f.renderSummary({servers:next,activeServer:next[0]});
    previousCheck({state:'queued'});
    assert.equal(active.getAttribute('data-server-id'),'a');
    assert.equal(active.disabled,false);
    assert.equal(active.textContent,'Проверить');
});
test('a server badge changes from its confirmed result to checking and back to fresh readback',async()=>{
    const h=serverFixture();
    const initial={...row('solo'),displayGroupId:'solo',quality:{status:'available',freshness:'fresh',ping:12}};
    h.f.renderSummary({servers:[initial]});
    const card=h.document.getElementById('servers-list').children[0];
    const badge=card.querySelector('.server-card-quality');
    assert.equal(badge.textContent,'Доступен');
    let finish;
    h.ui.followQueued=async(url,id,listener)=>{finish=listener;listener({state:'running'});};
    card.querySelectorAll('button').find(button=>button.dataset.checkServer==='solo').dispatch('click');
    assert.equal(badge.textContent,'Проверяется');
    assert.match(badge.className,/status-loading/);
    const latest={...initial,quality:{status:'unavailable',freshness:'fresh'}};
    h.ui.apiRequest=async()=>({servers:[latest]});
    finish({state:'completed'});
    await tick();
    assert.equal(badge.textContent,'Недоступен');
    assert.match(badge.className,/status-badge-danger/);
});
test('server source labels identify formats and distinguish repeated captions',()=>{
    const h=serverFixture();
    const fromProvider=(id,sourceFormats)=>({...row(id),subscriptionName:'Provider',sourceFormats});
    const rows=[
        fromProvider('a',['clash']),
        fromProvider('b',['json','uri']),
        fromProvider('c',['clash']),
        fromProvider('d',[])
    ];
    h.f.state.summary.servers=rows;
    h.f.renderServers(rows);
    const select=h.document.getElementById('servers-list').children[0].querySelector('select');
    assert.deepEqual(select.children.map(option=>[option.value,option.textContent]),[
        ['a','Provider · Clash · 1'],
        ['b','Provider · JSON / URI'],
        ['c','Provider · Clash · 2'],
        ['d','Provider']
    ]);
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

test('pending probes scan the list once and retain dedicated active and selected variant buttons',()=>{
    const h=serverFixture();
    const rows=[
        row('a'),row('b',true),row('c',false,'manual'),
        {...row('d'),displayGroupId:'d'},{...row('e'),displayGroupId:'e'}
    ];
    h.f.renderSummary({servers:rows,activeServer:rows[1],total:rows.length});
    const list=h.document.getElementById('servers-list');
    const select=list.children[0].querySelector('select');
    select.value='a';
    select.dispatch('change');
    let scans=0;
    let scanningDocument=false;
    const all=h.document.querySelectorAll.bind(h.document);
    h.document.querySelectorAll=function(selector){
        if(selector==='[data-check-server]')scans++;
        scanningDocument=true;
        try{return all(selector);}
        finally{scanningDocument=false;}
    };
    const inList=list.querySelectorAll.bind(list);
    list.querySelectorAll=function(selector){
        if(selector==='[data-check-server]'&&!scanningDocument)scans++;
        return inList(selector);
    };
    h.ui.hasQueuedRequest=(url,id)=>url==='/api/servers/check.cgi'&&['a','b','c','d','e'].includes(id);
    h.ui.followQueued=async(url,id,listener)=>{h.calls.push({url,id});listener({state:'queued'});};
    h.f.renderServers(rows);
    assert.deepEqual(h.calls.map(c=>c.id),['a','b','d','e']);
    assert.equal(h.document.getElementById('check-active-server').disabled,true);
    for(const id of ['a','d','e']){
        const button=inList('[data-check-server]').find(item=>item.dataset.checkServer===id);
        assert.ok(button?.disabled,id);
    }
    assert.equal(scans,1,'redraw should build one button index');
});

test('four server sort options keep stable ties, original rows and the active identity',()=>{
    const h=fixture('servers.js','bindEvents,renderSummary');
    const server=(id,name,status,freshness,ping,active=false)=>({...row(id,active,'manual'),displayGroupId:id,name,quality:{status,freshness,ping}});
    const rows=[
        server('old','Бета 10','available','expired',1),
        server('fast','АЛЬФА 2','available','fresh',12),
        server('tie','Альфа 10','available','fresh',12),
        server('unknown','Дельта','unknown','unknown',null),
        server('down','Бета 2','unavailable','stale',2),
        server('unsupported','Гамма','unsupported','not-applicable',null),
        server('active','Омега','available','fresh',55,true)
    ];
    const initial=JSON.stringify(rows);
    const summary={servers:rows,activeServer:rows[6],total:rows.length};
    h.f.bindEvents();
    h.f.renderSummary(summary);
    const select=h.document.getElementById('servers-sort-order');
    const ordered=()=>h.document.getElementById('servers-list').children.map(card=>card.getAttribute('data-server-id'));
    assert.deepEqual(ordered(),rows.map(item=>item.id));
    for(const [mode,order] of [
        ['name',['fast','tie','down','old','unsupported','unknown','active']],
        ['availability',['old','fast','tie','active','unknown','down','unsupported']],
        ['latency',['fast','tie','active','old','unknown','down','unsupported']],
        ['current',rows.map(item=>item.id)]
    ]){
        select.value=mode;
        select.dispatch('change');
        assert.deepEqual(ordered(),order,mode);
        h.f.renderSummary(summary);
        assert.deepEqual(ordered(),order,mode+' after summary refresh');
        assert.equal(h.document.getElementById('check-active-server').getAttribute('data-server-id'),'active');
    }
    assert.equal(JSON.stringify(rows),initial,'sorting must not affect server data, existing group or reserve priority');
    const page=fs.readFileSync(path.join(__dirname,'../../webui/static/servers.html'),'utf8');
    const control=page.match(/<select id="servers-sort-order"[^>]*>([\s\S]*?)<\/select>/)?.[1]||'';
    assert.deepEqual([...control.matchAll(/<option value="([^"]+)"/g)].map(match=>match[1]),['current','name','availability','latency']);
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
    const h=subscriptionFixture();await h.f.loadAll(true);
    const list=h.document.getElementById('subscriptions-list');
    assert.match(list.innerHTML,/data-action="refresh"[^>]*><span class="button-label">Обновить<\/span>/);
    assert.match(list.innerHTML,/data-action="servers"[^>]*><span class="button-label">Серверы<\/span>/);
    await h.f.toggleServers('A');assert.equal(h.gets,1);
    assert.match(list.innerHTML,/data-action="servers"[^>]*><span class="button-label">Скрыть серверы<\/span>/);
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
    h.catalog=[metadata(1,'3')];await h.f.loadAll(true);await tick();
    h.ui.followQueued=async(url,id,cb)=>{assert.ok(url.endsWith('/delete.cgi'));h.catalog=[];cb({state:'completed'});};
    await h.f.removeSubscription(h.catalog[0]);finish();await tick();
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
test('page asset cache tags match the current build and revision',()=>{
 const app=fs.readFileSync(path.join(__dirname,'../../internal/app/app.go'),'utf8');const version=app.match(/var Version = "([^"]+)"/)[1];
 for(const name of ['home','servers','subscriptions']){
  const html=fs.readFileSync(path.join(__dirname,'../../webui/static/'+name+'.html'),'utf8');
  const tag=html.match(/allpage\.css\?v=([^" ]+)/)[1];
  assert.ok(tag.startsWith('WebUI-'+version+'-c'));assert.match(tag,/-c[1-9][0-9]*$/);
  assert.ok(html.includes('/assets/js/'+name+'.js?v='+tag));
 }
});

 test('update warnings are a collapsed disclosure with escaped messages',()=>{
 const h=fixture('subscriptions.js','renderWarnings');
 const html=h.f.renderWarnings({lastUpdateResult:{warnings:['<private>','second']}});
 assert.match(html, /^<details\b/); assert.match(html, /<summary>Предупреждения обновления<\/summary>/);
 assert.doesNotMatch(html, /<details[^>]*\bopen(?:[\s=>])/);assert.match(html,/&lt;private&gt;/);
 assert.equal(h.f.renderWarnings({lastUpdateResult:{warnings:[]}}),'');
 });
test('provider details have a bordered disclosure before update warnings',()=>{
 const h=fixture('subscriptions.js','state,renderSubscriptions');
 const provider=fs.readFileSync(path.join(__dirname,'../../webui/static/assets/js/subscription-provider.js'),'utf8');
 vm.runInNewContext(provider,h.context);
 h.f.state.subscriptions=[{...metadata(),providerMetadata:{schemaVersion:1,title:'Test provider'},
    lastUpdateResult:{warnings:['Check this']}}];
 h.f.renderSubscriptions();
 const html=h.document.getElementById('subscriptions-list').innerHTML;
 assert.match(html,/<details class="subscription-provider subscription-provider-card"><summary>Сведения провайдера<\/summary>/);
 assert.ok(html.indexOf('Сведения провайдера')<html.indexOf('Предупреждения обновления'));
 const css=fs.readFileSync(path.join(__dirname,'../../webui/static/assets/css/allpage.css'),'utf8');
 assert.match(css,/\.subscription-provider-card\s*\{[^}]*border:\s*1px solid/);
});
test('saved server warning stays in Details after import and reload',async()=>{
 const h=serverFixture();h.f.state.summary.servers.forEach(r=>r.importWarnings=['Synthetic warning <value>']);h.f.renderServers(h.f.state.summary.servers);
 const card=h.document.getElementById('servers-list').children[0];assert.doesNotMatch(card.textContent,/Synthetic warning/);
 h.ui.apiRequest=async()=>({id:'b',importWarnings:['Synthetic warning <value>']});
 card.querySelector('.server-details-toggle').dispatch('click');await tick();
 const warnings=card.querySelector('.server-import-warnings');assert.match(warnings.textContent,/Synthetic warning <value>/);assert.equal(warnings.hasAttribute('open'),false);
});
test('DNS unsaved selection does not reuse saved matchesSelection',()=>{
 const h=fixture('dns.js','selectionMatches,setFixture:function(s,p){status=s;selected=p;}');
 const a={id:'a',address:'192.0.2.1',port:853,sni:'a.example',present:true};const b={id:'b',address:'192.0.2.2',port:853,sni:'b.example',present:true};
 h.f.setFixture({servers:[a,b],selectedIds:['a','b'],managed:[a,b],matchesSelection:true},{a:true,b:false});assert.equal(h.f.selectionMatches(),false);
});
test('subscription settings roundtrip independent export switches and optional UTC apply time',async()=>{
 const h=fixture('subscriptions.js','state,resetForm,openEdit,saveForm');let saved;
 h.f.resetForm();assert.equal(h.document.getElementById('subscription-fetch-json').checked,false);assert.equal(h.document.getElementById('subscription-fetch-clash').checked,false);
 const time=h.document.getElementById('subscription-profile-apply-time');
 assert.equal(time.value,'');
 h.context.fetcher=async(url,opt)=>{if(url.includes('/details.'))return response({id:'test',name:'Synthetic',url:'https://example.test/sub',fetchJSON:true,fetchClash:false,autoUpdateEnabled:false,applyProfileAtUTC:'07:21'});if(url.includes('/update.')){saved=JSON.parse(opt.body);return response({});}if(url.includes('/list.'))return response([]);return response({});};
 await h.f.openEdit('test');assert.equal(h.document.getElementById('subscription-fetch-json').checked,true);assert.equal(h.document.getElementById('subscription-fetch-clash').checked,false);
 assert.equal(time.value,'07:21');
 time.value='03:12';
 h.document.getElementById('subscription-fetch-json').checked=false;h.document.getElementById('subscription-fetch-clash').checked=true;await h.f.saveForm({preventDefault(){}});
 assert.equal(saved.fetchJSON,false);assert.equal(saved.fetchClash,true);
 assert.equal(saved.applyProfileAtUTC,'03:12');
 assert.equal(saved.autoUpdateEnabled,false);
 assert.equal(time.value,'');
 const html=fs.readFileSync(path.join(__dirname,'../../webui/static/subscriptions.html'),'utf8');for(const id of ['subscription-fetch-json','subscription-fetch-clash']){assert.match(html,new RegExp('id="'+id+'"[^>]*type="checkbox"'));assert.doesNotMatch(html,new RegExp('id="'+id+'"[^>]*checked'));}
 assert.match(html,/id="subscription-profile-apply-time"[^>]*type="time"/);
});

 test('home summary keeps configuration warning text in subscription Details only',()=>{
 const h=fixture('home.js','renderSubscriptions');
 h.f.renderSubscriptions({severity:'ok',lastUpdateStatus:'ok',lastWarnings:['configuration-only-secretless-marker'],enabled:1,total:1,serversReceived:16});
 assert.doesNotMatch(h.document.getElementById('home-subscriptions-main').textContent,/configuration-only-secretless-marker/);
 assert.match(h.document.getElementById('home-subscriptions-main').textContent,/Последнее обновление/);
 h.f.renderSubscriptions({severity:'warning',lastUpdateStatus:'partial',lastWarnings:['configuration-only-secretless-marker'],enabled:1,total:1,serversReceived:16});
 assert.equal(h.document.getElementById('home-subscriptions-status').textContent,'Обновлено частично');
 });


function sourceRemovalFixture() {
 const h=fixture('subscriptions.js','state,removeSubscription,renderSubscriptions,loadAll');
 h.item={id:'source-a',name:'Synthetic',serversCount:2,enabled:true,lastUpdateStatus:'ok'};
 h.f.state.subscriptions=[h.item];h.f.state.editingId=h.item.id;
 h.f.state.serverCache.set(h.item.id,[{id:'preserved'}]);
 h.confirmations=0;h.window.BROrayDialogs.confirm=async()=>{h.confirmations++;return true;};
 h.context.fetcher=async(url)=>{h.calls.push({legacy:url});return response(url.includes('/list.')?[h.item]:{});};
 h.ui.followQueued=async(url,id,listener)=>{h.calls.push({url,id});h.listener=listener;listener({state:'queued',requestId:'remove-one'});return {accepted:true,state:'queued'};};
 return h;
}
test('source deletion waits for terminal completion and never offers refresh cancellation',async()=>{
 const h=sourceRemovalFixture();await h.f.removeSubscription(h.item);
 assert.equal(typeof h.listener,'function','deletion must enter queued owner');
 assert.equal(h.f.state.editingId,h.item.id);assert.ok(h.f.state.serverCache.has(h.item.id));
 assert.equal(h.toasts.length,0,'admission is not completion');
 let html=h.document.getElementById('subscriptions-list').innerHTML;
 assert.match(html,/Удалени/);assert.doesNotMatch(html,/data-action="cancel-update"/);
 await h.f.removeSubscription(h.item);assert.equal(h.confirmations,1,'duplicate submission blocked');
 h.listener({state:'running'});html=h.document.getElementById('subscriptions-list').innerHTML;assert.doesNotMatch(html,/data-action="cancel-update"/);
 h.context.fetcher=async(url)=>response(url.includes('/list.')?[]:{});
 h.listener({state:'completed'});await tick();assert.equal(h.f.state.editingId,null);assert.equal(h.f.state.serverCache.has(h.item.id),false);
 assert.equal(h.toasts.filter(x=>/Подписка.*удален/.test(x[0])).length,1);
 assert.equal(h.calls.filter(x=>x.legacy&&x.legacy.includes('/delete.')).length,0);
});
for(const terminal of ['failed','cancelled','unknown'])test('source deletion '+terminal+' preserves form and avoids false success',async()=>{
 const h=sourceRemovalFixture();await h.f.removeSubscription(h.item);assert.equal(typeof h.listener,'function');
 h.listener({state:terminal,message:'Removal outcome '+terminal});await tick();assert.equal(h.f.state.editingId,h.item.id);
 assert.equal(h.toasts.filter(x=>/Подписка.*удален/.test(x[0])).length,0);
 assert.ok(h.toasts.some(x=>x[0]==='Removal outcome '+terminal));
 if(terminal==='unknown'){
  assert.match(h.document.getElementById('subscriptions-list').innerHTML,/Проверить удаление/);
  await h.f.removeSubscription(h.item);assert.equal(h.confirmations,1,'unknown recheck must reuse prior intent');
 }
});
test('source deletion resumes saved queue request without another confirmation',async()=>{
 const h=sourceRemovalFixture();h.ui.hasQueuedRequest=(url,id)=>url.endsWith('/delete.cgi')&&id===h.item.id;
 await h.f.loadAll(true);assert.equal(typeof h.listener,'function');assert.equal(h.confirmations,0);assert.equal(h.f.state.editingId,h.item.id);
 assert.doesNotMatch(h.document.getElementById('subscriptions-list').innerHTML,/data-action="cancel-update"/);
});
test('source deletion refused confirmation sends no request',async()=>{
 const h=sourceRemovalFixture();h.window.BROrayDialogs.confirm=async()=>false;
 await h.f.removeSubscription(h.item);assert.equal(h.calls.length,0);assert.equal(h.f.state.editingId,h.item.id);
});
test('source deletion admission error preserves form and surfaces error',async()=>{
 const h=sourceRemovalFixture();h.ui.followQueued=async()=>{throw new Error('admission refused');};
 await h.f.removeSubscription(h.item);assert.equal(h.f.state.editingId,h.item.id);assert.ok(h.toasts.some(x=>x[0]==='admission refused'));
});

test('a current staged subscription profile appears separately from the catalog result',()=>{
    const h=fixture('subscriptions.js','state,renderSubscriptions');
    const item={...metadata(),id:'source-a',lastUpdateStatus:'success',lastUpdateResult:{updated:1},lastError:''};
    const revision='a'.repeat(64);
    h.f.state.subscriptions=[{...item,pendingActiveProfile:{serverId:'profile-a',revision}}];
    h.f.renderSubscriptions();
    const list=h.document.getElementById('subscriptions-list');
    assert.match(list.innerHTML,/Параметры VPN ожидают применения/);
    assert.match(list.innerHTML,/обновлено 1/);
    assert.doesNotMatch(list.innerHTML,new RegExp(revision));
    h.f.state.subscriptions=[item];
    h.f.renderSubscriptions();
    assert.doesNotMatch(list.innerHTML,/Параметры VPN ожидают применения/);
});

test('pending subscription apply confirms the exact revision and does not resubmit on resume',async()=>{
    const h=fixture('subscriptions.js','state,renderSubscriptions,applyPendingProfile');
    const rev='1'.repeat(64);
    const item={...metadata(),id:'source-a',pendingActiveProfile:{serverId:'profile-a',revision:rev}};
    h.f.state.subscriptions=[item];
    h.f.renderSubscriptions();
    assert.match(h.document.getElementById('subscriptions-list').innerHTML,/data-action="apply-profile"/);
    let confirmed=0;
    h.window.BROrayDialogs.confirm=async()=>{confirmed++;return confirmed===2;};
    const requests=[];
    h.ui.followQueued=async(url,id,listener,selection)=>{
        requests.push({url,id,selection});
        listener({state:'queued'});
    };
    await h.f.applyPendingProfile(item,false);
    assert.equal(requests.length,0,'cancelled confirmation submits nothing');
    await h.f.applyPendingProfile(item,false);
    assert.equal(requests[0].url,'/api/subscriptions/apply-profile.cgi');
    assert.equal(requests[0].id,item.id);
    assert.equal(requests[0].selection.revision,rev);
    assert.equal(confirmed,2);
    delete h.f.state.applying[item.id];
    await h.f.applyPendingProfile(item,true);
    assert.equal(requests[1].selection,undefined);
    assert.equal(confirmed,2);
});

