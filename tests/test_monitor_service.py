"""Real connection monitor lifecycle, private files and harmless address source."""
import ctypes,json,os,subprocess,time,unittest
from test_service_lifecycle import Services,ROOT
class MonitorService(unittest.TestCase):
    tearDown=Services.tearDown
    reap_daemon=Services.reap_daemon
    record=Services.record
    wait_running=Services.wait_running
    def setUp(self):
        Services.setUp(self)
        self.svc=self.state/'services/connection-monitor'
        self.env.update({'BRORAY_MONITOR_ROOT':str(self.app),'BRORAY_MONITOR_INTERVAL':'1',
          'BRORAY_MONITOR_PATH':str(self.app/'bin')+':/usr/bin:/bin',
          'BRORAY_MONITOR_BRORAY':str(self.app/'bin/fixture-current-address'),
          'BRORAY_MONITOR_MAINTENANCE':str(self.app/'bin/fixture-maintenance')})
        for name in ['fixture-current-address','fixture-maintenance']:
            path=self.app/'bin'/name;path.write_text('#!/bin/ash\nexit 0\n');path.chmod(0o755)
    def call(self,action,expected=0,timeout=35):
        p=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-service'),'connection-monitor',action],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.children.append(p);deadline=time.monotonic()+timeout
        while True:
            try:out,err=p.communicate(timeout=.1);break
            except subprocess.TimeoutExpired:
                self.reap_daemon()
                if time.monotonic()>deadline:p.kill();p.communicate();self.fail('monitor control timed out')
        if expected is not None:self.assertEqual(p.returncode,expected,(out,err))
        return subprocess.CompletedProcess(p.args,p.returncode,out,err)
    def direct(self):
        p=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-connection-monitor')],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.children.append(p);return p
    def test_empty_status_does_not_create_state(self):
        data=json.loads(self.call('status-json').stdout)
        self.assertTrue(data['complete']);self.assertFalse(data['running']);self.assertFalse(self.state.exists())
    def test_unknown_legacy_file_is_preserved_by_stop_and_restart(self):
        pid=self.app/'run/connection-monitor.pid';pid.write_text('99999999\n')
        for action in ['status-json','stop','restart']:
            self.call(action,expected=0 if action=='status-json' else 75)
            self.assertEqual(pid.read_text(),'99999999\n')
            self.assertFalse(json.loads(self.call('status-json').stdout)['complete'])
        self.assertFalse((self.svc/'identity.json').exists())
        # Exact object created above, no daemon was admitted.
        pid.unlink()
    def test_real_monitor_start_stop_publishes_status(self):
        self.call('start');self.wait_running()
        until=time.monotonic()+10
        while not (self.app/'run/connection-status.json').exists():
            self.assertLess(time.monotonic(),until);time.sleep(.1)
        data=json.loads((self.app/'run/connection-status.json').read_text())
        self.assertFalse(data['available']);self.assertEqual(data['address'],'')
        self.call('stop');self.assertFalse((self.app/'run/connection-monitor.pid').exists())
    def test_stale_generation_stop_cannot_stop_current_monitor(self):
        p=self.direct();record=self.wait_running(p)
        (self.svc/'stop.json').write_text(json.dumps({'schemaVersion':1,'generation':'0'*32}))
        self.assertNotEqual(record['generation'],'0'*32);time.sleep(2)
        self.assertIsNone(p.poll());self.assertTrue(json.loads(self.call('status-json').stdout)['ready'])
    def test_busy_cycle_returns_pending_then_stops_without_signals(self):
        (self.app/'bin/fixture-maintenance').write_text('''#!/bin/ash
echo ready >"$BRORAY_ROOT/tmp/maintenance.ready"
for n in $(seq 1 50); do
  [ ! -e "$BRORAY_ROOT/tmp/maintenance.release" ] || exit 0
  sleep 1
done
exit 1
''')
        p=self.direct();self.wait_running(p);until=time.monotonic()+10
        try:
            while not (self.app/'tmp/maintenance.ready').exists():
                self.assertLess(time.monotonic(),until);time.sleep(.1)
            self.call('stop',expected=75)
            data=json.loads(self.call('status-json').stdout)
            self.assertTrue(data['running']);self.assertEqual(data['state'],'stopping')
            self.assertIsNone(p.poll())
        finally:(self.app/'tmp/maintenance.release').touch()
        self.call('stop');p.communicate(timeout=10);self.assertEqual(p.returncode,0)
    def test_stop_leaves_unrelated_persistent_process_alive(self):
        canary=subprocess.Popen(['/bin/sleep','60']);self.children.append(canary)
        self.call('start');self.call('stop');self.assertIsNone(canary.poll())
    def test_concurrent_starts_share_one_generation(self):
        argv=['/bin/ash',str(self.app/'bin/broray-service'),'connection-monitor','start']
        callers=[subprocess.Popen(argv,env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE) for _ in range(2)]
        self.children.extend(callers);responses=[]
        for p in callers:
            out,err=p.communicate(timeout=30);self.assertEqual(p.returncode,0,(out,err));responses.append(json.loads(out))
        self.assertEqual(responses[0]['pid'],responses[1]['pid']);self.assertTrue(responses[0]['ready'])
    def test_confirmed_dead_monitor_retires_before_restart(self):
        p=self.direct();first=self.wait_running(p)
        p.kill();p.communicate(timeout=10)
        time.sleep(2)
        self.call('stop');self.call('start');second=self.record()
        self.assertNotEqual(first['generation'],second['generation'])
if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(MonitorService))
    (ROOT/'docs/evidence/monitor-service-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'actual Linux monitor and service processes, private files, harmless transport','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
