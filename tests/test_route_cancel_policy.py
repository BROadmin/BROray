"""Route cancellation policy; private state, synthetic owners, no router writes."""
import json, os, shutil, subprocess, unittest, uuid
from pathlib import Path
from test_operations import Operations, APP, BB, WORKSPACE
from test_operations_http import HTTP

class RoutePolicy(unittest.TestCase):
    def setUp(self):
        self.ops=Operations('test_normal_finish_and_next_begin'); self.ops.setUp()
        self.routes=self.ops.temp/'routes'; self.routes.mkdir()
        self.progress=self.routes/'operations'
        self.env={**self.ops.env,'BRORAY_ROUTES_ROOT':str(self.routes),
                  'BRORAY_ROUTES_PROGRESS_DIR':str(self.progress)}
    def tearDown(self): self.ops.tearDown()
    def begin(self,scope='routes',action='export'):
        a=self.ops.call('begin',scope,action,'fixture','USER','900001','cooperative',uuid.uuid4().hex)
        self.ops.call('ack',a['operationId'],a['token'],'900001'); return a
    def shell(self,body):
        return subprocess.run([str(BB),'ash','-c',body],env=self.env,capture_output=True,timeout=20)
    def fixture(self):
        p=self.shell('. "$BRORAY_ROOT/lib/routes-operation-progress.sh"\nbroray_routes_progress_begin fixture install 3\n')
        self.assertEqual(p.returncode,0,p.stderr)
    def test_admission_enforces_protection_for_route_actions_and_scope(self):
        for scope,action in [('routes','export'),('system','download'),('system','custom:commit'),('system','preflight:resume'),('routes','dot:apply')]:
            a=self.begin(scope,action)
            state=json.loads(self.ops.opfile(a,'state.json').read_bytes())
            self.assertEqual(state['cancelability'],'protected',(scope,action))
            self.ops.call('tick',a['operationId'],a['token'],'fetching')
            public=next(op for op in self.ops.call('status')['operations'] if op['operationId']==a['operationId'])
            self.assertEqual(public['cancelability'],'protected')
            self.ops.call('finish',a['operationId'],a['token'],'completed','')
    def test_cancel_and_stop_all_preserve_legacy_cooperative_route_record(self):
        a=self.begin()
        file=self.ops.opfile(a,'state.json'); state=json.loads(file.read_bytes())
        state.update(cancelability='cooperative',initialCancelability='cooperative'); file.write_text(json.dumps(state))
        before=file.read_bytes(); fence=(self.ops.temp/'global.lock').readlink()
        self.assertEqual(self.ops.call('cancel',a['operationId'],expected=2)['errorCode'],'CANCEL_NOT_SUPPORTED')
        result=self.ops.call('stop-background')
        self.assertTrue(result['automationPaused']); self.assertTrue(result['operations'][0]['protected'])
        self.assertFalse(result['operations'][0]['cancelRequested'])
        self.assertFalse(self.ops.opfile(a,'cancel.json').exists())
        self.assertEqual(file.read_bytes(),before); self.assertEqual((self.ops.temp/'global.lock').readlink(),fence)
        public=self.ops.call('status')['operations'][0]
        self.assertEqual(public['cancelability'],'protected'); self.assertEqual(public['type'],'route_operation')
    def test_legacy_stop_library_cannot_change_progress(self):
        self.fixture(); before={p.name:p.read_bytes() for p in self.progress.iterdir()}
        p=self.shell('. "$BRORAY_ROOT/lib/routes-operation-progress.sh"\nbroray_routes_progress_request_stop fixture\n')
        self.assertEqual(p.returncode,4,p.stderr)
        self.assertEqual({p.name:p.read_bytes() for p in self.progress.iterdir()},before)
    def test_cli_stop_rejected_without_writing_state(self):
        self.fixture(); before={p.name:p.read_bytes() for p in self.progress.iterdir()}
        p=subprocess.run([str(BB),'ash',str(APP/'bin/broray-routes'),'stop','fixture'],env=self.env,capture_output=True,timeout=20)
        self.assertEqual(p.returncode,1); self.assertIn('Остановка операций с маршрутами недоступна'.encode(),p.stderr)
        self.assertEqual({p.name:p.read_bytes() for p in self.progress.iterdir()},before)
    def test_old_stop_marker_does_not_interrupt_and_error_resume_is_preserved(self):
        self.fixture(); (self.progress/'fixture.stop').write_text('old request')
        p=self.shell('. "$BRORAY_ROOT/lib/routes-operation-progress.sh"\nbroray_routes_progress_stop_requested fixture\n')
        self.assertEqual(p.returncode,1)
        p=self.shell('''. "$BRORAY_ROOT/lib/routes-operation-progress.sh"
broray_routes_progress_begin fixture install 3
broray_routes_progress_update applying 1 3
broray_routes_progress_pause "Fixture error" false "203.0.113.0/24"
broray_routes_progress_resume_values fixture install 2
broray_routes_progress_read fixture
''')
        self.assertEqual(p.returncode,0,p.stderr)
        first,raw=p.stdout.split(b'\n',1); self.assertEqual(first,b'1\t3\ttrue')
        state=json.loads(raw); self.assertTrue(state['resumable']); self.assertFalse(state['canStop'])
        self.assertEqual(state['phase'],'failed_resumable')
    def test_dead_protected_route_keeps_fence(self):
        a=self.begin(); self.ops.set_owner({'status':'absent'})
        self.assertEqual(self.ops.call('recover',expected=2)['result'],'protected_recovery')
        self.assertTrue((self.ops.temp/'global.lock').exists())
    def test_subscription_remains_cancellable(self):
        a=self.ops.begin(); self.assertTrue(self.ops.call('cancel',a['operationId'])['cancelRequested'])

