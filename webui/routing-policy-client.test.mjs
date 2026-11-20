import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert/strict';
import test from 'node:test';

const code=fs.readFileSync(new URL('./static/assets/js/routing-policy-client.js',import.meta.url),'utf8');
const clone=x=>JSON.parse(JSON.stringify(x));
const rev=n=>String(n).padStart(64,'0');

test('routes overview cards link to native domains and blocking',()=>{
    const page=fs.readFileSync(new URL('./static/routes.html',import.meta.url),'utf8');
    const cards=[...page.matchAll(/<article class="routes-hub-card ui-card"[^>]*>([\s\S]*?)<\/article>/g)]
        .map(([,card])=>card);
    assert.ok(cards.some(card=>card.includes('<h2>По доменам</h2>')&&card.includes('href="/routes-domains.html"')));
    assert.ok(cards.some(card=>card.includes('<h2>Блокировки</h2>')&&card.includes('href="/routes-blocking.html"')));
});

function fixture(){
    let serial=1,scheduleSerial=1,queued=null,mode='',readFails=false,release;
    let policy={state:{schemaVersion:1,rules:[{id:'own',name:'Own',enabled:false,action:'vpn',domains:['domain:example.com']}]},revision:rev(serial),busy:false,pending:false};
    let schedule={enabled:false,revision:rev(scheduleSerial),attempts:{},intervalHours:24,retryMinutes:15};
    const requests=[],store=new Map(),events=[];
    const api=async(url,options={})=>{
        requests.push({url,...clone(options)});
        if(url==='/api/operations/status.cgi'){
            assert.equal(options.body.nonce,'a'.repeat(32));
            if(mode==='unknown')throw Object.assign(new Error('unconfirmed'),{status:404});
            if(!queued)throw new Error('missing request');
            return {ok:true,...queued};
        }
        if(options.method!=='POST'){
            if(readFails)throw new Error('offline');
            return {success:true,data:clone(url.includes('schedule')?schedule:policy)};
        }
        if(mode==='hold')await new Promise(r=>{release=r;});
        if(url.includes('schedule')){
            assert.equal(options.body.expectedRevision,schedule.revision);
            assert.deepEqual(Object.keys(options.body).sort(),['enabled','expectedRevision']);
            schedule.enabled=options.body.enabled;schedule.revision=rev(++scheduleSerial);
            return {success:true,data:clone(schedule)};
        }
        assert.equal(options.body.expectedRevision,policy.revision);
        if(mode==='reject')throw Object.assign(new Error('stale'),{status:409});
        if(options.body.operation.startsWith('geo-')){
            assert.equal(options.headers['X-BROray-Nonce'],'a'.repeat(32));
            queued={requestId:'q-'+'a'.repeat(32),state:mode==='failed'?'failed':'completed',message:'fixture failure',errorCode:'TEST_FAILED'};
            if(mode==='lost'||mode==='unknown')throw new Error('response lost');
            return {success:true,data:{ok:true,...queued}};
        }
        const rule=policy.state.rules[0];
        if(options.body.operation==='enabled')rule.enabled=options.body.enabled;
        if(options.body.operation==='action')rule.action=options.body.action;
        policy.revision=rev(++serial);
        if(mode==='lost')throw new Error('response lost');
        return {success:true,data:clone(policy)};
    };
    const root={};vm.runInNewContext(code,{window:root,AbortController,setTimeout,clearTimeout,console});
    const client=root.BROrayPolicy.createClient({api,storage:{getItem:k=>store.get(k)||null,setItem:(k,v)=>store.set(k,v),removeItem:k=>store.delete(k)},nonce:()=> 'a'.repeat(32),wait:async()=>{},onChange:s=>events.push(clone(s))});
    return {client,requests,store,events,mode:v=>{mode=v;},readFails:v=>{readFails=v;},release:()=>release(),policy,stored:()=>clone(policy),schedule:()=>clone(schedule)};
}

test('independent switches use current CAS and preserve other values',async()=>{
    const f=fixture();await f.client.refresh();await f.client.refreshSchedule();
    await f.client.change({operation:'enabled',id:'own',enabled:true});
    assert.equal(f.stored().state.rules[0].enabled,true);assert.equal(f.stored().state.rules[0].action,'vpn');
    await f.client.change({operation:'action',id:'own',action:'direct'});
    assert.equal(f.stored().state.rules[0].enabled,true);assert.equal(f.client.snapshot().policy.revision,rev(3));
    await f.client.setSchedule(true);assert.equal(f.schedule().enabled,true);
    assert.equal(f.stored().state.rules[0].action,'direct');
});
test('pending and busy state refuse policy writes but allow independent daily toggle',async()=>{
    const f=fixture();f.policy.pending=true;await f.client.refresh();await f.client.refreshSchedule();
    await assert.rejects(f.client.change({operation:'enabled',id:'own',enabled:true}));
    await f.client.setSchedule(true);assert.equal(f.schedule().enabled,true);
    assert.equal(f.requests.filter(r=>r.url.endsWith('/policy.cgi')&&r.method==='POST').length,0);
});
test('lost queued response observes saved nonce, never submits twice',async()=>{
    const f=fixture();await f.client.refresh();f.mode('lost');
    await f.client.change({operation:'geo-update',id:'geoip',kind:'geoip'});
    assert.equal(f.requests.filter(r=>r.url.endsWith('/policy.cgi')&&r.method==='POST').length,1);
    assert.equal(f.requests.filter(r=>r.url.endsWith('/status.cgi')).length,1);
    assert.equal(f.store.size,0);assert.equal(f.client.snapshot().uncertain,false);
});
test('queue failure is not success and retains actual saved choices',async()=>{
    const f=fixture();await f.client.refresh();f.mode('failed');
    await assert.rejects(f.client.change({operation:'geo-update',id:'geoip',kind:'geoip'}));
    assert.equal(f.client.snapshot().policy.state.rules[0].enabled,false);
    assert.equal(f.store.size,0);
});
test('unknown queued result blocks until explicit successful readback',async()=>{
    const f=fixture();await f.client.refresh();f.mode('unknown');
    await assert.rejects(f.client.change({operation:'geo-update',id:'geoip',kind:'geoip'}));
    assert.equal(f.client.snapshot().uncertain,true);assert.equal(f.store.size,1);
    await assert.rejects(f.client.change({operation:'enabled',id:'own',enabled:true}));
    await f.client.refresh();
    assert.equal(f.client.snapshot().uncertain,false);assert.equal(f.store.size,0);
});
test('readback failure keeps visible state and prevents blind second write',async()=>{
    const f=fixture();await f.client.refresh();f.readFails(true);
    await assert.rejects(f.client.change({operation:'enabled',id:'own',enabled:true}));
    assert.equal(f.client.snapshot().policy.state.rules[0].enabled,false);
    assert.equal(f.client.snapshot().uncertain,true);
    await assert.rejects(f.client.change({operation:'enabled',id:'own',enabled:false}));
});
test('concurrent writes are rejected before transport',async()=>{
    const f=fixture();await f.client.refresh();f.mode('hold');
    const first=f.client.change({operation:'enabled',id:'own',enabled:true});
    await assert.rejects(f.client.change({operation:'action',id:'own',action:'direct'}));
    f.release();await first;
    assert.equal(f.requests.filter(r=>r.method==='POST').length,1);
});
