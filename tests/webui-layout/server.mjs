// Loopback-only fixture: real WebUI markup/assets, controlled operation status.
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {fileURLToPath} from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = path.resolve(here, '../..');
const assets = path.join(source, 'webui/static');
const arg = name => process.argv.find(x => x.startsWith(name + '='))?.slice(name.length + 1);
const port = Number(arg('--port') || 28411);
const readyFile = arg('--ready');
const fixtureID = 'broray-route-toast-layout-v1';
let scene = {mode: 'paused', longTitle: false};
let closing = false;

function status() {
    const active = scene.mode !== 'inactive';
    const running = scene.mode === 'running';
    const resumable = active && !running;
    const id = active ? 'user-layout-fixture' : null;
    const message = resumable
        ? 'Удаление маршрутов остановлено по запросу пользователя. Операцию можно продолжить.'
        : 'Установка маршрутов в Keenetic.';
    return {
        schemaVersion: 1, generatedAt: '2026-11-06T00:00:00+0000',
        active, pending: resumable, running, resumable, canStop: running,
        bundleId: id, bundleType: active ? 'custom' : null,
        bundleName: active ? (scene.longTitle ? 'Пользовательский набор с длинным названием для проверки изменения высоты панели' : 'Проверка положения уведомлений') : null,
        action: active ? (running ? 'export' : 'resume-required') : null,
        globalOperation: {active, pending: resumable, resumable, scope: active ? 'routes' : null,
            action: active ? (running ? 'export' : 'resume-required') : null, bundleId: id,
            pid: null, startedAt: null, updatedAt: null, stale: false},
        progress: {schemaVersion: 2, kind: 'routes', bundleId: id || '',
            operation: running ? 'install' : 'delete', phase: active ? (running ? 'applying' : 'paused') : 'idle',
            current: active ? 16 : 0, total: active ? 254 : 0, percent: active ? 6 : 0,
            currentRoute: null, message, running, success: null, rolledBack: false,
            resumable, canStop: running, stopRequested: false, stoppedByUser: resumable,
            resumed: false, errorRoute: null, pid: null, startedAt: null, updatedAt: null,
            completedAt: null, backgroundOperationId: active ? 'op-local-layout-fixture' : ''}
    };
}

function json(res, code, value) {
    res.writeHead(code, {'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store'});
    res.end(JSON.stringify(value));
}

function fixtureHTML(url) {
    let html = fs.readFileSync(path.join(assets, 'routes-custom.html'), 'utf8');
    html = html.replace(/<script\b[^>]*>[\s\S]*?<\/script>/gi, '');
    const theme = url.searchParams.get('theme') === 'dark' ? 'dark' : 'light';
    html = html.replace('</head>', '<script>document.documentElement.setAttribute("data-theme",' + JSON.stringify(theme) + ');</script></head>');
    const scripts = ['common.js', 'icons.js', 'dialogs.js', 'routes-operation.js']
        .map(name => '<script src="/assets/js/' + name + '"></script>').join('');
    return html.replace('</body>', scripts + '<script src="/__fixture__/probe.js"></script></body>');
}

const server = http.createServer(async (req, res) => {
    try {
        const url = new URL(req.url, 'http://127.0.0.1:' + port);
        if (url.pathname === '/__fixture__/info' && req.method === 'GET') {
            return json(res, 200, {fixtureID, pid: process.pid, source});
        }
        if (url.pathname === '/__fixture__/shutdown' && req.method === 'POST') {
            if (closing) return json(res, 409, {error: 'already closing'});
            closing = true;
            json(res, 200, {fixtureID, closing: true});
            server.close(() => process.exit(0));
            return;
        }
        if (url.pathname === '/__fixture__/state' && req.method === 'POST') {
            let body = '';
            for await (const chunk of req) {
                body += chunk;
                if (body.length > 1024) return json(res, 413, {error: 'fixture input too large'});
            }
            const value = JSON.parse(body);
            if (!['paused', 'running', 'inactive'].includes(value.mode)) return json(res, 400, {error: 'bad fixture mode'});
            scene = {mode: value.mode, longTitle: value.longTitle === true};
            return json(res, 200, {ok: true});
        }
        if (url.pathname === '/api/routes/operation-status.cgi' && req.method === 'GET') {
            return json(res, 200, {success: true, data: status()});
        }
        if (url.pathname === '/api/routes/stop.cgi' && req.method === 'POST') {
            if (url.searchParams.get('bundleId') !== 'user-layout-fixture' ||
                url.searchParams.get('operationId') !== 'op-local-layout-fixture') {
                return json(res, 409, {success: false, error: {code: 'FIXTURE_WRONG_OPERATION'}});
            }
            scene.mode = 'paused';
            return json(res, 202, {success: true, data: {accepted: true}});
        }
        if (url.pathname === '/__fixture__/probe.js' && req.method === 'GET') {
            res.writeHead(200, {'Content-Type': 'text/javascript; charset=utf-8', 'Cache-Control': 'no-store'});
            res.end(fs.readFileSync(path.join(here, 'probe.js')));
            return;
        }
        if (url.pathname === '/layout' && req.method === 'GET') {
            const mode = url.searchParams.get('mode') || 'paused';
            if (!['paused', 'running', 'inactive'].includes(mode)) return json(res, 400, {error: 'bad mode'});
            scene = {mode, longTitle: url.searchParams.get('long') === '1'};
            res.writeHead(200, {'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store'});
            res.end(fixtureHTML(url));
            return;
        }
        if (!['GET', 'HEAD'].includes(req.method) ||
            !(url.pathname.startsWith('/assets/') || url.pathname === '/manifest.json')) {
            return json(res, 404, {error: 'fixture route not found'});
        }
        const relative = decodeURIComponent(url.pathname).replace(/^\/+/, '');
        const file = path.resolve(assets, relative);
        if (!file.startsWith(assets + path.sep) || !fs.statSync(file).isFile()) return json(res, 404, {error: 'asset not found'});
        const types = {'.css':'text/css', '.js':'text/javascript', '.svg':'image/svg+xml', '.png':'image/png',
            '.ico':'image/x-icon', '.woff2':'font/woff2', '.json':'application/json'};
        res.writeHead(200, {'Content-Type': types[path.extname(file)] || 'application/octet-stream', 'Cache-Control':'no-store'});
        res.end(req.method === 'HEAD' ? undefined : fs.readFileSync(file));
    } catch (error) {
        json(res, 500, {error: String(error.message)});
    }
});
server.on('error', error => { console.error(error.stack); process.exitCode = 1; });
server.listen(port, '127.0.0.1', () => {
    const info = {fixtureID, pid: process.pid, port, host: '127.0.0.1', source,
        serverSHA256: crypto.createHash('sha256').update(fs.readFileSync(fileURLToPath(import.meta.url))).digest('hex')};
    if (readyFile) fs.writeFileSync(readyFile, JSON.stringify(info, null, 2) + '\n', {flag:'wx'});
    console.log(JSON.stringify(info));
});
setTimeout(() => { server.close(() => process.exit(0)); }, 90 * 60 * 1000).unref();