class RouteHTTP(HTTP):
    # Use HTTP's session fixture, but only run the route-specific cases below.
    def route_request(self,env=None,status=409):
        path=self.ops.temp/'route-stop.cgi'
        code=(APP/'web-new/api/routes/stop.cgi').read_text().replace('/opt/broray',str(self.ops.temp/'route-app'))
        root=self.ops.temp/'route-app'; (root/'web-new/api').mkdir(parents=True,exist_ok=True)
        (root/'routes').mkdir(exist_ok=True); (root/'routes/bundles.json').write_text('{"bundles":["fixture"]}')
        for name,target in [('lib',APP/'lib'),('web-new/api/auth-common.sh',APP/'web-new/api/auth-common.sh')]:
            dest=root/name
            if not dest.exists():dest.symlink_to(target)
        path.write_text(code)
        settings={**self.env,'REQUEST_METHOD':'POST','QUERY_STRING':'bundleId=fixture','CONTENT_LENGTH':'2',
                  'BRORAY_ROUTES_PROGRESS_DIR':str(self.ops.temp/'progress'),**(env or {})}
        p=subprocess.run([str(BB),'ash',str(path)],env=settings,input=b'{}',capture_output=True,timeout=20)
        self.assertEqual(p.returncode,0,p.stderr)
        headers,raw=p.stdout.split(b'\r\n\r\n',1); self.assertIn(f'Status: {status} '.encode(),headers,(headers,raw))
        return json.loads(raw)
    def test_route_endpoint_rejects_old_client_and_retains_auth(self):
        self.route_request({'HTTP_COOKIE':''},401)
        self.route_request({'REQUEST_METHOD':'GET'},405)
        result=self.route_request()
        self.assertEqual(result['error']['code'],'ROUTES_STOP_NOT_SUPPORTED')
        self.assertFalse((self.ops.temp/'progress').exists())
    def test_general_http_cancel_and_stop_all_leave_route_running(self):
        a=self.ops.call('begin','routes','export','fixture','USER','900001','cooperative',uuid.uuid4().hex)
        self.ops.call('ack',a['operationId'],a['token'],'900001')
        self.request('cancel','POST',{'operationId':a['operationId']},status=409)
        result=self.request('stop-background','POST',{'pauseAutomation':True},status=202)[1]
        self.assertTrue(result['operations'][0]['protected']); self.assertFalse(self.ops.opfile(a,'cancel.json').exists())

if __name__=='__main__':
    if os.name=='nt':raise SystemExit('Run in isolated Linux guest')
    (Path('/opt/bin')).mkdir(parents=True,exist_ok=True)
    Path('/opt/broray').symlink_to(APP,target_is_directory=True)
    subprocess.run([str(BB),'--install','-s','/opt/bin'],check=True)
    jq=Path('/opt/bin/jq')
    if not jq.exists():jq.symlink_to('/usr/bin/jq')
    suite=unittest.defaultTestLoader.loadTestsFromTestCase(RoutePolicy)
    for name in ['test_route_endpoint_rejects_old_client_and_retains_auth','test_general_http_cancel_and_stop_all_leave_route_running']:
        suite.addTest(RouteHTTP(name))
    result=unittest.TextTestRunner(verbosity=2,failfast=False).run(suite)
    Path('/opt/broray').unlink()
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
            'environment':'Linux native guard, synthetic owners, prefix CGI with real session validator','routerAccessed':False}
    (WORKSPACE/'docs/evidence/route-cancel-policy-tests.json').write_text(json.dumps(report,indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
