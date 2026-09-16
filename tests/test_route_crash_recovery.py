"""Crash a real protected route owner in a private Linux namespace."""
import ctypes,json,os,subprocess,time,unittest
from pathlib import Path
from test_route_entry import ROOT,RouteEntry


class RouteCrashRecovery(unittest.TestCase):
    setUp=RouteEntry.setUp
    tearDown=RouteEntry.tearDown

    def start_fixture(self,progress=False,exit_with_lease=False,exit_with_progress=False):
        body='''
    . "$BRORAY_ROOT/lib/routes-resource-lock.sh"
    broray_route_resource_acquire "$BRORAY_ROOT/routes/locks/operation.lock" export fixture || return $?
'''
        if progress:
            body+='''    . "$BRORAY_ROOT/lib/routes-operation-progress.sh"
    broray_routes_progress_begin fixture install 3 || return $?
    broray_routes_progress_update applying 1 3 || return $?
'''
        if exit_with_progress:
            body+='    broray_route_resource_release "$BRORAY_ROOT/routes/locks/operation.lock" "$BRORAY_ROUTE_RESOURCE_TOKEN" || return $?\n'
        body+='    echo READY >"$BRORAY_ROOT/ready"\n'
        body+=('    return 0\n' if exit_with_lease or exit_with_progress else '    while :; do sleep 1; done\n')
        (self.app/'lib/routes-export-build.sh').write_text('broray_routes_export_build_run() { :; }\n')
        (self.app/'lib/routes-router-sync.sh').write_text('broray_routes_sync_apply() {\n'+body+'}\n')
        trace=(self.app/'entry.log').open('wb');self.addCleanup(trace.close)
        p=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-routes'),'export','fixture'],env=self.env,
            stdout=trace,stderr=trace)
        deadline=time.monotonic()+180
        while not (self.app/'ready').exists():
            if p.poll() is not None:self.fail((p.returncode,(self.app/'entry.log').read_text()))
            if time.monotonic()>deadline:
                self.kill_owner_and_drain(p)
                self.fail('Entry readiness timeout: '+(self.app/'entry.log').read_text())
            time.sleep(.02)
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
            env=self.env,capture_output=True,timeout=45)

    def operation(self):
        files=list((self.state/'operations').glob('*/owner.json'));self.assertEqual(len(files),1)
        return files[0].parent

    def resource(self):return self.app/'routes/locks/operation.lock'

    def assert_blocked(self):
        result=self.recover();self.assertEqual(result.returncode,2,(result.stdout,result.stderr))
        self.assertTrue(self.lock.is_symlink());self.assertTrue(self.resource().is_dir())

    def live_identity(self):
        p=subprocess.run(['/bin/ash','-c','OPS_PROC=/proc; OPS_APP="$BRORAY_ROOT"; . "$BRORAY_ROOT/lib/operation-owner.sh"; broray_ops_capture_owner '+str(os.getpid())],env=self.env,capture_output=True,timeout=10)
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr));return json.loads(p.stdout)

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
        archive=self.app/'routes/locks'/('recovered-'+states[0]['operationId'])
        self.assertEqual(json.loads((archive/'owner.json').read_bytes()),record)
        again=self.recover();self.assertEqual(again.returncode,0,(again.stdout,again.stderr))

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
        plan=json.loads((self.operation()/'route-recovery-progress.json').read_bytes())
        self.assertEqual(plan['before'],before);self.assertEqual(plan['after'],after)
        # Existing route entry can run again after the interrupted projection.
        result=RouteEntry.run_cli(self);self.assertEqual(result.returncode,0,(result.stdout,result.stderr))

    wait_cli=RouteEntry.wait_cli

    def test_live_route_job_is_not_cancelled_or_recovered(self):
        p=self.start_fixture()
        try:
            self.assert_blocked()
            self.assertFalse((self.operation()/'cancel.json').exists())
            self.assertIsNone(p.poll())
        finally:self.kill_owner_and_drain(p)

    def test_foreign_job_binding_is_preserved(self):
        p=self.start_fixture();self.kill_owner_and_drain(p)
        path=self.resource()/'owner.json';record=json.loads(path.read_bytes())
        record['job']['jobTokenDigest']='0'*64;path.write_text(json.dumps(record))
        before=path.read_bytes();self.assert_blocked();self.assertEqual(path.read_bytes(),before)

    def test_unknown_hidden_resource_file_is_preserved(self):
        p=self.start_fixture();self.kill_owner_and_drain(p)
        path=self.resource()/'.foreign';path.write_text('KEEP')
        self.assert_blocked();self.assertEqual(path.read_text(),'KEEP')

    def test_live_resource_owner_is_preserved(self):
        p=self.start_fixture();self.kill_owner_and_drain(p)
        path=self.resource()/'owner.json';record=json.loads(path.read_bytes())
        record['owner']=self.live_identity();path.write_text(json.dumps(record))
        (self.resource()/'pid').write_text(str(os.getpid())+'\n')
        self.assert_blocked()

    def test_ambiguous_resource_owner_is_preserved(self):
        p=self.start_fixture();self.kill_owner_and_drain(p)
        path=self.resource()/'owner.json';record=json.loads(path.read_bytes())
        record['owner']=self.live_identity();record['owner']['commandDigest']='0'*64
        path.write_text(json.dumps(record));(self.resource()/'pid').write_text(str(os.getpid())+'\n')
        self.assert_blocked()

    def test_registered_live_helper_blocks_recovery(self):
        p=self.start_fixture();self.kill_owner_and_drain(p)
        path=self.operation()/'children.json';path.write_text(json.dumps({'children':[self.live_identity()]}))
        self.assert_blocked()

    def test_unbound_running_progress_is_preserved(self):
        p=self.start_fixture(progress=True);self.kill_owner_and_drain(p)
        path=self.app/'routes/operations/fixture.json';record=json.loads(path.read_bytes())
        record.pop('backgroundOperationId');path.write_text(json.dumps(record))
        before=path.read_bytes();self.assert_blocked();self.assertEqual(path.read_bytes(),before)

    def test_counter_and_resumable_progress_are_preserved(self):
        p=self.start_fixture(progress=True);self.kill_owner_and_drain(p)
        path=self.app/'routes/operations/fixture.json';record=json.loads(path.read_bytes())
        record.update(running=False,resumable=True,phase='paused',current=2)
        # A preflight/resume may still see a committed record from its predecessor.
        record['backgroundOperationId']='op-previous-generation'
        path.write_text(json.dumps(record));before=path.read_bytes()
        counter=self.app/'routes/operations/fixture.counter';counter.write_text('2\t192.0.2.0/24\n')
        result=self.recover();self.assertEqual(result.returncode,0,(result.stdout,result.stderr))
        self.assertEqual(path.read_bytes(),before);self.assertEqual(counter.read_text(),'2\t192.0.2.0/24\n')
        self.assertFalse((self.operation()/'route-recovery-progress.json').exists())

    def test_latest_counter_is_materialized_after_crash(self):
        p=self.start_fixture(progress=True);self.kill_owner_and_drain(p)
        (self.app/'routes/operations/fixture.counter').write_text('2\t192.0.2.0/24\n')
        result=self.recover();self.assertEqual(result.returncode,0,(result.stdout,result.stderr))
        self.assertEqual(json.loads((self.app/'routes/operations/fixture.json').read_bytes())['current'],2)

    def test_direct_recovery_helper_cannot_bypass_coordinator_guard(self):
        p=self.start_fixture();self.kill_owner_and_drain(p)
        result=subprocess.run([self.env['BRORAY_OPS_GUARD'],str(self.resource().parent/'resource.control.guard'),
          '/bin/ash',str(self.app/'lib/routes-resource-recover.sh'),str(self.resource()),self.operation().name,'retire'],env=self.env,capture_output=True,timeout=15)
        self.assertEqual(result.returncode,73,(result.stdout,result.stderr));self.assertTrue(self.resource().exists())

    def test_finish_retains_fence_when_backend_leaves_lease(self):
        p=self.start_fixture(exit_with_lease=True)
        result=self.wait_cli(p);self.assertEqual(result.returncode,75)
        self.assertTrue(self.lock.is_symlink());self.assertTrue(self.resource().is_dir())
        result=self.recover();self.assertEqual(result.returncode,0,(result.stdout,result.stderr))

    def test_finish_retains_fence_when_backend_leaves_running_progress(self):
        p=self.start_fixture(progress=True,exit_with_progress=True)
        result=self.wait_cli(p);self.assertEqual(result.returncode,75)
        self.assertTrue(self.lock.is_symlink());self.assertFalse(self.resource().exists())
        result=self.recover();self.assertEqual(result.returncode,0,(result.stdout,result.stderr))

    def test_interrupted_progress_publication_can_be_retried(self):
        p=self.start_fixture(progress=True);self.kill_owner_and_drain(p)
        # Simulate a filesystem refusing the progress replacement. Backup and
        # lease archive must survive, and the global fence cannot be released.
        guard=self.app/'bin/fault-guard'
        guard.write_text('''#!/bin/ash
if [ "$1" = --replace-file ] && [ "$3" = "$BRORAY_ROOT/routes/operations/fixture.json" ]; then exit 74; fi
exec "'''+self.env['BRORAY_OPS_GUARD']+'''" "$@"
''');guard.chmod(0o755)
        self.env['BRORAY_OPS_GUARD']=str(guard)
        result=self.recover();self.assertEqual(result.returncode,2,(result.stdout,result.stderr))
        self.assertTrue(self.lock.is_symlink());self.assertFalse(self.resource().exists())
        self.assertTrue((self.operation()/'route-recovery-progress.json').exists())
        self.env['BRORAY_OPS_GUARD']=str(ROOT/'.local/bin/linux-guard')
        result=self.recover();self.assertEqual(result.returncode,0,(result.stdout,result.stderr))
        self.assertFalse(self.lock.is_symlink())


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
