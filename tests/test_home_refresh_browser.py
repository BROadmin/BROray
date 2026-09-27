"""Original Home HTML + complete production home.js in Chromium.

No navigation or network: static Home markup is loaded with set_content and
fetch is an in-memory fixture. Scripts, styles and image loads are removed.
This is a DOM/controller test, NOT full WebUI/visual/router acceptance.
Requires Python Playwright and a Chromium executable; nothing is installed here.
"""
from pathlib import Path
import re
import json
import os
import unittest
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
JS = Path(os.environ.get('BRORAY_HOME_JS', ROOT / 'runtime/app/web-new/assets/js/home.js'))
HTML = Path(os.environ.get('BRORAY_HOME_HTML', ROOT / 'runtime/app/web-new/home.html'))

def snapshot(name='Alpha', total=7):
    h = {'severity': 'ok', 'reasons': []}
    return {'health': h, 'errors': [], 'updatedAt': '2026-09-17T08:00:00Z',
        'servers': {'health': h, 'connectionState': 'connected', 'total': total,
                    'activeServer': {'name': name, 'quality': {'ping': 20}}},
        'xray': {'health': h, 'version': 'fixture', 'configValid': True, 'socksActive': True},
        'subscriptions': {'health': h, 'enabled': 1, 'total': 1, 'serversReceived': total},
        'dns': {'health': h, 'effectiveCount': 3, 'managedPresentCount': 3},
        'routes': {'health': h, 'installedBundles': 1, 'availableBundles': 10},
        'keenetic': {'health': h, 'link': True, 'connected': True, 'state': 'up'},
        'broray': {'installationHealthy': True, 'version': 'fixture'}}

