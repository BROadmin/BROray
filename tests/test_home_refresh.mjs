/** Execute the complete production home.js; only DOM, clock and HTTP are fixtures.
 * node --test tests/test_home_refresh.mjs
 * BRORAY_HOME_JS=/absolute/baseline/home.js selects the unmodified negative control.
 */
import assert from 'node:assert/strict';
import test from 'node:test';
import vm from 'node:vm';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const source = fs.readFileSync(process.env.BRORAY_HOME_JS || path.join(root, 'runtime/app/web-new/assets/js/home.js'), 'utf8');
const clone = x => JSON.parse(JSON.stringify(x));
const health = (severity = 'ok') => ({severity, reasons: []});
function summary(name = 'Alpha', total = 7) {
    return {health: health(), errors: [], updatedAt: '2026-09-17T08:00:00Z',
        xray: {health: health(), version: '26.9.9', configValid: true, socksActive: true},
        servers: {health: health(), connectionState: 'connected', total, activeServer: {name, quality: {ping: 20, freshness: 'fresh'}}},
        subscriptions: {health: health(), total: 1, enabled: 1, serversReceived: total, lastUpdateStatus: 'success'},
        dns: {health: health(), selectedCount: 3, maxServers: 8, effectiveCount: 3, managedPresentCount: 3},
        routes: {health: health(), installedBundles: 2, availableBundles: 10, updatesAvailableCount: 0},
        keenetic: {health: health(), interfaceDisplayName: 'BROray test', link: true, connected: true, state: 'up'},
        broray: {installationHealthy: true, version: '3.1.1', updateAvailable: false}};
}
class Target {
    listeners = new Map();
    addEventListener(type, fn) { const a = this.listeners.get(type) || []; a.push(fn); this.listeners.set(type, a); }
    dispatch(type, props = {}) { for (const fn of this.listeners.get(type) || []) fn({type, ...props}); }
}
class Element extends Target {
    constructor(id = '') { super(); this.id = id; this.textContent = ''; this.hidden = false; this.disabled = false; this.className = ''; this.attrs = {}; this.title = ''; }
    classList = {toggle: (name, force) => {
        const a = new Set(this.className.split(' ').filter(Boolean));
        if (force === undefined ? !a.has(name) : force) a.add(name); else a.delete(name);
        this.className = [...a].join(' ');
    }};
    setAttribute(k, v) { this.attrs[k] = String(v); }
    getAttribute(k) { return this.attrs[k] ?? null; }
    removeAttribute(k) { delete this.attrs[k]; }
}
const flush = async () => { for (let i = 0; i < 35; i++) await Promise.resolve(); };
class Clock {
    now = 0; seq = 0; timers = new Map();
    setTimeout = (fn, delay = 0) => { const id = ++this.seq; this.timers.set(id, {at: this.now + delay, fn}); return id; };
    clearTimeout = id => { this.timers.delete(id); };
    async advance(ms) {
        const end = this.now + ms; let guard = 0;
        while (true) {
            const q = [...this.timers].filter(([, t]) => t.at <= end).sort((a, b) => a[1].at - b[1].at)[0];
            if (!q) break;
            assert.ok(++guard < 1000, 'timer loop');
            this.now = q[1].at; this.timers.delete(q[0]); q[1].fn(); await flush();
        }
        this.now = end; await flush();
    }
}
function harness(options = {}) {
    const clock = new Clock(), document = new Target(), window = new Target(), nodes = new Map();
    const ids = ['app','page-loader','current-user','refresh-status','logout-button','home-health','home-warning','home-updated-at','home-server-name','home-server-quality'];
    for (const p of ['servers','subscriptions','dns','routes','keenetic','xray','broray']) for (const s of ['status','main','total','version','config','enabled','servers','selected','installed','count','update','link','state']) ids.push(`home-${p}-${s}`);
    for (const id of ids) nodes.set(id, new Element(id));
    document.hidden = !!options.hidden;
    document.getElementById = id => nodes.get(id) || null;
    document.createElement = () => new Element();
    const parent = {insertBefore: e => {nodes.set(e.id, e); e.parentNode = parent;}};
    nodes.get('home-warning').parentNode = parent;
    nodes.get('app').hidden = true;
    nodes.get('refresh-status').hidden = true;
    nodes.get('refresh-status').setAttribute('aria-hidden', 'true');
    nodes.get('refresh-status').setAttribute('tabindex', '-1');
    if (options.noButton) nodes.delete('refresh-status');
    const redirects = [], toasts = [], requests = [], plans = [];
    let current = summary();
    window.location = {replace: v => redirects.push(v)};
    window.BROrayUI = {toast: (...args) => toasts.push(args)};
    window.AbortController = options.noAbort ? undefined : AbortController;
    window.setTimeout = clock.setTimeout; window.clearTimeout = clock.clearTimeout;
    const fetch = (url, opts) => {
        const plan = plans.length ? plans.shift() : {};
        const entry = {url, opts, at: clock.now, aborted: false}; requests.push(entry);
        const payload = plan.data === undefined ? (url.includes('/session.cgi') ? {ok: true, authenticated: true, user: 'fixture-user'} : {success: true, data: clone(current)}) : plan.data;
        return new Promise((resolve, reject) => {
            const abort = () => {entry.aborted = true; if (!plan.ignoreAbort) reject(Object.assign(new Error('aborted'), {name: 'AbortError'}));};
            opts.signal?.addEventListener('abort', abort, {once: true});
            entry.finish = () => plan.error ? reject(new Error(plan.error)) : resolve({status: plan.status || 200, ok: (plan.status || 200) < 400,
                json: () => {
                    entry.jsonCalled = true;
                    return new Promise((res, rej) => {
                        const done = () => plan.badJson ? rej(new Error('JSON broken')) : res(clone(payload));
                        entry.finishBody = done;
                        if (!plan.holdBody) done();
                    });
                }});
            if (!plan.hold) entry.finish();
        });
    };
    class FixtureDate extends Date {constructor(...args) {super(...(args.length ? args : [1789632000000 + clock.now]));} static now() {return 1789632000000 + clock.now;}}
    const ctx = vm.createContext({window, document, fetch, AbortController, Date: FixtureDate, console});
    vm.runInContext(source, ctx, {filename: 'home.js'});
    return {clock, window, document, nodes, plans, redirects, toasts, requests, ctx,
        text: id => nodes.get(id)?.textContent, button: () => nodes.get('refresh-status'),
        setSummary: data => {current = data;},
        summaryRequests: () => requests.filter(x => x.url === '/api/home/summary.cgi'),
        click: async id => {nodes.get(id).dispatch('click'); await flush();},
        visible: async visible => {document.hidden = !visible; document.dispatch('visibilitychange'); await flush();}};
}
async function ready(options) {const h = harness(options); await flush(); return h;}

