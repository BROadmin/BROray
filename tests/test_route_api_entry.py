"""Original route CGIs in a private Linux root; auth/backend fixtures only."""
import ctypes, json, os, shutil, subprocess, unittest
from pathlib import Path
from test_route_entry import ROOT, RouteEntry


class RouteApiEntry(unittest.TestCase):
    wait_cli=RouteEntry.wait_cli

    def setUp(self):
        RouteEntry.setUp(self)
        self.link=Path('/opt/broray'); self.assertFalse(self.link.exists())
        self.link.symlink_to(self.app)
        self.api=self.app/'web-new/api'
        shutil.copytree(ROOT/'implementation/runtime/app/web-new/api',self.api)
        shutil.copy2(ROOT/'implementation/runtime/app/bin/broray-routes-user',self.app/'bin')
        (self.app/'tmp').mkdir()
        (self.app/'lib/web-auth.sh').write_text('''broray_cookie_value() { printf fixture; }
broray_session_validate() { [ "${TEST_AUTH:-yes}" = yes ]; }
''')
        for folder in ['manifests','state']:(self.app/'routes'/folder).mkdir()
        (self.app/'routes/bundles.json').write_text('{"schemaVersion":1,"bundles":["fixture"]}')
        (self.app/'routes/config.json').write_text('{"managedInterface":"Proxy0"}')
        (self.app/'routes/manifests/fixture.json').write_text('{"id":"fixture","targetInterface":"Proxy0","exportComment":"BROray"}')
        (self.app/'routes/state/fixture.json').write_text('{"schemaVersion":1,"bundleId":"fixture","status":"available"}')
        self.env.update(REQUEST_METHOD='POST',QUERY_STRING='bundleId=fixture')

    def tearDown(self):
        self.assertEqual(self.link.resolve(),self.app.resolve()); self.link.unlink()
        RouteEntry.tearDown(self)

    def run_api(self,name='check.cgi',body=None):
        if body is not None:
            self.env['CONTENT_LENGTH']=str(len(body)); self.env['CONTENT_TYPE']='application/json'
        p=subprocess.Popen(['/bin/ash',str(self.api/'routes'/name)],env=self.env,
            stdin=subprocess.PIPE if body is not None else subprocess.DEVNULL,
            stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        if body is not None:p.stdin.write(body);p.stdin.close();p.stdin=None
        result=self.wait_cli(p)
        self.assertEqual(result.returncode,0,(result.stdout,result.stderr))
        head,data=result.stdout.decode().replace('\r\n','\n').split('\n\n',1)
        return head,json.loads(data)

    def states(self):
        return [json.loads(p.read_text()) for p in (self.state/'operations').glob('*/state.json')]

    def test_web_check_and_cli_share_one_protected_owner(self):
        (self.app/'lib/routes-download.sh').write_text('''broray_routes_check_run() {
    test -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" || return 93
    jq -e '.scope=="routes" and .cancelability=="protected" and .running' \
      "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json" >/dev/null || return 94
    echo CHANGED >"$BRORAY_ROOT/changed"
}
''')
        _,data=self.run_api(); self.assertTrue(data['success'],data)
        states=self.states(); self.assertEqual(len(states),1)
        self.assertEqual(states[0]['state'],'completed')
        self.assertFalse(self.lock.exists()); self.assertFalse(self.lock.is_symlink())

    def test_http_error_is_recorded_as_failed(self):
        (self.app/'lib/routes-download.sh').write_text('broray_routes_check_run() { return 42; }\n')
        head,data=self.run_api(); self.assertIn('502',head); self.assertFalse(data['success'])
        states=self.states(); self.assertEqual(len(states),1)
        self.assertEqual(states[0]['state'],'failed')
        self.assertFalse(self.lock.exists()); self.assertFalse(self.lock.is_symlink())

    def test_auth_failure_does_not_start_job(self):
        self.env['TEST_AUTH']='no'
        head,data=self.run_api(); self.assertIn('401',head); self.assertFalse(data['success'])
        self.assertEqual(self.states(),[]); self.assertFalse((self.app/'changed').exists())

    def test_foreign_fence_preserves_web_conflict(self):
        self.lock.mkdir(); (self.lock/'sentinel').write_text('KEEP')
        head,data=self.run_api(); self.assertIn('409',head); self.assertFalse(data['success'])
        self.assertEqual((self.lock/'sentinel').read_text(),'KEEP')
        self.assertEqual(self.states(),[]); self.assertFalse((self.app/'changed').exists())

    def test_custom_preview_preserves_request_body(self):
        (self.app/'lib/routes-user-import.sh').write_text('''broray_user_routes_cleanup() { :; }
broray_user_routes_preview() {
    test -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" || return 93
    cat "$1"
}
''')
        body=json.dumps({'label':'Маршруты теста','input':['192.0.2.0/24']},ensure_ascii=False).encode()
        _,data=self.run_api('custom-preview.cgi',body)
        self.assertTrue(data['success'],data); self.assertEqual(data['data'],json.loads(body))
        states=self.states(); self.assertEqual(len(states),1)
        self.assertIn(states[0]['bundleId'],('',None)); self.assertEqual(states[0]['state'],'completed')

    def test_custom_list_during_foreign_fence_does_not_initialize_catalog(self):
        self.env['REQUEST_METHOD']='GET'
        self.lock.mkdir(); (self.lock/'sentinel').write_text('KEEP')
        before=set(self.app.rglob('*'))
        _,data=self.run_api('custom-list.cgi')
        self.assertTrue(data['success'],data); self.assertEqual(data['data']['bundles'],[])
        self.assertEqual(set(self.app.rglob('*')),before)
        self.assertEqual(self.states(),[]); self.assertEqual((self.lock/'sentinel').read_text(),'KEEP')


if __name__=='__main__':
    if os.name=='nt':raise SystemExit('Run in isolated Linux guest')
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    Path('/opt/bin').mkdir(parents=True,exist_ok=True)
    subprocess.run(['/bin/busybox','--install','-s','/opt/bin'],check=True)
    if not Path('/opt/bin/jq').exists():Path('/opt/bin/jq').symlink_to('/usr/bin/jq')
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RouteApiEntry))
    (ROOT/'docs/evidence/route-api-entry-tests.json').write_text(json.dumps({
        'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
        'environment':'Real Linux processes, original route CGIs and CLI, private auth/backend fixtures',
        'routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