class HomeBrowser(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.pw = sync_playwright().start()
        executable = os.environ.get('CHROMIUM_EXECUTABLE')
        cls.browser = cls.pw.chromium.launch(headless=True, **({'executable_path': executable} if executable else {}))
    @classmethod
    def tearDownClass(cls):
        cls.browser.close()
        cls.pw.stop()
    def setUp(self):
        self.context = self.browser.new_context(viewport={'width': 1440, 'height': 1000})
        self.page = self.context.new_page()
        self.page.clock.install()
        self.errors = []
        self.page.on('pageerror', lambda e: self.errors.append(str(e)))
        self.data = snapshot()
        self.mode = 'ok'
        self.page.route('**/*', lambda route: route.abort())
    def tearDown(self):
        self.assertEqual(self.errors, [], 'unhandled page JavaScript errors')
        self.context.close()
    def sync(self):
        self.page.evaluate("x => {window.fixture.data=x.data;window.fixture.mode=x.mode;}", {'data': self.data, 'mode': self.mode})
    def open(self):
        html = HTML.read_text(encoding='utf-8')
        html = re.sub(r'<script\b[^>]*>.*?</script\s*>', '', html, flags=re.S | re.I)
        html = re.sub(r'<link\b[^>]*>', '', html, flags=re.I)
        html = re.sub(r'<img\b[^>]*>', '', html, flags=re.I)
        self.page.set_content(html)
        self.page.evaluate("""() => {
            window.fixture={data:null,mode:'ok',requests:[],held:[]};
            window.fetch=(url, options={})=>{
                const f=window.fixture;
                f.requests.push({url,method:options.method||'GET',cache:options.cache,credentials:options.credentials});
                if(url==='/api/session.cgi') return Promise.resolve(new Response(JSON.stringify({ok:true,authenticated:true,user:'fixture-user'})));
                const body=JSON.stringify({success:true,data:f.data});
                if(f.mode==='hold') return new Promise((resolve,reject)=>{
                    options.signal.addEventListener('abort',()=>reject(new DOMException('Aborted','AbortError')),{once:true});
                    f.held.push(()=>resolve(new Response(JSON.stringify({success:true,data:f.data}))));
                });
                if(f.mode==='500') return Promise.resolve(new Response(JSON.stringify({success:false,error:{message:'Fixture unavailable'}}),{status:500}));
                if(f.mode==='bad') return Promise.resolve(new Response('{'));
                return Promise.resolve(new Response(body));
            };
        }""")
        self.sync()
        self.page.add_script_tag(content=JS.read_text(encoding='utf-8'))
        expect(self.page.locator('#home-server-name')).to_have_text('Alpha')
    def summaries(self):
        return self.page.evaluate("fixture.requests.filter(r=>r.url==='/api/home/summary.cgi').length")
    def test_01_visible_manual_button(self):
        self.open()
        b = self.page.get_by_role('button', name='Обновить сводку', exact=True)
        expect(b).to_be_visible()
        self.assertIsNone(b.get_attribute('tabindex'))
        self.data = snapshot('Beta', 8)
        self.sync()
        b.click()
        expect(self.page.locator('#home-server-name')).to_have_text('Beta')
        expect(self.page.locator('#home-servers-total')).to_have_text('8')
    def test_02_keyboard_and_mobile_viewport(self):
        self.page.set_viewport_size({'width': 390, 'height': 844})
        self.open()
        b = self.page.locator('#refresh-status')
        self.data = snapshot('Keyboard')
        self.sync()
        b.focus()
        self.page.keyboard.press('Enter')
        expect(self.page.locator('#home-server-name')).to_have_text('Keyboard')
    def test_03_periodic_refresh(self):
        self.open()
        self.data = snapshot('Timer')
        self.sync()
        self.page.clock.run_for(30000)
        expect(self.page.locator('#home-server-name')).to_have_text('Timer')
        self.assertEqual(self.summaries(), 2)
    def test_04_no_concurrent_summary(self):
        self.open()
        self.mode = 'hold'
        self.sync()
        self.page.locator('#refresh-status').click()
        expect(self.page.locator('#refresh-status')).to_be_disabled()
        self.page.evaluate("for (let i=0;i<10;i++) { document.getElementById('refresh-status').dispatchEvent(new Event('click')); window.dispatchEvent(new Event('online')); }")
        self.assertEqual(self.summaries(), 2)
        self.mode = 'ok'
        self.data = snapshot('Released')
        self.sync()
        self.page.evaluate('fixture.held.pop()()')
        expect(self.page.locator('#home-server-name')).to_have_text('Released')
    def test_05_timeout_recovery(self):
        self.open()
        self.mode = 'hold'
        self.sync()
        self.page.locator('#refresh-status').click()
        self.page.clock.run_for(60000)
        expect(self.page.locator('#home-health')).to_have_text('Данные не обновлены')
        expect(self.page.locator('#refresh-status')).to_be_enabled()
        self.mode = 'ok'
        self.data = snapshot('Recovered')
        self.sync()
        self.page.locator('#refresh-status').click()
        expect(self.page.locator('#home-server-name')).to_have_text('Recovered')
    def test_07_failed_request_retains_data_and_backend_warning_survives_recovery(self):
        self.open()
        self.mode = '500'
        self.sync()
        self.page.locator('#refresh-status').click()
        expect(self.page.locator('#home-refresh-feedback')).to_be_visible()
        expect(self.page.locator('#home-servers-total')).to_have_text('7')
        self.mode = 'ok'
        self.data['health'] = {'severity': 'warning', 'reasons': [{'message': 'Fixture snapshot stale'}]}
        self.sync()
        self.page.locator('#refresh-status').click()
        expect(self.page.locator('#home-refresh-feedback')).to_be_hidden()
        expect(self.page.locator('#home-warning')).to_have_text('Fixture snapshot stale')
    def test_08_missing_modules_clear_counters(self):
        self.open()
        for key in ['xray','servers','subscriptions','dns','routes','keenetic','broray']:
            self.data[key] = None
        self.data['health'] = {'severity': 'error', 'reasons': [{'message': 'Fixture absent'}]}
        self.sync()
        self.page.locator('#refresh-status').click()
        expect(self.page.locator('#home-servers-total')).to_have_text('—')
        expect(self.page.locator('#home-subscriptions-servers')).to_have_text('—')
        expect(self.page.locator('#home-xray-version')).to_have_text('—')
    def test_09_invalid_json_retains_existing_state(self):
        self.open()
        self.mode = 'bad'
        self.sync()
        self.page.locator('#refresh-status').click()
        expect(self.page.locator('#home-health')).to_have_text('Данные не обновлены')
        expect(self.page.locator('#home-server-name')).to_have_text('Alpha')
    def test_10_request_scope_is_only_authenticated_read_endpoints(self):
        self.open()
        self.sync()
        self.page.clock.run_for(30000)
        self.sync()
        self.page.locator('#refresh-status').click()
        expect(self.page.locator('#refresh-status')).to_be_enabled()
        requests = self.page.evaluate('fixture.requests')
        self.assertTrue(all(x['method'] == 'GET' for x in requests))
        self.assertTrue(all(x['url'] in ['/api/session.cgi','/api/home/summary.cgi'] for x in requests))

if __name__ == '__main__':
    unittest.main(verbosity=2)
