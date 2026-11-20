import fs from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert/strict';
import test from 'node:test';

test('BROray journal button expands and collapses without changing journal requests', async () => {
    const html = fs.readFileSync(new URL('./static/broray.html', import.meta.url), 'utf8');
    const script = fs.readFileSync(new URL('./static/assets/js/background-operations.js', import.meta.url), 'utf8');
    assert.match(html, /<button id="bg-journal-toggle"[^>]*aria-controls="bg-journal" aria-expanded="false">Показать журнал<\/button>/);
    assert.match(html, /<div id="bg-journal" hidden>/);

    const elements = new Map();
    const requests = [];
    let complete = true;
    function element(id) {
        if (!elements.has(id)) {
            elements.set(id, {
                hidden: false, attributes: {}, events: {},
                addEventListener(name, callback) { this.events[name] = callback; },
                setAttribute(name, value) { this.attributes[name] = value; },
                append() {}, appendChild() {}, replaceChildren() {}
            });
        }
        return elements.get(id);
    }
    const button = element('bg-journal-toggle');
    const panel = element('bg-journal');
    const status = element('bg-journal-badge');
    panel.hidden = true;
    status.hidden = true;
    const document = {
        readyState: 'complete', hidden: true, getElementById: element,
        addEventListener() {}, createElement() { return element(Symbol()); }
    };
    vm.runInNewContext(script, {
        document, AbortController, setTimeout, clearTimeout,
        async fetch(url, options) {
            requests.push([url, options.method]);
            return {ok: true, json: async () => ({complete, events: [], operations: [], queue: []})};
        }
    });
    await new Promise(setImmediate);
    assert.equal(status.hidden, true);
    const initialRequests = requests.length;
    button.events.click.call(button);
    assert.equal(panel.hidden, false);
    assert.equal(button.attributes['aria-expanded'], 'true');
    assert.equal(button.textContent, 'Скрыть журнал');
    button.events.click.call(button);
    assert.equal(panel.hidden, true);
    assert.equal(button.attributes['aria-expanded'], 'false');
    assert.equal(button.textContent, 'Показать журнал');
    assert.equal(requests.length, initialRequests);

    complete = false;
    await element('bg-journal-refresh').events.click();
    assert.equal(status.hidden, false);
    assert.equal(status.textContent, 'Журнал неполный');
    assert.ok(requests.every(([, method]) => method === 'GET'));
});
