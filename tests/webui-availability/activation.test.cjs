const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const source=n=>fs.readFileSync(path.join(__dirname,'../../webui/static/assets/js/'+n),'utf8');
const tick=()=>new Promise(r=>setImmediate(r));
function ui(failure){
 const calls=[],notices=[],attrs=new Map();let listener,loads=0;
 const button={textContent:'Подключить',disabled:false,hasAttribute:k=>attrs.has(k),setAttribute:(k,v)=>attrs.set(k,v),getAttribute:k=>attrs.get(k),removeAttribute:k=>attrs.delete(k)};
 const document={getElementById(){return null;},querySelectorAll(){return [];}};
 const window={BROrayDialogs:{confirm:async()=>true}};
 const BROrayUI={apiRequest:async(url)=>{calls.push({legacy:url});return {success:true,data:{}};},toast:(text,type)=>notices.push({text,type}),queueLabel:r=>r.state,
 followQueued:async(url,id,fn)=>{calls.push({url,id});listener=fn;if(failure)throw Error('unconfirmed');fn({state:'running'});return {state:'running'};}};
 vm.runInNewContext(source('servers.js').replace('    bindEvents();\n    initialize();','    loadSummary=async function(){ fixtureLoaded(); }; window.fixture={activateServer,state};'),{window,document,BROrayUI,fixtureLoaded(){loads++;}});
 return {...window.fixture,button,calls,notices,get loads(){return loads;},complete(result){listener(result);}};
}
test('activation follows a queued receipt and reloads state only after completion',async()=>{
 const v=ui();v.activateServer({id:'manual-a',name:'A'},v.button);await tick();
 assert.deepEqual(v.calls,[{url:'/api/servers/activate.cgi',id:'manual-a'}]);assert.equal(v.button.disabled,true);assert.equal(v.loads,0);assert.equal(v.notices.length,0);
 v.complete({state:'completed'});await tick();assert.equal(v.loads,1);assert.equal(v.button.disabled,false);assert.equal(v.notices[0].type,'success');
});
test('uncertain activation refreshes real state without retrying the mutation',async()=>{
 const v=ui(true);v.activateServer({id:'manual-a',name:'A'},v.button);await tick();
 assert.deepEqual(v.calls,[{url:'/api/servers/activate.cgi',id:'manual-a'}]);assert.equal(v.loads,1);assert.equal(v.button.disabled,false);assert.equal(v.notices[0].type,'error');
});
test('common queue admits activation with a durable nonce',async()=>{
 const requests=[],saved=new Map(),window={location:{origin:'http://example.test'}};
 const fetch=async(url,options)=>{requests.push({url,body:JSON.parse(options.body),headers:options.headers});return {ok:true,status:202,json:async()=>({ok:true,requestId:'q-'+requests[0].body.nonce,state:'queued'})};};
 vm.runInNewContext(source('common.js'),{window,document:{},fetch,AbortController,setTimeout,clearTimeout,crypto:require('node:crypto').webcrypto,Uint8Array,sessionStorage:{getItem:k=>saved.get(k)||null,setItem:(k,v)=>saved.set(k,v),removeItem:k=>saved.delete(k)}});
 const receipt=await window.BROrayUI.submitQueued('/api/servers/activate.cgi','manual-a');
 assert.equal(receipt.state,'queued');assert.equal(requests.length,1);assert.equal(requests[0].headers['X-BROray-Queue'],'1');assert.equal(saved.size,1);
});
