"""Crash a real protected route owner in a private Linux namespace."""
import ctypes,json,os,subprocess,time,unittest
from pathlib import Path
from test_route_entry import ROOT,RouteEntry


class RouteCrashRecovery(unittest.TestCase):
    setUp=RouteEntry.setUp
    tearDown=RouteEntry.tearDown

    def start_fixture(self,progress=False):
        body='''
    . "$BRORAY_ROOT/lib/routes-resource-lock.sh"
    broray_route_resource_acquire "$BRORAY_ROOT/routes/locks/operation.lock" export fixture || return $?
'''
        if progress:
            body+='''    . "$BRORAY_ROOT/lib/routes-operation-progress.sh"
    broray_routes_progress_begin fixture install 3 || return $?
    broray_routes_progress_update applying 1 3 || return $?
'''
        body+='''    echo READY >"$BRORAY_ROOT/ready"
    while :; do sleep 1; done
'''
        (self.app/'lib/routes-export-build.sh').write_text('broray_routes_export_build_run() { :; }\n')
        (self.app/'lib/routes-router-sync.sh').write_text('broray_routes_sync_apply() {\n'+body+'}\n')
        p=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-routes'),'export','fixture'],env=self.env,
            stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        deadline=time.monotonic()+40
        while not (self.app/'ready').exists():
            self.assertIsNone(p.poll());self.assertLess(time.monotonic(),deadline);time.sleep(.02)
        return p

    def kill_owner_and_drain(self,p):
        # Popen is our own unreaped direct child, never a PID read from state.
        p.kill();p.wait(timeout=5)
        deadline=time.monotonic()+30
        while True:
            alive=[]
            for file in (self.temp/'ram').rglob('children.json'):
                record=json.loads(file.read_bytes())
                for child in record.get('children',[]):
                    try:os.waitpid(child['pid'],os.WNOHANG)
                    except ChildProcessError:pass
                    if Path('/proc',str(child['pid'])).exists():alive.append(child['pid'])
                try:os.waitpid(record['supervisorPid'],os.WNOHANG)
                except ChildProcessError:pass
                if Path('/proc',str(record['supervisorPid'])).exists():alive.append(record['supervisorPid'])
            if not alive:return
            self.assertLess(time.monotonic(),deadline,alive);time.sleep(.05)

    def recover(self):
        return subprocess.run(['/bin/ash','-c','. "$BRORAY_ROOT/lib/operation-client.sh"\nbroray_ops_call recover\n'],
            env=self.env,capture_output=True,timeout=20)

    def test_dead_proven_job_retires_only_its_bound_resource_and_global_fence(self):
        p=self.start_fixture()
        self.kill_owner_and_drain(p)
        record=json.loads((self.app/'routes/locks/operation.lock/owner.json').read_bytes())
        self.assertIn('job',record)
        result=self.recover();self.assertEqual(result.returncode,0,(result.stdout,result.stderr))
        self.assertFalse((self.app/'routes/locks/operation.lock').exists())
        self.assertFalse(self.lock.exists());self.assertFalse(self.lock.is_symlink())
        states=[json.loads(f.read_bytes()) for f in (self.state/'operations').glob('*/state.json')]
        self.assertEqual(len(states),1);self.assertEqual(states[0]['state'],'recovered')

    def test_crashed_progress_remains_available_for_existing_restore(self):
        p=self.start_fixture(progress=True)
        self.kill_owner_and_drain(p)
        file=self.app/'routes/operations/fixture.json';before=json.loads(file.read_bytes())
        result=self.recover();self.assertEqual(result.returncode,0,(result.stdout,result.stderr))
        after=json.loads(file.read_bytes())
        self.assertEqual(after['bundleId'],'fixture');self.assertEqual(after['operation'],before['operation'])
        self.assertEqual(after['current'],1);self.assertEqual(after['total'],3)
        self.assertFalse(after['running']);self.assertFalse(after['success'])
        self.assertFalse(after['resumable'],'A crash must not invent successful partial commit/resume metadata')
        self.assertEqual(after['phase'],'interrupted')
        self.assertFalse(self.lock.exists());self.assertFalse(self.lock.is_symlink())


if __name__=='__main__':
    if os.name=='nt':raise SystemExit('Run in isolated Linux guest')
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    Path('/opt/bin').mkdir(parents=True,exist_ok=True)
    subprocess.run(['/bin/busybox','--install','-s','/opt/bin'],check=True)
    if not Path('/opt/bin/jq').exists():Path('/opt/bin/jq').symlink_to('/usr/bin/jq')
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RouteCrashRecovery))
    (ROOT/'docs/evidence/route-crash-recovery-tests.json').write_text(json.dumps({
        'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
        'environment':'Real Linux protected job/lease/progress, direct-child crash, harmless route backend',
        'routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
