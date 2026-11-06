// UI smoke test: log in through the 3.x login page, open every page, record
// console errors, page errors and failed /api requests, take screenshots.
//   node tools/uitest/smoke.cjs [base] [outDir]
const { chromium } = require('playwright');
const fs = require('fs');

const base = process.argv[2] || 'http://127.0.0.1:18080';
const out = process.argv[3] || '/tmp/broray-uitest';
const pages = ['home', 'servers', 'subscriptions', 'xray', 'routes', 'routes-custom', 'routes-import', 'dns', 'keenetic', 'broray'];

(async () => {
  fs.mkdirSync(out, { recursive: true });
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await ctx.newPage();
  const report = {};
  let cur = 'login';
  const rec = () => (report[cur] = report[cur] || { console: [], pageErrors: [], api: [] });
  page.on('console', m => { if (m.type() === 'error') rec().console.push(m.text()); });
  page.on('pageerror', e => rec().pageErrors.push(String(e)));
  page.on('response', async r => {
    const u = new URL(r.url());
    if (u.pathname.startsWith('/api/')) {
      const row = { method: r.request().method(), path: u.pathname + u.search, status: r.status() };
      if (r.status() >= 400) {
        try { row.body = (await r.text()).slice(0, 300); } catch (e) { /* ignore */ }
      }
      rec().api.push(row);
    }
  });
  await page.goto(base + '/');
  await page.fill('#login', 'admin').catch(() => page.fill('input[name=login]', 'admin'));
  await page.fill('#password', 'dev').catch(() => page.fill('input[type=password]', 'dev'));
  await Promise.all([page.waitForURL(/home\.html/, { timeout: 15000 }).catch(() => {}), page.click('button[type=submit]')]);
  await page.screenshot({ path: `${out}/login-result.png` });
  for (const p of pages) {
    cur = p;
    rec();
    await page.goto(`${base}/${p}.html`, { waitUntil: 'networkidle', timeout: 30000 }).catch(e => rec().pageErrors.push('goto: ' + e.message));
    await page.waitForTimeout(1500);
    await page.screenshot({ path: `${out}/${p}.png`, fullPage: true });
  }
  await browser.close();
  fs.writeFileSync(`${out}/report.json`, JSON.stringify(report, null, 2));
  let bad = 0;
  for (const [p, r] of Object.entries(report)) {
    // An unauthenticated session probe is the login page's normal redirect
    // check. Keep it in report.json, but do not fail a successful login for it.
    const expectedLoginProbe = p === 'login' &&
      r.api.filter(a => a.method === 'GET' && a.path === '/api/session.cgi' &&
        a.status === 401 && a.body?.includes('"error":"SESSION_REQUIRED"')).length === 1 &&
      r.api.some(a => a.method === 'POST' && a.path === '/api/login.cgi' && a.status === 200) &&
      r.api.some(a => a.method === 'GET' && a.path === '/api/session.cgi' && a.status === 200);
    const failed = r.api.filter(a => a.status >= 400 && !(expectedLoginProbe &&
      a.method === 'GET' && a.path === '/api/session.cgi' && a.status === 401 &&
      a.body?.includes('"error":"SESSION_REQUIRED"')));
    let ignoredProbeConsole = false;
    const consoleErrors = r.console.filter(message => {
      if (expectedLoginProbe && !ignoredProbeConsole &&
          message === 'Failed to load resource: the server responded with a status of 401 (Unauthorized)') {
        ignoredProbeConsole = true;
        return false;
      }
      return true;
    });
    const n = consoleErrors.length + r.pageErrors.length + failed.length;
    bad += n;
    console.log(`${n ? 'FAIL' : 'ok  '} ${p}: console=${consoleErrors.length} pageErrors=${r.pageErrors.length} apiFailed=${failed.length} apiCalls=${r.api.length}`);
    for (const f of failed) console.log(`      ${f.status} ${f.method} ${f.path} ${f.body || ''}`);
    for (const e of r.pageErrors) console.log(`      pageerror ${e}`);
  }
  process.exit(bad ? 1 : 0);
})();
