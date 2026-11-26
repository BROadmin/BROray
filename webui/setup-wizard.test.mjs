import {readFileSync} from "node:fs";
import assert from "node:assert/strict";
import {test} from "node:test";
import {runInNewContext} from "node:vm";

const read = (path) => readFileSync(new URL("./static/" + path, import.meta.url), "utf8");

test("setup links are available from Home and BROray and the page announces changes", () => {
    assert.match(read("home.html"), /id="home-quick-setup"[^>]*hidden/);
    assert.match(read("broray.html"), /href="\/setup\.html"/);
    const page = read("setup.html");
    assert.match(page, /name="setup-input"/);
    assert.match(page, /role="status"/);
    assert.match(page, /Настроить позже/);
    assert.match(page, /assets\/js\/setup-wizard\.js/);
});

function logic() {
    const window = {};
    runInNewContext(read("assets/js/setup-wizard.js"), {window, document: {getElementById: () => null}});
    return window.BROraySetup;
}

test("VPN readiness requires fresh HTTPS for the exact current server", () => {
    const {vpnVerified} = logic();
    const state = {activeServer: {id: "active"}, connectionState: "connected",
        activeConnection: {available: true, up: true, freshness: "fresh", method: "current-socks-https"}};
    assert.equal(vpnVerified(state, "active"), true);
    assert.equal(vpnVerified(state, "different"), false);
    assert.equal(vpnVerified({...state, activeConnection: {...state.activeConnection, freshness: "stale"}}), false);
});

test("Keenetic readback requires the exact preview and enabled state", () => {
    const {routeVerified} = logic();
    const preview = {id: "telegram", sourceRevision: "r", sourceSHA256: "hash"};
    const enabled = {phase: "enabled", preview};
    assert.equal(routeVerified(enabled, preview), true);
    assert.equal(routeVerified({...enabled, phase: "creating"}, preview), false);
    assert.equal(routeVerified({...enabled, preview: {...preview, sourceSHA256: "changed"}}, preview), false);
});

test("lost ordinary write resolves only from exact readback and is never replayed", async () => {
    const {applyOnce} = logic();
    let writes = 0;
    let marker;
    const lost = async () => { writes++; throw new Error("network"); };
    const intent = {kind: "route", id: "telegram"};
    const save = (value) => { marker = value; };
    const observed = await applyOnce(intent, save, lost, async () => "confirmed");
    assert.equal(observed, "confirmed");
    assert.equal(writes, 1);
    assert.equal(marker, null);
    await assert.rejects(applyOnce(intent, save, lost, async () => null), /результат/i);
    assert.equal(writes, 2);
    assert.equal(marker, intent);
});

function browser(handler, history = new Map()) {
    const elements = new Map();
    const panels = [1, 2, 3].map((number) => ({dataset: {setupStep: number}}));
    function element() {
        return {
            attrs: {}, listener: {}, hidden: false, value: "", children: [],
            classList: {toggle() {}},
            addEventListener(event, fn) { this.listener[event] = fn; },
            setAttribute(name, value) { this.attrs[name] = value; },
            removeAttribute(name) { delete this.attrs[name]; },
            replaceChildren(...children) { this.children = children; this.value = children[0].value; },
            append(child) { this.children.push(child); },
            get selectedOptions() { return this.children.filter((child) => child.value === this.value); },
            focus() { this.focused = true; },
            querySelectorAll(query) { return query === "[data-setup-step]" ? panels : [...elements.values()]; }
        };
    }
    const get = (id) => {
        if (!elements.has(id)) elements.set(id, element());
        return elements.get(id);
    };
    const scope = {
        window: {location: {origin: "https://router.example"}, BROrayDialogs: {confirm: async () => true},
            BROrayUI: {
                apiRequest: async (path, options) => path === "/api/session.cgi" ? {user: "fixture"} :
                    {success: true, data: await handler(path, options && options.body)},
                hasQueuedRequest: () => false,
                followQueued: async (path, id, callback) => {
                    await handler(path, {id, queued: true});
                    callback({state: "completed"});
                },
                queueLabel: ({state}) => state, redirectToLogin() { throw new Error("unexpected logout"); }
            }},
        document: {getElementById: get, createElement: element},
        sessionStorage: {getItem: (key) => history.get(key) ?? null, setItem: (key, value) => history.set(key, value)}
    };
    runInNewContext(read("assets/js/setup-wizard.js"), scope);
    return {get, history, panels, settled: () => new Promise((resolve) => setImmediate(resolve)),
        click: (id) => get(id).listener.click()};
}