test('regression: manual refresh button is visible and keyboard accessible', async () => {
    const h = await ready(); assert.equal(h.button().hidden, false); assert.equal(h.button().getAttribute('aria-hidden'), null); assert.equal(h.button().getAttribute('tabindex'), null);
    h.setSummary(summary('Beta', 8)); await h.click('refresh-status'); assert.equal(h.text('home-server-name'), 'Beta'); assert.equal(h.text('home-servers-total'), '8');
});
test('regression: visible page refreshes summary after 30 seconds', async () => {
    const h = await ready(); h.setSummary(summary('Beta', 8)); await h.clock.advance(29999); assert.equal(h.summaryRequests().length, 1);
    await h.clock.advance(1); assert.equal(h.summaryRequests().length, 2); assert.equal(h.text('home-server-name'), 'Beta');
});
test('regression: returning to a visible tab obtains changed summary', async () => {
    const h = await ready(); await h.visible(false); h.setSummary(summary('Beta')); await h.clock.advance(120000); assert.equal(h.summaryRequests().length, 1);
    await h.visible(true); assert.equal(h.text('home-server-name'), 'Beta'); assert.equal(h.summaryRequests().length, 2);
});
test('first load authenticates before requesting summary with no-store, same-origin GET', async () => {
    const h = await ready(); assert.deepEqual(h.requests.map(x => x.url), ['/api/session.cgi','/api/home/summary.cgi']); assert.equal(h.text('current-user'), 'fixture-user');
    for (const r of h.requests) {assert.equal(r.opts.credentials, 'same-origin'); assert.equal(r.opts.cache, 'no-store'); assert.equal(r.opts.method || 'GET', 'GET'); assert.ok(r.opts.signal);}
    assert.equal(h.nodes.get('app').hidden, false); assert.equal(h.nodes.get('page-loader').hidden, true);
});
test('successful periodic refresh changes all seven modules', async () => {
    const h = await ready(), s = summary('Next', 8); s.xray.version='test-next'; s.subscriptions.enabled=2; s.subscriptions.total=2;
    s.routes.installedBundles=3; s.keenetic.connected=false; s.broray.version='candidate'; s.dns.managedPresentCount=2;
    s.installedRelease={version:'candidate'};
    h.setSummary(s); await h.clock.advance(30000);
    for (const [id,value] of Object.entries({'home-xray-version':'test-next','home-servers-total':'8','home-subscriptions-enabled':'2 из 2','home-routes-count':'3','home-keenetic-state':'Нет подключения','home-broray-version':'candidate','home-dns-selected':'3 из 8','home-dns-installed':'2 из 3'})) assert.equal(h.text(id),value);
});
test('duplicate home.js execution does not duplicate requests, handlers or timers', async () => {
    const h = await ready(); vm.runInContext(source,h.ctx); await h.clock.advance(30000); assert.equal(h.summaryRequests().length,2); assert.equal(h.button().listeners.get('click').length,1); assert.equal(h.clock.timers.size,1);
});
test('multiple refresh signals while a request runs are single-flight', async () => {
    const h = await ready(); h.plans.push({hold:true}); await h.click('refresh-status');
    for(let i=0;i<10;i++){await h.click('refresh-status'); h.window.dispatch('online'); h.document.dispatch('visibilitychange');} await flush();
    assert.equal(h.summaryRequests().length,2); assert.equal(h.button().disabled,true); h.requests.at(-1).finish(); await flush(); assert.equal(h.button().disabled,false); assert.equal(h.clock.timers.size,1);
});
test('body parsing remains part of the single-flight request', async () => {
    const h = await ready(); h.plans.push({holdBody:true}); await h.click('refresh-status'); await h.click('refresh-status'); assert.equal(h.summaryRequests().length,2);
    h.requests.at(-1).finishBody(); await flush(); assert.equal(h.button().disabled,false);
});
test('timeout covers response body and enables retry', async () => {
    const h = await ready(); h.plans.push({holdBody:true}); await h.click('refresh-status'); await h.clock.advance(15000);
    assert.equal(h.button().disabled,false); assert.equal(h.text('home-health'),'Данные не обновлены'); assert.equal(h.text('home-server-name'),'Alpha');
});
test('fetch timeout aborts and stale late response cannot overwrite the next response', async () => {
    const h = await ready(); h.plans.push({hold:true,ignoreAbort:true,data:{success:true,data:summary('Late')}}); await h.click('refresh-status'); const old=h.requests.at(-1);
    await h.clock.advance(15000); assert.equal(old.aborted,true); h.setSummary(summary('New')); await h.click('refresh-status'); old.finish(); await flush(); assert.equal(h.text('home-server-name'),'New');
});
test('late unauthorized response from cancelled request does not redirect current page', async () => {
    const h=await ready(); h.plans.push({hold:true,ignoreAbort:true,status:401}); await h.click('refresh-status'); const old=h.requests.at(-1);
    await h.visible(false); await h.visible(true); old.finish(); await flush(); assert.deepEqual(h.redirects,[]);
});
test('network failure preserves last values and marks them not updated', async () => {
    const h=await ready(); const stamp=h.text('home-updated-at'); h.plans.push({error:'fixture network error'}); await h.click('refresh-status');
    assert.equal(h.text('home-server-name'),'Alpha'); assert.equal(h.text('home-servers-total'),'7'); assert.equal(h.text('home-updated-at'),stamp); assert.equal(h.text('home-health'),'Данные не обновлены'); assert.match(h.text('home-refresh-feedback'),/Последние|последние/);
});
test('HTTP 500 does not wipe data; next success clears only transport warning', async () => {
    const h=await ready(); h.plans.push({status:500,data:{success:false,error:{message:'fixture unavailable'}}}); await h.click('refresh-status'); assert.equal(h.nodes.get('home-refresh-feedback').hidden,false);
    const s=summary();s.health={severity:'warning',reasons:[{message:'Fixture module warning'}]};h.setSummary(s);await h.click('refresh-status');assert.equal(h.nodes.get('home-refresh-feedback').hidden,true);assert.equal(h.text('home-warning'),'Fixture module warning');
});
test('invalid JSON response retains old counts', async () => {const h=await ready();h.plans.push({badJson:true});await h.click('refresh-status');assert.equal(h.text('home-servers-total'),'7');assert.equal(h.text('home-health'),'Данные не обновлены');});
for (const [name,data] of Object.entries({null:null,array:[],string:'bad',empty:{},missingData:{success:true},missingModules:{success:true,data:{health:health()}},scalarModule:{success:true,data:{...summary(),servers:42}},arrayHealth:{success:true,data:{...summary(),health:[]}},errorEnvelope:{ok:false,error:'failure'}})) {
    test(`malformed summary ${name} is not a successful refresh`,async()=>{const h=await ready();h.plans.push({data});await h.click('refresh-status');assert.equal(h.text('home-server-name'),'Alpha');assert.equal(h.text('home-health'),'Данные не обновлены');assert.equal(h.button().disabled,false);});
}
test('missing module snapshots clear old metrics rather than display zeros or previous values',async()=>{
    const h=await ready(), s=summary();for(const m of ['xray','servers','subscriptions','dns','routes','keenetic','broray'])s[m]=null;s.health={severity:'error',reasons:[{message:'Snapshots unavailable'}]};h.setSummary(s);await h.click('refresh-status');
    for(const id of ['home-xray-version','home-xray-config','home-servers-total','home-server-quality','home-subscriptions-enabled','home-subscriptions-servers','home-dns-selected','home-dns-installed','home-routes-count','home-routes-update','home-keenetic-link','home-keenetic-state','home-broray-version','home-broray-update'])assert.equal(h.text(id),'—',id);
    assert.equal(h.text('home-health'),'Требуется исправление');
});
test('successful HTTP does not hide backend stale snapshot warning',async()=>{
    const h=await ready(),s=summary();s.health={severity:'warning',reasons:[{message:'Snapshot stale'}]};s.servers.health=s.health;h.setSummary(s);await h.clock.advance(30000);assert.equal(h.text('home-health'),'Требуется внимание');assert.equal(h.text('home-warning'),'Snapshot stale');assert.equal(h.text('home-servers-status'),'Требуется внимание');
});
test('automatic refreshes do not emit success toasts; manual refresh does',async()=>{const h=await ready();await h.clock.advance(60000);assert.equal(h.toasts.length,0);await h.click('refresh-status');assert.deepEqual(h.toasts,[['Сводка обновлена.','success']]);});
test('hidden initial page does not start authentication or polling',async()=>{const h=await ready({hidden:true});assert.equal(h.requests.length,0);assert.equal(h.clock.timers.size,0);await h.visible(true);assert.equal(h.requests.length,2);});
test('hiding page aborts active fetch without displaying an error',async()=>{const h=await ready();h.plans.push({hold:true});await h.click('refresh-status');const r=h.requests.at(-1);await h.visible(false);assert.equal(r.aborted,true);assert.equal(h.nodes.get('home-refresh-feedback').hidden,true);assert.equal(h.clock.timers.size,0);});
test('immediate hide/show while abort settles schedules exactly one fresh request',async()=>{
    const h=await ready();h.plans.push({hold:true});await h.click('refresh-status');h.document.hidden=true;h.document.dispatch('visibilitychange');h.document.hidden=false;h.document.dispatch('visibilitychange');await flush();await h.clock.advance(0);assert.equal(h.summaryRequests().length,3);assert.equal(h.clock.timers.size,1);
});
test('pagehide suspends and persisted pageshow revalidates session and resumes',async()=>{
    const h=await ready();h.window.dispatch('pagehide');await h.clock.advance(120000);assert.equal(h.requests.length,2);h.setSummary(summary('Restored'));h.window.dispatch('pageshow',{persisted:true});await flush();assert.deepEqual(h.requests.map(x=>x.url),['/api/session.cgi','/api/home/summary.cgi','/api/session.cgi','/api/home/summary.cgi']);assert.equal(h.text('home-server-name'),'Restored');
});
test('normal pageshow does not duplicate first load',async()=>{const h=await ready();h.window.dispatch('pageshow',{persisted:false});await flush();assert.equal(h.requests.length,2);});
test('online event retries once without creating another poll loop',async()=>{const h=await ready();h.setSummary(summary('Online'));h.window.dispatch('online');await flush();assert.equal(h.text('home-server-name'),'Online');assert.equal(h.clock.timers.size,1);});
test('initial non-JSON 401 redirects once without parsing body or polling',async()=>{
    const h=harness({hidden:true});h.plans.push({status:401,badJson:true});await h.visible(true);assert.deepEqual(h.redirects,['/']);assert.equal(h.requests[0].jsonCalled,undefined);assert.equal(h.nodes.get('app').hidden,true);await h.clock.advance(120000);assert.equal(h.requests.length,1);assert.equal(h.clock.timers.size,0);
});
test('summary 401 stops refresh after expiration; visibility/online cannot restart',async()=>{const h=await ready();h.plans.push({status:401,badJson:true});await h.click('refresh-status');await h.visible(false);await h.visible(true);h.window.dispatch('online');await h.clock.advance(300000);assert.deepEqual(h.redirects,['/']);assert.equal(h.requests.length,3);assert.equal(h.clock.timers.size,0);});
test('logout cancels pending request and prevents all further page requests',async()=>{const h=await ready();h.plans.push({hold:true});await h.click('refresh-status');const r=h.requests.at(-1);await h.click('logout-button');await h.clock.advance(300000);h.window.dispatch('online');await flush();assert.equal(r.aborted,true);assert.equal(h.requests.length,3);assert.equal(h.clock.timers.size,0);});
test('initial session error exposes retry instead of leaving endless loader',async()=>{
    const h=harness({hidden:true});h.plans.push({status:503,data:{success:false}});await h.visible(true);assert.equal(h.nodes.get('page-loader').hidden,true);assert.equal(h.button().disabled,false);assert.equal(h.text('home-health'),'Сводка недоступна');assert.equal(h.summaryRequests().length,0);await h.click('refresh-status');assert.equal(h.text('home-server-name'),'Alpha');
});
test('session with no verified user cannot start summary',async()=>{const h=harness({hidden:true});h.plans.push({data:{ok:true,authenticated:false}});await h.visible(true);assert.equal(h.summaryRequests().length,0);assert.equal(h.text('home-health'),'Сводка недоступна');});
test('initial authentication timeout is bounded and retryable',async()=>{const h=harness({hidden:true});h.plans.push({hold:true});await h.visible(true);await h.clock.advance(15000);assert.equal(h.text('home-health'),'Сводка недоступна');assert.equal(h.button().disabled,false);await h.click('refresh-status');assert.equal(h.text('home-server-name'),'Alpha');});
test('retry intervals back off 30/60/120 seconds, cap at 120 and reset after success',async()=>{
    const h=await ready();h.plans.push(...Array.from({length:4},()=>({error:'offline'})));await h.click('refresh-status');
    await h.clock.advance(30000);await h.clock.advance(60000);await h.clock.advance(120000);assert.deepEqual(h.summaryRequests().map(r=>r.at),[0,0,30000,90000,210000]);
    await h.clock.advance(120000);await h.clock.advance(30000);assert.deepEqual(h.summaryRequests().map(r=>r.at),[0,0,30000,90000,210000,330000,360000]);
});
test('without optional refresh button automatic refresh still works',async()=>{const h=await ready({noButton:true});await h.clock.advance(30000);assert.equal(h.summaryRequests().length,2);});
test('without AbortController no uncontrolled request is launched',async()=>{const h=await ready({noAbort:true});assert.equal(h.requests.length,0);assert.match(h.text('home-refresh-feedback'),/браузер/);assert.equal(h.clock.timers.size,0);});
test('summary cache timestamps and health are not rewritten to client now',async()=>{const h=await ready();assert.match(h.text('home-updated-at'),/17/);assert.match(h.nodes.get('home-updated-at').title,/модуля/);const before=h.text('home-updated-at');await h.clock.advance(30000);assert.equal(h.text('home-updated-at'),before);});

test('installed release version is independent of an expired previous-release snapshot',async()=>{
    const h=await ready(),s=summary();
    s.installedRelease={version:'3.2.0',candidateId:'3.2.0-r01c12'};
    s.broray._snapshot={freshness:'expired'};
    h.setSummary(s);await h.click('refresh-status');
    assert.equal(h.text('home-broray-version'),'3.2.0');
    assert.equal(h.text('home-broray-status'),'Данные устарели');
    assert.equal(h.text('home-broray-update'),'—');
});
test('missing current release metadata never falls back to a cached installed version',async()=>{
    const h=await ready(),s=summary();s.installedRelease=null;
    h.setSummary(s);await h.click('refresh-status');
    assert.equal(h.text('home-broray-version'),'—');
});
test('current release version remains available without a component snapshot',async()=>{
    const h=await ready(),s=summary();s.installedRelease={version:'3.2.1'};s.broray=null;
    h.setSummary(s);await h.click('refresh-status');
    assert.equal(h.text('home-broray-version'),'3.2.1');
    assert.equal(h.text('home-broray-status'),'Недоступно');
});
