import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert/strict';
import test from 'node:test';

const code = fs.readFileSync(new URL('./static/assets/js/routing-policy-page.js', import.meta.url), 'utf8');
const flush = () => new Promise(setImmediate);
const previewPath = '/api/routes/native-domain-preview.cgi';
const applyPath = '/api/routes/native-domain-apply.cgi';

async function fixture() {
    const elements = new Map(), requests = [];
    let document;
    class Element {
        constructor(tag) { this.tagName = tag; }
        children = [];
        events = {};
        attributes = {};
        dataset = {};
        text = '';
        get textContent() { return this.text + this.children.map(x => x.textContent).join(''); }
        set textContent(value) { this.text = value; this.children = []; }
        append(...items) { this.children.push(...items); }
        replaceChildren(...items) { this.children = items; this.text = ''; }
        setAttribute(name, value) { this.attributes[name] = value; }
        addEventListener(name, callback) { this.events[name] = callback; }
        click() { if (!this.disabled) return this.events.click?.call(this); }
        focus() { document.activeElement = this; }
        showModal() { this.open = true; }
        close() { this.open = false; this.events.close?.call(this); }
    }
    const fixed = id => {
        if (!elements.has(id)) elements.set(id, Object.assign(new Element('div'), {id}));
        return elements.get(id);
    };
    const find = (node, id) => node.id === id ? node : node.children.map(child => find(child, id)).find(Boolean);
    const dynamic = id => [...elements.values()].map(item => find(item, id)).find(Boolean);
    document = {
        body: {dataset: {page: 'routes-domains'}},
        getElementById: id => dynamic(id) || fixed(id),
        createElement: tag => new Element(tag),
        createTextNode: text => Object.assign(new Element('#text'), {text}),
        querySelectorAll: () => [],
        addEventListener() {}
    };
    const own = {id: 'own-demo', name: 'Свой список', domains: ['domain:own.example', 'full:one.example']};
    const state = {policy: {state: {rules: [own]}}};
    const makePreview = id => ({id, sourceRevision: id === own.id ? 'own' : 'rev',
        sourceSHA256: 'a'.repeat(64), domains: [id === own.id ? 'own.example' : 'service.example'],
        skipped: id === own.id ? [{value: 'full:one.example', reason: 'keenetic-includes-subdomains'}] : []});
    const states = {
        chatgpt: {phase: 'enabled', preview: makePreview('chatgpt')},
        'own-demo': {phase: 'enabled', preview: makePreview('own-demo')}
    };
    const envelope = data => ({success: true, data});
    const client = {snapshot: () => state, refresh: async () => {}, refreshSchedule: async () => {}};
    const window = {
        location: {origin: 'http://fixture.test'}, addEventListener() {},
        BROrayPolicy: {createClient: () => client},
        BROrayUI: {
            toast() {},
            async apiRequest(path, options) {
                requests.push({path, ...options});
                if (path === '/api/session.cgi') return {user: 'admin'};
                if (path === applyPath && !options) return envelope({states});
                if (path === applyPath) return envelope({phase: 'enabled'});
                if (path === previewPath && !options) return envelope({presets: [
                    {id: 'chatgpt', name: 'ChatGPT'}, {id: 'facebook', name: 'Facebook'}
                ]});
                if (path === previewPath) return envelope(makePreview(JSON.parse(options.body).id));
                throw new Error('Unexpected request ' + path);
            }
        }
    };
    vm.runInNewContext(code, {document, window, setTimeout: () => 0, clearTimeout() {}});
    await flush();
    return {fixed, dynamic, requests, makePreview, states};
}

test('ready and own cards keep their main actions visible and open details outside the grid', async () => {
    const h = await fixture();
    for (const id of ['chatgpt', 'facebook', 'own-demo']) {
        assert.ok(h.dynamic('native-preview-' + id));
        assert.ok(h.dynamic('native-apply-' + id));
    }
    h.dynamic('native-preview-own-demo').click();
    await flush();
    assert.equal(h.fixed('policy-domain-dialog').open, true);
    const contents = h.fixed('policy-domain-dialog-content').textContent;
    assert.match(contents, /own\.example/);
    assert.match(contents, /Пропущенные правила/);
    assert.match(contents, /full:one\.example/);
    assert.doesNotMatch(h.fixed('policy-own').textContent, /full:one\.example/);
    assert.equal(h.fixed('policy-domain-dialog-confirm').hidden, true);
    h.fixed('policy-domain-dialog').close();
    assert.equal(h.requests.filter(x => x.path === applyPath && x.method === 'POST').length, 0);
});

test('enabling a disabled domain list requires its preview and explicit confirmation', async () => {
    const h = await fixture();
    h.dynamic('native-apply-facebook').click();
    await flush();
    assert.equal(h.fixed('policy-domain-dialog').open, true);
    assert.equal(h.fixed('policy-domain-dialog-confirm').disabled, false);
    assert.match(h.fixed('policy-domain-dialog-content').textContent, /service\.example/);
    assert.equal(h.requests.filter(x => x.path === applyPath && x.method === 'POST').length, 0);
    h.fixed('policy-domain-dialog-confirm').click();
    await flush();
    const applied = h.requests.filter(x => x.path === applyPath && x.method === 'POST');
    assert.equal(applied.length, 1);
    assert.equal(JSON.stringify(applied[0].body), JSON.stringify({
        id: 'facebook', action: 'enable', sourceRevision: 'rev', sourceSHA256: 'a'.repeat(64)
    }));
});

test('an operation changing in Keenetic invalidates the open confirmation', async () => {
    const h = await fixture();
    h.dynamic('native-apply-chatgpt').click();
    await flush();
    assert.equal(h.fixed('policy-domain-dialog-confirm').disabled, false);
    h.states.chatgpt = {phase: 'removing', preview: h.makePreview('chatgpt')};
    await h.fixed('policy-refresh').click();
    assert.equal(h.fixed('policy-domain-dialog-confirm').disabled, true);
    h.fixed('policy-domain-dialog-confirm').click();
    assert.equal(h.requests.filter(x => x.path === applyPath && x.method === 'POST').length, 0);
});
