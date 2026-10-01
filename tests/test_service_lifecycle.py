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
    def readiness_daemon(self, gate):
        daemon=self.app/'bin/broray-subscription-scheduler'
        daemon.write_text('''#!/bin/ash
. "$BRORAY_ROOT/lib/service-lifecycle.sh"
if [ "${BRORAY_SERVICE_BOOTSTRAP:-}" = subscriptions ]; then
'''+gate+'''
fi
broray_service_daemon_enter subscriptions || exit $?
trap 'broray_service_daemon_exit' EXIT
while ! broray_service_stop_requested; do /bin/sleep 1; done
''')
    def test_start_observes_ready_after_last_sleep(self):
        self.assert_ready_at_sleep(10)
    def test_start_observes_ready_at_extended_wait_boundary(self):
        self.assert_ready_at_sleep(30)
    def assert_ready_at_sleep(self, boundary):
        self.env['TEST_READY_SLEEP']=str(boundary)
        self.readiness_daemon('read -r release <"$BRORAY_ROOT/tmp/birth-gate"')
        os.mkfifo(self.app/'tmp/birth-gate')
        # Deterministic interleaving: the final old wait publishes a real,
        # identity-bound daemon before returning to the start controller.
        sleeper=self.app/'bin/sleep'
        sleeper.write_text('''#!/bin/ash
n=0; [ ! -f "$BRORAY_ROOT/tmp/polls" ] || read -r n <"$BRORAY_ROOT/tmp/polls"
n=$((n+1)); echo "$n" >"$BRORAY_ROOT/tmp/polls"
if [ "$n" = "$TEST_READY_SLEEP" ]; then
 echo release >"$BRORAY_ROOT/tmp/birth-gate"
 for i in $(seq 1 100); do
  jq -e '.state=="running"' "$BRORAY_STATE_ROOT/services/subscriptions/identity.json" >/dev/null 2>&1 && exit 0
  /bin/sleep .05
 done
 exit 91
fi
''');sleeper.chmod(0o755)
        result=self.call('start',expected=None)
        self.assertEqual(self.record()['state'],'running')
        status=json.loads(self.call('status-json').stdout)
        self.assertTrue(status['complete'] and status['ready'],status)
        self.assertEqual(result.returncode,0,result.stdout)
        self.assertTrue(json.loads(result.stdout)['ready'])
    def test_start_timeout_never_claims_ready_or_relaunches(self):
        daemon=self.app/'bin/broray-subscription-scheduler'
        daemon.write_text('#!/bin/ash\necho launch >>"$BRORAY_ROOT/tmp/launches"\nexit 0\n')
        sleeper=self.app/'bin/sleep'
        sleeper.write_text('#!/bin/ash\necho wait >>"$BRORAY_ROOT/tmp/waits"\n')
        sleeper.chmod(0o755)
        result=self.call('start',expected=75)
        self.assertFalse(json.loads(result.stdout)['ready'])
        self.assertEqual((self.app/'tmp/launches').read_text().splitlines(),['launch'])
        self.assertEqual(len((self.app/'tmp/waits').read_text().splitlines()),30)
    def test_start_accepts_slow_identity_bound_adoption(self):
        # Physical KN-2710 evidence: adoption took12-14s during release switch.
        self.readiness_daemon('/bin/sleep 12')
        result=self.call('start',expected=None)
        self.wait_running()
        self.assertEqual(result.returncode,0,result.stdout)
        self.assertTrue(json.loads(result.stdout)['ready'])
    def test_stop_waits_for_cooperative_foreground_job(self):
        # KN-2710/1101routes: the exact home-snapshot generation needed17s
        # after its stop request to finish the current foreground refresh.
        daemon=self.app/'bin/broray-subscription-scheduler'
        daemon.write_text('''#!/bin/ash
. "$BRORAY_ROOT/lib/service-lifecycle.sh"
broray_service_daemon_enter subscriptions || exit $?
trap 'broray_service_daemon_exit' EXIT
while ! broray_service_stop_requested; do /bin/sleep 1; done
/bin/sleep 17
''')
        p=self.direct();owner=self.wait_running(p)
        started=time.monotonic();result=self.call('stop',expected=None)
        elapsed=time.monotonic()-started
        # Drain the same owned fixture before asserting the original failure.
        # There is no second stop request or signal/restart in this test.
        p.communicate(timeout=25)
        self.assertEqual(result.returncode,0,(result.stdout,result.stderr))
        self.assertFalse(json.loads(result.stdout)['running'])
        self.assertEqual(self.record()['generation'],owner['generation'])
        self.assertEqual(self.record()['state'],'stopped')
        self.assertLess(elapsed,28,'Do not wait out the maximum after completion')
    def test_stop_accepts_completion_after_twenty_observations(self):
        self.assert_stop_after_observations(20)
    def test_stop_rechecks_after_final_observation(self):
        self.assert_stop_after_observations(120)
    def test_stop_timeout_preserves_unfinished_generation(self):
        self.assert_stop_after_observations(121)
    def assert_stop_after_observations(self,boundary):
        gate=self.app/'tmp/foreground-gate';os.mkfifo(gate)
        daemon=self.app/'bin/broray-subscription-scheduler'
        daemon.write_text('''#!/bin/ash
. "$BRORAY_ROOT/lib/service-lifecycle.sh"
broray_service_daemon_enter subscriptions || exit $?
trap 'broray_service_daemon_exit' EXIT
while ! broray_service_stop_requested; do /bin/sleep 1; done
read -r completion <"$BRORAY_ROOT/tmp/foreground-gate"
''')
        sleeper=self.app/'bin/sleep'
        sleeper.write_text('''#!/bin/ash
echo wait >>"$BRORAY_ROOT/tmp/stop-waits"
if [ "$(wc -l <"$BRORAY_ROOT/tmp/stop-waits")" = "$TEST_STOP_BOUNDARY" ]; then
 echo complete >"$BRORAY_ROOT/tmp/foreground-gate"
 echo released >"$BRORAY_ROOT/tmp/foreground-released"
 for attempt in 1 2 3 4 5 6 7 8 9 10; do
  jq -e '.state=="stopped"' "$BRORAY_STATE_ROOT/services/subscriptions/identity.json" >/dev/null && exit 0
  /bin/sleep 1
 done
 exit 1
fi
''');sleeper.chmod(0o755);self.env['TEST_STOP_BOUNDARY']=str(boundary)
        p=self.direct();owner=self.wait_running(p)
        try:
            #120 real identity probes are slower under QEMU than on Keenetic;
            # this outer fixture budget must outlast the tested controller.
            result=self.call('stop',expected=None,timeout=300)
            at_return=self.record();waits=(self.app/'tmp/stop-waits').read_text().splitlines()
        finally:
            if not (self.app/'tmp/foreground-released').exists():
                with gate.open('w') as f:f.write('fixture cleanup\n')
            p.communicate(timeout=15)
        if boundary==121:
            self.assertEqual(result.returncode,75,(result.stdout,result.stderr))
            self.assertTrue(json.loads(result.stdout)['running'])
            self.assertEqual(at_return['state'],'running')
            self.assertEqual(at_return['generation'],owner['generation'])
            self.assertEqual(len(waits),120)
            return
        self.assertEqual(result.returncode,0,(result.stdout,result.stderr))
        self.assertFalse(json.loads(result.stdout)['running'])
        self.assertEqual(at_return['state'],'stopped')
        self.assertEqual(at_return['generation'],owner['generation'])
        self.assertEqual(len(waits),boundary,'Return after verified completion without waiting the remaining maximum')
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
    def stale_mismatch(self):
        p=self.direct();record=self.wait_running(p)
        self.call('stop');p.communicate(timeout=10)
        record['owner']['bootId']='previous-boot';record['state']='running'
        (self.svc/'identity.json').write_text(json.dumps(record))
        (self.app/'run/subscription-scheduler.pid').write_text('99999999\n')
        (self.app/'run/subscription-scheduler.starttime').write_text('42\n')
        return {p:p.read_bytes() for p in [self.svc/'identity.json',self.app/'run/subscription-scheduler.pid',self.app/'run/subscription-scheduler.starttime']}
    def test_ambiguous_projection_requires_boot_boundary(self):
        before=self.stale_mismatch()
        self.call('recover',expected=75)
        receipt=self.svc/('recovery-'+self.record()['generation']+'.json');self.assertTrue(receipt.exists())
        evidence=receipt.read_bytes();self.call('recover',expected=75)
        self.assertEqual(receipt.read_bytes(),evidence)
        self.assertTrue(all(p.read_bytes()==b for p,b in before.items()))
        status=json.loads(self.call('status-json').stdout)
        self.assertEqual(status['errorCode'],'SERVICE_RECOVERY_REBOOT_REQUIRED')
    def test_recovery_rejects_changed_projection_after_boot(self):
        before=self.stale_mismatch();self.call('recover',expected=75)
        receipt=self.svc/('recovery-'+self.record()['generation']+'.json');record=json.loads(receipt.read_bytes())
        record['bootId']='simulated-previous-boot';receipt.write_text(json.dumps(record))
        pid=self.app/'run/subscription-scheduler.pid';pid.write_text('87654321\n')
        self.call('recover',expected=75);self.assertEqual(pid.read_text(),'87654321\n')
        self.assertEqual((self.svc/'identity.json').read_bytes(),before[self.svc/'identity.json'])
    def test_recovery_after_boot_preserves_evidence_and_can_start(self):
        before=self.stale_mismatch();self.call('recover',expected=75)
        receipt=self.svc/('recovery-'+self.record()['generation']+'.json');record=json.loads(receipt.read_bytes())
        record['bootId']='simulated-previous-boot';receipt.write_text(json.dumps(record))
        self.call('recover');self.assertEqual(self.record()['state'],'stopped')
        self.assertFalse((self.app/'run/subscription-scheduler.pid').exists())
        self.assertFalse((self.app/'run/subscription-scheduler.starttime').exists())
        evidence=receipt.read_bytes();self.call('recover');self.assertEqual(receipt.read_bytes(),evidence)
        self.call('start');self.assertTrue(json.loads(self.call('status-json').stdout)['ready'])
        self.assertTrue(receipt.exists(),'Recovery evidence must survive successful startup')
    def test_recovery_refuses_live_generation_without_mutation(self):
        p=self.direct();self.wait_running(p)
        identity=self.svc/'identity.json';before=identity.read_bytes()
        self.call('recover',expected=75,timeout=20)
        self.assertIsNone(p.poll());self.assertEqual(identity.read_bytes(),before)
        self.assertFalse(list(self.svc.glob('recovery-*.json')))
    def test_recovery_keeps_corrupt_receipt(self):
        before=self.stale_mismatch();self.call('recover',expected=75)
        receipt=self.svc/('recovery-'+self.record()['generation']+'.json');receipt.write_bytes(b'{broken')
        self.call('recover',expected=75)
        self.assertEqual(receipt.read_bytes(),b'{broken')
        self.assertTrue(all(p.read_bytes()==b for p,b in before.items()))
    def test_recovery_valid_json_wrong_target_hash_preserves_all_files(self):
        before=self.stale_mismatch();self.call('recover',expected=75)
        receipt=self.svc/('recovery-'+self.record()['generation']+'.json');record=json.loads(receipt.read_bytes())
        record['bootId']='simulated-previous-boot';record['stoppedSha256']='0'*64
        receipt.write_text(json.dumps(record));evidence=receipt.read_bytes()
        self.call('recover',expected=75)
        self.assertEqual(receipt.read_bytes(),evidence)
        self.assertTrue(all(p.exists() and p.read_bytes()==b for p,b in before.items()))
    def test_recovery_resumes_after_one_projection_retired(self):
        before=self.stale_mismatch();self.call('recover',expected=75)
        receipt=self.svc/('recovery-'+self.record()['generation']+'.json');record=json.loads(receipt.read_bytes())
        import base64
        self.assertEqual(base64.b64decode(record['identityBytes']),before[self.svc/'identity.json'])
        record['bootId']='simulated-previous-boot';receipt.write_text(json.dumps(record))
        (self.app/'run/subscription-scheduler.pid').unlink()
        self.call('recover');self.call('start')
        self.assertTrue(json.loads(self.call('status-json').stdout)['ready'])
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
