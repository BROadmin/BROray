#!/usr/bin/env node
"use strict";
const fs = require("fs");
const vm = require("vm");
const path = process.argv[2] || "/opt/broray/web-new/assets/js/routes-dot.js";
function fail(message) { console.error("BROray DNS-over-TLS UI self-test: FAIL — " + message); process.exit(1); }
const sandbox = {
    window: {BROrayDotTestMode: true},
    document: {addEventListener: function () {}, getElementById: function () { return null; }},
    console, Date, Math, Number, String, Boolean, Array, Object, Promise, setTimeout, clearTimeout
};
sandbox.window.window = sandbox.window;
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync(path, "utf8"), sandbox, {filename:path});
const hooks = sandbox.window.BROrayDotTestHooks;
if (!hooks) fail("тестовые функции не экспортированы");
const now = 2000000;
const fresh = {ok:true,testedEpoch:(now-1000)/1000,latencyMs:25};
const stale = {ok:true,testedEpoch:(now-700000)/1000,latencyMs:25};
if (!hooks.serverTestFresh(fresh, now)) fail("свежая проверка не распознана");
if (hooks.serverTestFresh(stale, now)) fail("устаревшая проверка признана свежей");
const base = {
    runningConfigAvailable:true,
    drift:false,
    managed:[],
    servers:[
        {id:"google",address:"8.8.8.8",sni:"dns.google",present:false,test:fresh},
        {id:"cloudflare",address:"1.1.1.1",sni:"cloudflare-dns.com",present:false,test:fresh}
    ]
};
if (hooks.recommendedAction(base,["google","cloudflare"],now)!=="apply") fail("проверенный набор не предлагает экспорт");
const installed = JSON.parse(JSON.stringify(base));
installed.servers.forEach(s=>s.present=true);
installed.managed=installed.servers.map(s=>({address:s.address,sni:s.sni}));
if (hooks.recommendedAction(installed,["google","cloudflare"],now)!=="test") fail("исправная конфигурация не предлагает следующую проверку");
const drift = JSON.parse(JSON.stringify(installed)); drift.drift=true; drift.servers[0].test=stale;
if (hooks.recommendedAction(drift,["google","cloudflare"],now)!=="test") fail("дрейф с устаревшей проверкой не предлагает тест");
if (hooks.recommendedAction({runningConfigAvailable:false,servers:[]},["google"],now)!=="refresh") fail("недоступный Keenetic не предлагает обновить состояние");
if (hooks.testPresentation({test:{ok:false,status:"failed",testedEpoch:Date.now()/1000}}).className!=="is-error") fail("ошибка TLS оформлена неверно");
console.log("BROray DNS-over-TLS UI self-test: PASS");
