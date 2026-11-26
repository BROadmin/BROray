import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert/strict';
import test from 'node:test';

const code = fs.readFileSync(new URL('./static/assets/js/servers-auto-switch.js', import.meta.url), 'utf8');
const flush = async () => { for (let i=0;i<10;i++) await new Promise(setImmediate); };
async function fixture({paused=false, lastError=''} = {}) {
    const els = new Map(), requests = [], messages = [], events = {};
    let stored = { enabled: paused, failureThreshold: 3, cooldownMinutes: 10, minimumRating: 'acceptable', selectionRule: 'best-quality', preferredServerId: null };
    let revision = 'r1', serial = 1, mode = '', release, readFails = false;
    const controls = ['enabled','rule','preferred','threshold','cooldown','minimum','save'];
    function el(id) {
        if (!els.has(id)) els.set(id, {id, attributes:{}, events:{}, checked:false, value:'', disabled:false,
            setAttribute(k,v){this.attributes[k]=String(v);}, getAttribute(k){return this.attributes[k] ?? null;}, removeAttribute(k){delete this.attributes[k];},
            addEventListener(k,fn){(this.events[k]??=[]).push(fn);}, querySelectorAll(){return controls.map(n=>el('auto-switch-'+n));}, appendChild(){}, click(){} });
        return els.get(id);
    }
    const document = {readyState:'complete', hidden:false, getElementById:id=>id==='auto-switch-badge'?null:el(id), addEventListener(k,fn){events[k]=fn;}, querySelector(){return null;},createElement(){return el('option');}};
    const api = async (url, options={}) => {
        requests.push({url,...options});
        if (url.endsWith('/summary.cgi')) return {servers:[]};
        if (options.method==='POST') {
            if(mode==='pending') await new Promise(resolve=>{release=resolve;});
            if(mode==='reject') throw Object.assign(new Error('Denied'),{status:409,payload:{success:false,error:{code:'OPERATION_BUSY'}}});
            assert.equal(options.body.expectedRevision,revision, 'write must use confirmed revision');
            stored={...stored,...Object.fromEntries(Object.entries(options.body).filter(([k])=>k!=='expectedRevision'))}; revision='r'+(++serial);
            if(mode==='lost') throw new Error('connection lost');
            return {value:stored.enabled,revision};
        }
        if(readFails) throw new Error('offline');
        return {config:{...stored},revision,state:{lastError},automation:{paused},service:{running:false}};
    };
    vm.runInNewContext(code,{document,window:{setTimeout(){return 1;},clearTimeout(){}},BROrayUI:{apiRequest:api,toast(m){messages.push(m);},redirectToLogin(){}}});
    await flush();
    const dispatch=(target,type)=>{const event={target,preventDefault(){}}; for(const fn of target.events[type]||[]) fn(event); if(target!==el('auto-switch-form')) for(const fn of el('auto-switch-form').events[type]||[]) fn(event);};
    return {el,requests,messages,events,dispatch,stored:()=>stored,external(){stored.cooldownMinutes=40;revision='r'+(++serial);},mode(v){mode=v;},readFails(v){readFails=v;},release(){release();},toggle(v){el('auto-switch-enabled').checked=v;dispatch(el('auto-switch-enabled'),'change');}};
}

test('pause and error are visible with a plain settings heading',async()=>{
    const f=await fixture({paused:true,lastError:'Проверка завершилась ошибкой'});
    assert.equal(f.el('auto-switch-enabled').checked,true);
    assert.equal(f.el('auto-switch-status').textContent,'На паузе');
    assert.equal(f.el('auto-switch-error').hidden,false);
    assert.equal(f.el('auto-switch-error').textContent,'Проверка завершилась ошибкой');
});

test('standalone switch saves both ways, preserves dirty neighbours, survives reload/status',async()=>{
    const f=await fixture();
    f.el('auto-switch-threshold').value='7'; f.dispatch(f.el('auto-switch-threshold'),'input');
    for(const value of [true,false]) {
        f.toggle(value); await flush();
        assert.equal(f.stored().enabled,value);
        assert.equal(f.stored().failureThreshold,3);
        assert.equal(f.el('auto-switch-threshold').value,'7');
        const post=f.requests.filter(r=>r.method==='POST').at(-1);
        assert.ok(post.url.endsWith('/auto-switch-enabled.cgi'));
        assert.deepEqual(Object.keys(post.body).sort(),['enabled','expectedRevision']);
    }
    f.dispatch(f.el('auto-switch-form'),'submit'); await flush();
    assert.equal(f.stored().failureThreshold,7,'existing Save button persists parameters');
});
test('pending write blocks duplicate switch and whole form submission',async()=>{
    const f=await fixture(); f.mode('pending'); f.toggle(true); await flush();
    assert.equal(f.el('auto-switch-enabled').disabled,true);
    f.toggle(false); f.dispatch(f.el('auto-switch-form'),'submit'); await flush();
    assert.equal(f.requests.filter(r=>r.method==='POST').length,1);
    f.release(); await flush(); assert.equal(f.stored().enabled,true); assert.equal(f.el('auto-switch-enabled').checked,true);
});
test('explicit refusal restores confirmed value',async()=>{
    const f=await fixture(); f.mode('reject'); f.toggle(true); await flush();
    assert.equal(f.stored().enabled,false); assert.equal(f.el('auto-switch-enabled').checked,false);
    assert.equal(f.el('auto-switch-enabled').disabled,false); assert.ok(f.messages.length);
});
test('lost response reads back without a second write',async()=>{
    const f=await fixture(); f.mode('lost'); f.toggle(true); await flush();
    assert.equal(f.el('auto-switch-enabled').checked,true); assert.equal(f.el('auto-switch-enabled').disabled,false);
    assert.equal(f.requests.filter(r=>r.method==='POST').length,1);
});
test('unavailable readback locks writes until fresh confirmed status',async()=>{
    const f=await fixture(); f.mode('lost'); f.readFails(true); f.toggle(true); await flush();
    assert.equal(f.el('auto-switch-enabled').disabled,true); assert.equal(f.el('auto-switch-save').disabled,true);
    f.readFails(false); f.events.visibilitychange(); await flush();
    assert.equal(f.el('auto-switch-enabled').disabled,false); assert.equal(f.el('auto-switch-enabled').checked,true);
});
test('poll cannot authorize overwriting a dirty form from another tab',async()=>{
    const f=await fixture();
    f.el('auto-switch-threshold').value='7';f.dispatch(f.el('auto-switch-threshold'),'input');
    f.external();f.events.visibilitychange();await flush();
    f.dispatch(f.el('auto-switch-form'),'submit');await flush();
    const post=f.requests.filter(r=>r.method==='POST').at(-1);
    assert.equal(post.body.expectedRevision,'r1','dirty form must retain its original revision');
    assert.equal(f.stored().cooldownMinutes,40,'newer server state preserved');
});
