// Shared helpers for UI end-to-end flows against tools/devrun.sh.
const { chromium } = require('playwright');
const fs = require('fs');
exports.open = async function (base, out) {
  fs.mkdirSync(out, { recursive: true });
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await ctx.newPage();
  const log = { console: [], pageErrors: [], api: [] };
  page.on('console', m => { if (m.type() === 'error') log.console.push(m.text()); });
  page.on('pageerror', e => log.pageErrors.push(String(e)));
  page.on('response', async r => {
    const u = new URL(r.url());
    if (!u.pathname.startsWith('/api/')) return;
    const row = { t: new Date().toISOString().slice(11, 19), m: r.request().method(), p: u.pathname + u.search, s: r.status() };
    if (r.status() >= 400) { try { row.b = (await r.text()).slice(0, 400); } catch (e) {} }
    log.api.push(row);
  });
  await page.goto(base + '/');
  await page.fill('#login', 'admin');
  await page.fill('#password', 'dev');
  await Promise.all([page.waitForURL(/home\.html/, { timeout: 15000 }), page.click('#login-submit')]);
  return { browser, page, log,
    shot: async (name) => page.screenshot({ path: `${out}/${name}.png`, fullPage: true }),
    maybeAccept: async () => { try { await page.waitForSelector('#confirm-root:not([hidden])', { timeout: 2500 }); await page.click('#confirm-accept'); return true; } catch (e) { return false; } },
    accept: async () => { await page.waitForSelector('#confirm-root:not([hidden])', { timeout: 15000 }); await page.click('#confirm-accept'); },
  };
};
