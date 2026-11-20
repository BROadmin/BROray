const assert = require('node:assert/strict');
const { webcrypto } = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const { test } = require('node:test');
const vm = require('node:vm');

test('the browser submits subscription removal through the protected queue', async () => {
    const endpoint = '/api/subscriptions/delete.cgi';
    const id = 'sub-fixture';
    const saved = new Map();
    const window = { location: { origin: 'https://example.test' } };
    const requests = [];
    const fetch = async (url, options) => {
        const body = JSON.parse(options.body);
        requests.push({ url, method: options.method, headers: options.headers, body });
        assert.match(body.nonce, /^[a-f0-9]{32}$/);
        assert.equal(saved.get(`broray.queue.v1:${endpoint}:${id}`), body.nonce);
        return { ok: true, status: 202, json: async () => ({
            ok: true, requestId: `q-${body.nonce}`, state: 'queued'
        }) };
    };
    const source = fs.readFileSync(path.join(__dirname, '../../webui/static/assets/js/common.js'), 'utf8');
    vm.runInNewContext(source, {
        window, document: {}, fetch, AbortController, setTimeout, clearTimeout,
        crypto: webcrypto, Uint8Array,
        sessionStorage: { getItem: key => saved.get(key) ?? null,
            setItem: (key, value) => saved.set(key, value), removeItem: key => saved.delete(key) }
    });

    const result = await window.BROrayUI.submitQueued(endpoint, id);
    assert.equal(result.state, 'queued');
    assert.equal(requests.length, 1);
    assert.equal(requests[0].url, endpoint);
    assert.equal(requests[0].method, 'POST');
    assert.equal(requests[0].body.id, id);
    assert.equal(requests[0].headers['X-BROray-Queue'], '1');
});
