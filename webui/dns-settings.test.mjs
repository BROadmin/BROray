import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert/strict';
import test from 'node:test';

const code = fs.readFileSync(new URL('./static/assets/js/dns.js', import.meta.url), 'utf8');
const flush = async () => { for (let i=0;i<12;i++) await new Promise(setImmediate); };
const clone = value => JSON.parse(JSON.stringify(value));
async function fixture() {
    const nodes=new Map(), requests=[], messages=[], events={}, feedback=[];
    let serial=1, mode='', releaseWrite, releaseRead, holdRead=false, failRead=false;
    let stored={enabled:false,revision:'1'.padStart(64,'0'),settingsValid:true,savedIds:['google-primary'],paused:false,last:{}};
    let saved=['google-primary'];
	let actual=[];
    let capabilities;
    const servers=[
        {id:'google-primary',provider:'Google',name:'Google 8.8.8.8',address:'8.8.8.8',addressFamily:'IPv4',port:853,sni:'dns.google',present:false,test:null},
        {id:'cloudflare-primary-ipv6',provider:'Cloudflare',name:'Cloudflare IPv6',address:'2606:4700:4700::1111',addressFamily:'IPv6',port:853,sni:'one.one.one.one',present:false,
            test:{ok:false,status:'failed',testedEpoch:Date.now()/1000,tcpStatus:'ok',tlsStatus:'ok',dnsStatus:'failed',addressFamily:'IPv6',errorCode:'DOT_DNS_FAILED',latencyMs:21}}
    ];
    function node(tag='div') {
        const n={tagName:tag.toUpperCase(),children:[],events:{},attributes:{},className:'',checked:false,disabled:false,hidden:false,text:'',
            append(...items){this.children.push(...items);},appendChild(item){this.children.push(item);return item;},replaceChildren(...items){this.children=[...items];},
            setAttribute(k,v){this.attributes[k]=String(v);},getAttribute(k){return this.attributes[k]??null;},removeAttribute(k){delete this.attributes[k];},
            addEventListener(k,fn){(this.events[k]??=[]).push(fn);},hasChildNodes(){return this.children.length>0;},
            classList:{add(){},remove(){},toggle(){}}};
        Object.defineProperty(n,'textContent',{get(){return this.text+this.children.map(c=>c.textContent??String(c)).join('');},set(v){this.text=String(v);this.children=[];}});
        return n;
    }
    const el=id=>{if(!nodes.has(id))nodes.set(id,node());return nodes.get(id);};
    const status=()=>({servers:clone(servers),selectedIds:[...saved],maxServers:8,managed:[],quarantinedReceipts:[],
        actual:{dot:clone(actual),determinate:true,dohCount:0,totalSecure:actual.length,runtime:{filterDot:[]}},runningConfigAvailable:true,installationState:'not-installed',
        writeProtocolEnabled:true,mutationAvailable:true,testAvailable:true,deleteEligible:false,capabilities,autoCheck:clone({...stored,savedIds:saved})});
    const api=async(url,options={})=>{
        const body=options.body?JSON.parse(options.body):undefined;
        requests.push({url,...options,body});
        if(url==='/api/session.cgi')return {user:'admin'};
        if(options.method==='POST') {
            if(url.endsWith('dot-auto-settings.cgi')) {
                if(mode==='pending')await new Promise(resolve=>{releaseWrite=resolve;});
                if(mode==='reject')throw Object.assign(new Error('refused'),{status:409,code:'ROUTES_OPERATION_BUSY'});
                if(body.expectedRevision && body.expectedRevision!==stored.revision)throw Object.assign(new Error('stale'),{status:409,code:'DOT_SETTINGS_CHANGED'});
                stored={...stored,enabled:body.enabled,revision:String(++serial).padStart(64,'0')};
                if(mode==='lost')throw new Error('reply lost');
                return clone(stored);
            }
            if(url.endsWith('dot-test.cgi')){saved=[...body.serverIds];return status();}
            throw new Error('unexpected mutation '+url);
        }
        if(failRead)throw new Error('offline');
        const result=status();
        if(holdRead){holdRead=false;await new Promise(resolve=>{releaseRead=resolve;});}
        return result;
    };
    const document={readyState:'complete',hidden:false,getElementById:el,createElement:node,addEventListener(k,fn){events[k]=fn;}};
    const window={location:{origin:'http://router.test'},setTimeout(){return 1;},clearTimeout(){},
        BROrayActionFeedback:{show(id,info){feedback.push({id,...info});},clear(){}},
        BROrayUI:{apiRequest:api,toast(m){messages.push(m);},redirectToLogin(){}}};
    vm.runInNewContext(code,{document,window,URL,AbortController});
    await flush();
    const dispatch=(n,type)=>{for(const fn of n.events[type]??[])fn({target:n,preventDefault(){}});};
    const inputs=()=>el('dns-server-list').children.map(card=>card.children[0]);
    return {el,requests,messages,feedback,events,dispatch,inputs,stored:()=>clone(stored),saved:()=>[...saved],
        toggle(value){el('dns-auto-check').checked=value;dispatch(el('dns-auto-check'),'change');},
        select(index,value){const n=inputs()[index];n.checked=value;dispatch(n,'change');},
        capability(v){capabilities=v;},probeMessage(v){servers[1].test.message=v;},
        actual(v){actual=v;},mode(v){mode=v;},readFails(v){failRead=v;},holdRead(){holdRead=true;},releaseRead(){releaseRead();},releaseWrite(){releaseWrite();}};
}