const ready = {activeServer: {id: "live", name: "Current"}, connectionState: "connected",
    activeConnection: {available: true, up: true, freshness: "fresh", method: "current-socks-https"}};

test("configured router resumes an installed route after a lost reply without new writes", async () => {
    let applied = 0;
    const preview = {id: "example", name: "Example", domains: ["www.example.com"], skipped: [],
        sourceRevision: "fixture-revision", sourceSHA256: "fixture-hash"};
    const api = async (path, body) => {
        if (path.includes("/servers/summary")) return {...ready, servers: [], total: 0};
        if (path.includes("/subscriptions/list")) return [];
        if (path.includes("native-domain-preview")) return body ? preview : {presets: [preview]};
        if (path.includes("native-domain-apply") && body) {
            applied++;
            assert.equal(body.id, preview.id);
            assert.equal(body.sourceSHA256, preview.sourceSHA256);
            throw new Error("connection interrupted after commit");
        }
        if (path.includes("native-domain-apply")) return {states: applied ? {example: {phase: "enabled", preview}} : {}};
        throw new Error("unexpected API: " + path);
    };
    const ui = browser(api);
    await ui.settled();
    assert.equal(ui.get("setup-use-current").hidden, false);
    await ui.click("setup-use-current");
    await ui.click("setup-route-preview");
    await ui.click("setup-route-apply");
    assert.equal(ui.panels[2].hidden, false);
    assert.match(ui.get("setup-final-routes").textContent, /Маршруты установлены/);
    assert.equal(ui.get("setup-example-site").href, "https://www.example.com");
    assert.equal(applied, 1);
    const reload = browser(api, ui.history);
    await reload.settled();
    assert.equal(reload.panels[2].hidden, false);
    assert.equal(applied, 1);
});

test("clean router imports one profile, checks only that server and skips routes", async () => {
    const profile = {id: "live", name: "Imported", connectionSupport: {supported: true}};
    let imported = false;
    let checked = false;
    let connected = false;
    const calls = [];
    const api = async (path, body) => {
        calls.push([path, Boolean(body)]);
        if (path.includes("/servers/summary")) return {...(connected ? ready : {}),
            total: imported ? 1 : 0, servers: imported ? [{...profile, quality: checked ? {status: "available", freshness: "fresh"} : null}] : []};
        if (path.includes("/subscriptions/list")) return [];
        if (path.includes("/servers/import") && body.preview) return profile;
        if (path.includes("/servers/import") && body.uri === "fixture-link") { imported = true; return {id: profile.id}; }
        if (path.includes("/servers/check") && body.id === profile.id) { checked = true; return {}; }
        if (path.includes("/servers/activate") && body.id === profile.id) { connected = true; return {}; }
        if (path.includes("native-domain-preview")) return {presets: []};
        throw new Error("unexpected API: " + path);
    };
    const ui = browser(api);
    await ui.settled();
    assert.equal(ui.get("setup-use-current").hidden, true);
    ui.get("setup-input").value = "fixture-link";
    await ui.click("setup-import-preview");
    assert.equal(ui.get("setup-import-save").hidden, false);
    await ui.click("setup-import-save");
    assert.equal(ui.get("setup-input").value, "");
    await ui.click("setup-check");
    await ui.click("setup-connect");
    assert.equal(connected, true);
    assert.equal(ui.panels[1].hidden, false);
    await ui.click("setup-skip");
    assert.match(ui.get("setup-final-vpn").textContent, /VPN проверен/);
    assert.match(ui.get("setup-final-routes").textContent, /позже/);
    assert.equal(calls.filter(([path, post]) => post && path.includes("native-domain-apply")).length, 0);
    assert.doesNotMatch([...ui.history.values()].join(""), /fixture-link/);
});
