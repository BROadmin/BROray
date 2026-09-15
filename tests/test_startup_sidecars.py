"""S24 sidecars with real Linux processes and harmless transport fixtures."""
import json,subprocess,time,unittest
from test_service_lifecycle import Services,ROOT

class Sidecar:
    tearDown=Services.tearDown
    reap_daemon=Services.reap_daemon
    record=Services.record
    wait_running=Services.wait_running
    def setUp(self):
        Services.setUp(self)
        self.svc=self.state/'services'/self.service
        self.env.update({'BRORAY_HOME_SNAPSHOT_REFRESH':str(self.app/'bin/fixture-refresh'),
          'BRORAY_LIGHTTPD_GUARD':str(self.app/'bin/fixture-ok'),'BRORAY_MONITOR_SERVICE':str(self.app/'bin/fixture-ok'),
          'BRORAY_RECONCILE_INTERFACE':str(self.app/'bin/fixture-interface'),
          'BRORAY_OPS_UPDATER_ROOT':str(self.temp/'updater'),'BRORAY_LEGACY_GLOBAL_LOCK':str(self.temp/'legacy.lock')})
        self.fixture('fixture-ok','exit 0\n')
        self.fixture('fixture-refresh','echo "$2" >>"$BRORAY_ROOT/tmp/refreshed"\n')
        self.fixture('fixture-interface','[ -L "$BRORAY_ROUTES_API_LOCK" ] || exit 99\necho "$1" >>"$BRORAY_ROOT/tmp/interface.calls"\n')
    def fixture(self,name,text):
        p=self.app/'bin'/name;p.write_text('#!/bin/ash\n'+text);p.chmod(0o755)
    def call(self,action,expected=0,timeout=35):
        p=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-service'),self.service,action],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.children.append(p);deadline=time.monotonic()+timeout
        while True:
            try:out,err=p.communicate(timeout=.1);break
            except subprocess.TimeoutExpired:
                self.reap_daemon()
                if time.monotonic()>deadline:p.kill();p.communicate();self.fail('sidecar control timed out')
        if expected is not None:self.assertEqual(p.returncode,expected,(out,err))
        return subprocess.CompletedProcess(p.args,p.returncode,out,err)
    def direct(self):
        name='broray-home-snapshotd' if self.service=='home-snapshot' else 'broray-interface-reconcile'
        p=subprocess.Popen(['/bin/ash',str(self.app/'bin'/name)],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.children.append(p);return p
    def wait_file(self,name,timeout=20):
        end=time.monotonic()+timeout
        while not (self.app/'tmp'/name).exists():
            self.assertLess(time.monotonic(),end,name);time.sleep(.1)
    def test_unknown_legacy_pid_is_preserved(self):
        name='home-snapshotd.pid' if self.service=='home-snapshot' else 'interface-reconcile.pid'
        pid=self.app/'run'/name;pid.write_text('99999999\n')
        for action in ['stop','restart']:
            self.call(action,expected=75);self.assertEqual(pid.read_text(),'99999999\n')
        self.assertFalse(json.loads(self.call('status-json').stdout)['complete'])
        pid.unlink()
    def test_empty_status_is_read_only(self):
        data=json.loads(self.call('status-json').stdout)
        self.assertTrue(data['complete']);self.assertFalse(data['running']);self.assertFalse(self.state.exists())

class HomeSnapshot(Sidecar,unittest.TestCase):
    service='home-snapshot'
    def test_actual_cycle_and_cooperative_stop(self):
        self.call('start');self.wait_file('refreshed');self.call('stop')
        self.assertFalse((self.app/'run/home-snapshotd.pid').exists())
        self.assertEqual(self.record()['state'],'stopped')
    def test_old_generation_cannot_stop_current_daemon(self):
        p=self.direct();record=self.wait_running(p)
        (self.svc/'stop.json').write_text(json.dumps({'schemaVersion':1,'generation':'0'*32}))
        self.assertNotEqual(record['generation'],'0'*32);time.sleep(2)
        self.assertIsNone(p.poll());self.assertTrue(json.loads(self.call('status-json').stdout)['ready'])
    def test_busy_snapshot_reports_pending_until_completion(self):
        self.fixture('fixture-refresh','echo ready >"$BRORAY_ROOT/tmp/busy.ready"\nfor n in $(seq 1 50); do [ ! -e "$BRORAY_ROOT/tmp/busy.release" ] || exit 0; sleep 1; done\nexit 1\n')
        p=self.direct();self.wait_running(p);self.wait_file('busy.ready')
        try:
            self.call('stop',expected=75)
            data=json.loads(self.call('status-json').stdout)
            self.assertTrue(data['running']);self.assertEqual(data['state'],'stopping');self.assertIsNone(p.poll())
        finally:(self.app/'tmp/busy.release').touch()
        self.call('stop');p.communicate(timeout=10);self.assertEqual(p.returncode,0)
    def test_stop_preserves_unrelated_persistent_process(self):
        canary=subprocess.Popen(['/bin/sleep','60']);self.children.append(canary)
        self.call('start');self.call('stop');self.assertIsNone(canary.poll())
    def test_concurrent_start_has_one_generation(self):
        callers=[subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-service'),self.service,'start'],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE) for _ in range(2)]
        self.children.extend(callers);views=[]
        for p in callers:
            out,err=p.communicate(timeout=30);self.assertEqual(p.returncode,0,(out,err));views.append(json.loads(out))
        self.assertEqual(views[0]['pid'],views[1]['pid'])

class InterfaceReconcile(Sidecar,unittest.TestCase):
    service='interface-reconcile'
    def test_reconcile_completes_under_protected_admission(self):
        p=self.direct();out,err=p.communicate(timeout=25);self.assertEqual(p.returncode,0,(out,err))
        calls=(self.app/'tmp/interface.calls').read_text().splitlines()
        self.assertEqual(calls,['check','sync-name','check'])
        self.assertFalse((self.temp/'global.lock').exists())
        self.assertEqual(self.record()['state'],'stopped')
    def test_stop_before_first_attempt_does_not_mutate(self):
        p=self.direct();self.wait_running(p);self.call('stop');p.communicate(timeout=10)
        self.assertFalse((self.app/'tmp/interface.calls').exists());self.assertEqual(p.returncode,0)
    def test_pause_defers_network_mutation(self):
        self.state.mkdir();(self.state/'background-automation.json').write_text('{"paused":true}\n')
        p=self.direct();self.wait_running(p);time.sleep(5)
        self.assertFalse((self.app/'tmp/interface.calls').exists());self.assertIsNone(p.poll())
        self.call('stop');p.communicate(timeout=10)
        self.assertIn('state=deferred',(self.app/'run/interface-reconcile.status').read_text())
    def test_failed_mutation_preserves_protected_fence(self):
        self.fixture('fixture-interface','echo "$1" >>"$BRORAY_ROOT/tmp/interface.calls"\nexit 1\n')
        p=self.direct();out,err=p.communicate(timeout=25);self.assertEqual(p.returncode,75,(out,err))
        self.assertTrue((self.temp/'global.lock').is_symlink())
        self.assertIn('recovery-required',(self.app/'run/interface-reconcile.status').read_text())
        self.call('stop');self.assertTrue((self.temp/'global.lock').is_symlink())

if __name__=='__main__':
    suite=unittest.TestSuite(unittest.defaultTestLoader.loadTestsFromTestCase(cls) for cls in [HomeSnapshot,InterfaceReconcile])
    r=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
    (ROOT/'docs/evidence/startup-sidecars-tests.json').write_text(json.dumps({'status':'PASS' if r.wasSuccessful() else 'FAIL','testsRun':r.testsRun,'environment':'real Linux sidecar processes, private files and transport fixtures','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if r.wasSuccessful() else 1)
