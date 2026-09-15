"""Actual Linux service identities and isolated init-script files."""
import ctypes,json,os,shutil,subprocess,tempfile,time,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
class Services(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='service-lifecycle-'))
        self.app=self.temp/'app';shutil.copytree(ROOT/'implementation/runtime/app',self.app)
        for directory in ['run','logs','tmp']:(self.app/directory).mkdir(exist_ok=True)
        self.state=self.temp/'state';self.svc=self.state/'services/subscriptions'
        self.env=os.environ|{'BRORAY_ROOT':str(self.app),'BRORAY_STATE_ROOT':str(self.state),
          'BRORAY_OPS_GUARD':str(ROOT/'.local/bin/linux-guard'),'BRORAY_OPS_ASH':'/bin/ash',
          'BRORAY_SUBSCRIPTION_PROC_ROOT':str(self.temp/'missing-proc'),
          'BRORAY_ROUTES_API_LOCK':str(self.temp/'global.lock'),'BRORAY_OPS_RAM_ROOT':str(self.temp/'ram')}
        self.children=[];ctypes.CDLL(None).prctl(36,1,0,0,0)
    def tearDown(self):
        if self.svc.exists():
            self.call('stop',expected=None,timeout=30)
        for child in self.children:
            if child.poll() is None:child.kill()
            child.communicate(timeout=10)
        # Detached daemon children were adopted by this test subreaper. They
        # received only generation-bound stop requests, never signals by PID.
        end=time.monotonic()+6
        children_absent=False
        while time.monotonic()<end:
            try:
                pid,_=os.waitpid(-1,os.WNOHANG)
                if not pid:time.sleep(.05)
            except ChildProcessError:children_absent=True;break
        self.assertTrue(children_absent,'Test child still live: preserve its fixture')
        assert self.temp.resolve().parent==Path('/tmp') and self.temp.name.startswith('service-lifecycle-')
        shutil.rmtree(self.temp)
    def call(self,action,expected=0,timeout=30):
        p=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-service'),'subscriptions',action],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        deadline=time.monotonic()+timeout
        while True:
            try:
                out,err=p.communicate(timeout=.1);break
            except subprocess.TimeoutExpired:
                self.reap_daemon()
                if time.monotonic()>deadline:p.kill();p.communicate();self.fail('service control timed out')
        p.stdout=out;p.stderr=err
        if expected is not None:self.assertEqual(p.returncode,expected,(p.stdout,p.stderr))
        return p
    def reap_daemon(self):
        try:pid=self.record()['owner']['pid']
        except (FileNotFoundError,KeyError,json.JSONDecodeError):return
        for child in self.children:
            if child.pid==pid:child.poll();return
        try:os.waitpid(pid,os.WNOHANG)
        except ChildProcessError:pass
    def record(self):return json.loads((self.svc/'identity.json').read_text())
    def direct(self):
        p=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-subscription-scheduler')],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.children.append(p);return p
    def wait_running(self,p=None):
        end=time.monotonic()+20
        while time.monotonic()<end:
            if p and p.poll() is not None:self.fail(p.communicate())
            try:
                record=self.record()
                if record['state']=='running':return record
            except FileNotFoundError:pass
            time.sleep(.1)
        self.fail('daemon did not publish ready identity')
    def test_status_preserves_unconfirmed_legacy_identity(self):
        pid=self.app/'run/subscription-scheduler.pid';birth=self.app/'run/subscription-scheduler.starttime'
        pid.write_text('99999999\n');birth.write_text('123456\n')
        p=subprocess.run(['/bin/ash',str(ROOT/'implementation/runtime/init/S28broray-subscriptions'),'status'],env=self.env,capture_output=True,timeout=15)
        self.assertNotEqual(p.returncode,0)
        self.assertTrue(pid.exists() and birth.exists(),'Read-only status erased unconfirmed legacy identity')
        self.assertEqual(pid.read_text(),'99999999\n');self.assertEqual(birth.read_text(),'123456\n')
    def test_empty_status_is_read_only(self):
        p=self.call('status-json');self.assertFalse(json.loads(p.stdout)['running'])
        self.assertFalse(self.state.exists())
    def test_status_rejects_symlinked_service_directory(self):
        foreign=self.temp/'foreign';foreign.mkdir();(foreign/'identity.json').write_text('PRIVATE_CANARY')
        self.svc.parent.mkdir(parents=True);self.svc.symlink_to(foreign)
        p=self.call('status-json');self.assertFalse(json.loads(p.stdout)['complete']);self.assertNotIn(b'PRIVATE_CANARY',p.stdout)
        self.assertEqual((foreign/'identity.json').read_text(),'PRIVATE_CANARY')
    def test_old_generation_stop_cannot_stop_current_daemon(self):
        p=self.direct();record=self.wait_running(p)
        stop=self.svc/'stop.json';stop.write_text(json.dumps({'schemaVersion':1,'generation':'0'*32}))
        self.assertNotEqual(record['generation'],'0'*32);time.sleep(2)
        self.assertIsNone(p.poll());self.assertTrue(json.loads(self.call('status-json').stdout)['ready'])
    def test_asynchronous_extra_wrapper_is_rejected_before_job(self):
        helper=self.app/'tmp/rejected-job.sh';helper.write_text('#!/bin/ash\necho BAD >"$BRORAY_ROOT/tmp/rejected-ran"\n')
        daemon=self.app/'bin/broray-subscription-scheduler'
        daemon.write_text('''#!/bin/ash
. "$BRORAY_ROOT/lib/service-lifecycle.sh"
broray_service_daemon_enter subscriptions || exit $?
broray_service_run_job "$BRORAY_ROOT/tmp/rejected-job.sh" & child=$!
wait "$child"; result=$?
broray_service_daemon_exit || exit 75
exit "$result"
''')
        p=self.direct();out,err=p.communicate(timeout=20);self.assertEqual(p.returncode,73,(out,err))
        self.assertFalse((self.app/'tmp/rejected-ran').exists())
    def test_real_scheduler_start_stop_and_generation(self):
        self.call('start');first=self.record();self.assertEqual(first['state'],'running')
        self.assertEqual(json.loads(self.call('status-json').stdout)['pid'],first['owner']['pid'])
        self.call('stop');self.assertFalse(json.loads(self.call('status-json').stdout)['running'])
        self.assertFalse((self.app/'run/subscription-scheduler.pid').exists())
        self.call('start');self.assertNotEqual(self.record()['generation'],first['generation'])
    def test_concurrent_starts_publish_one_daemon(self):
        argv=['/bin/ash',str(self.app/'bin/broray-service'),'subscriptions','start']
        jobs=[subprocess.Popen(argv,env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE) for _ in range(2)]
        self.children.extend(jobs)
        identities=[]
        for job in jobs:
            out,err=job.communicate(timeout=30);self.assertEqual(job.returncode,0,(out,err));identities.append(json.loads(out)['pid'])
        self.assertEqual(identities[0],identities[1]);self.assertEqual(self.record()['owner']['pid'],identities[0])
    def test_unknown_start_lock_is_preserved(self):
        lock=self.app/'run/subscription-scheduler.start.lock';lock.mkdir();(lock/'foreign').write_text('KEEP')
        self.call('start',expected=75);self.call('stop',expected=75)
        self.assertEqual((lock/'foreign').read_text(),'KEEP')
    def test_actual_daemon_crash_can_restart_without_pid_cleanup(self):
        p=self.direct();first=self.wait_running(p)
        p.kill();p.communicate(timeout=5)
        self.assertEqual(p.returncode,-9)
        self.call('stop')
        self.assertFalse((self.app/'run/subscription-scheduler.pid').exists())
        self.assertFalse((self.app/'run/subscription-scheduler.starttime').exists())
        self.assertEqual(self.record()['state'],'stopped')
        self.call('start');self.assertNotEqual(self.record()['generation'],first['generation'])
    def test_changed_executable_is_ambiguous_and_preserved(self):
        p=self.direct();record=self.wait_running(p)
        record['owner']['executable']='/foreign/unknown'
        identity=self.svc/'identity.json';identity.write_text(json.dumps(record));before=identity.read_bytes()
        self.call('stop',expected=75)
        view=json.loads(self.call('status-json').stdout);self.assertFalse(view['complete']);self.assertIsNone(view['running'])
        self.assertEqual(identity.read_bytes(),before);self.assertIsNone(p.poll())
        # Restore the exact known test identity so cooperative cleanup works.
        record['owner']['executable']=str(Path('/bin/ash').resolve());identity.write_text(json.dumps(record))
    def test_job_does_not_retain_daemon_lease(self):
        helper=self.app/'tmp/lease-child.sh';helper.write_text('#!/bin/ash\necho ready >"$BRORAY_ROOT/tmp/helper.ready"\nfor n in 1 2 3 4 5 6 7 8 9 10; do [ ! -f "$BRORAY_ROOT/tmp/helper.release" ] || break; sleep 1; done\necho done >"$BRORAY_ROOT/tmp/helper.done"\n')
        daemon=self.app/'bin/broray-subscription-scheduler'
        daemon.write_text('''#!/bin/ash
. "$BRORAY_ROOT/lib/service-lifecycle.sh"
broray_service_daemon_enter subscriptions || exit $?
broray_service_run_job "$BRORAY_ROOT/tmp/lease-child.sh"
''')
        p=self.direct()
        deadline=time.monotonic()+20
        while not (self.app/'tmp/helper.ready').exists():
            if p.poll() is not None:self.fail(p.communicate())
            if time.monotonic()>deadline:self.fail('helper never reached ready gate')
            time.sleep(.1)
        p.kill();p.wait(timeout=5);self.assertEqual(p.returncode,-9)
        self.assertTrue((self.app/'tmp/helper.ready').exists())
        try:
            probe=subprocess.run([str(ROOT/'.local/bin/linux-guard'),str(self.svc/'lifetime.guard'),'/bin/ash','-c',':'],capture_output=True,timeout=4)
            self.assertFalse((self.app/'tmp/helper.done').exists(),'Lease probe ran after helper exit')
            self.assertEqual(probe.returncode,0,probe.stderr)
        finally:
            (self.app/'tmp/helper.release').touch()
            p.communicate(timeout=15)
if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Services))
    (ROOT/'docs/evidence/service-lifecycle-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'isolated Linux service init; real processes where stated','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