test('empty selection can be saved with existing button without applying DNS',async()=>{
    const f=await fixture();f.select(0,false);
    assert.equal(f.el('dns-test').disabled,false);
    assert.match(f.el('dns-test').textContent,/Сохранить/);
    f.dispatch(f.el('dns-test'),'click');await flush();
    assert.deepEqual(f.saved(),[]);
    assert.equal(f.requests.filter(r=>r.method==='POST').length,1);
    assert.ok(f.requests.find(r=>r.method==='POST').url.endsWith('dot-test.cgi'));
    assert.equal(f.el('dns-apply').disabled,true);
});

test('auto toggle writes only value and confirmed revision, preserving draft selection',async()=>{
    const f=await fixture();f.select(1,true);
    f.toggle(true);await flush();
    const post=f.requests.find(r=>r.method==='POST');
    assert.deepEqual(Object.keys(post.body).sort(),['enabled','expectedRevision']);
    assert.equal(f.stored().enabled,true);
    assert.equal(f.inputs()[1].checked,true);
    assert.deepEqual(f.saved(),['google-primary']);
    f.toggle(false);await flush();assert.equal(f.stored().enabled,false);
});

test('lost auto-save reply reads back without a second mutation',async()=>{
    const f=await fixture();f.mode('lost');f.toggle(true);await flush();
    assert.equal(f.stored().enabled,true);
    assert.equal(f.el('dns-auto-check').checked,true);
    assert.equal(f.requests.filter(r=>r.method==='POST').length,1);
});

test('auto preference errors do not claim uncertain router DNS mutation',async()=>{
    for(const mode of ['lost','reject']) {
        const f=await fixture();f.mode(mode);f.toggle(true);await flush();
        assert.equal(f.feedback.length,0,'preference error must not show router-action recovery');
        assert.equal(f.messages.length,1,'error remains visible in notification history');
        assert.equal(f.el('dns-auto-check').disabled,false);
    }
});

test('unknown auto-save outcome locks toggle until confirmed readback',async()=>{
    const f=await fixture();f.mode('lost');f.readFails(true);f.toggle(true);await flush();
    assert.equal(f.el('dns-auto-check').disabled,true);
    f.toggle(false);await flush();assert.equal(f.requests.filter(r=>r.method==='POST').length,1);
    f.readFails(false);f.events.visibilitychange();await flush();
    assert.equal(f.el('dns-auto-check').disabled,false);
    assert.equal(f.el('dns-auto-check').checked,true);
});

test('late status response cannot overwrite a completed toggle save',async()=>{
    const f=await fixture();f.holdRead();f.events.visibilitychange();await flush();
    f.toggle(true);await flush();f.releaseRead();await flush();
    assert.equal(f.el('dns-auto-check').checked,true);
});

test('pending toggle prevents duplicate writes and explicit refusal restores state',async()=>{
    const f=await fixture();f.mode('pending');f.toggle(true);await flush();
    f.toggle(false);await flush();assert.equal(f.requests.filter(r=>r.method==='POST').length,1);
    f.releaseWrite();await flush();assert.equal(f.el('dns-auto-check').checked,true);
    f.mode('reject');f.toggle(false);await flush();assert.equal(f.el('dns-auto-check').checked,true);
});

test('IPv6 endpoint and failed DNS are presented independently from successful TLS',async()=>{
    const f=await fixture();const text=f.el('dns-server-list').textContent;
    assert.match(text,/\[2606:4700:4700::1111\]:853/);
    assert.match(text,/TCP: OK/);assert.match(text,/TLS: OK/);assert.match(text,/DNS: ошибка/);
});

test('draft capacity treats expanded IPv6 as the same external server',async()=>{
    const f=await fixture();
    f.actual([{address:'2606:4700:4700:0:0:0:0:1111',effectivePort:853,sni:'one.one.one.one',valid:true},
        ...Array.from({length:7},(_,i)=>({address:'192.0.2.'+(i+1),effectivePort:853,sni:'other.test',valid:true}))]);
    f.events.visibilitychange();await flush();f.select(0,false);f.select(1,true);
    assert.notEqual(f.el('dns-action-state').textContent,'Превышен лимит Keenetic');
});

test('component status distinguishes missing and unknown from readable configuration',async()=>{
    const f=await fixture();
    assert.equal(f.el('dns-support').textContent,'Не определён');
    for(const [state,label] of [['present','Установлен'],['missing','Не установлен'],['unknown','Не определён']]) {
        f.capability({dotComponent:state,firmware:'5.01.C.1',ipv6DoT:'unknown'});
        f.events.visibilitychange();await flush();
        assert.equal(f.el('dns-support').textContent,label);
        if(state==='missing')assert.match(f.el('dns-capability-hint').textContent,/Прокси-сервер DNS-over-TLS/);
        if(state==='present')assert.match(f.el('dns-capability-hint').textContent,/IPv6.*не подтверждена/);
    }
    f.capability({dotComponent:'present',firmware:'4.01.C.1',ipv6DoT:'confirmed'});
    f.events.visibilitychange();await flush();
    assert.match(f.el('dns-capability-hint').textContent,/IPv6.*подтверждена.*конфигураци/);
});

test('failed probe explains IPv6 path separately from DNS or certificate failure',async()=>{
    const f=await fixture();
    f.probeMessage('Рабочий IPv6-путь к выбранному серверу недоступен.');
    f.events.visibilitychange();await flush();
    assert.match(f.el('dns-server-list').textContent,/IPv6-путь.*недоступен/);
});
