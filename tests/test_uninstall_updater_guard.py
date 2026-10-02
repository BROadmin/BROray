"""Uninstall admission must retain coordinator exclusion through worker handoff."""
import json,os,subprocess,tempfile,time,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
GUARD=ROOT/'.local/bin/linux-guard'
class UninstallGuard(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory(prefix='uninstall-guard-')
        self.base=Path(self.tmp.name);self.lock=self.base/'operations.guard'
    def tearDown(self):self.tmp.cleanup()
    def run_guard(self,*args,env=None):
        return subprocess.run([str(GUARD),str(self.lock),*args],capture_output=True,timeout=12,env=env)
    def run_scope(self,*args):
        return subprocess.run([str(GUARD),'--scope',str(self.lock),*args],capture_output=True,timeout=12)
    def test_nested_coordinator_retains_same_kernel_exclusion(self):
        p=self.run_scope('/bin/sh','-c','"$1" "$2" /bin/true; result=$?; echo nested=$result; exit "$result"','nested',str(GUARD),str(self.lock))
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertIn(b'nested=0',p.stdout)
    def test_distinct_route_resource_guard_is_acquired_under_global_scope(self):
        resource=self.base/'resource.control.guard'
        p=self.run_scope('/bin/ash','-c','"$1" "$2" "$1" --assert-held "$3"',
            'route',str(GUARD),str(resource),str(self.lock))
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertTrue(resource.is_file())
    def test_detached_worker_keeps_guard_after_request_returns(self):
        Path('/opt/bin').mkdir(parents=True,exist_ok=True)
        if not Path('/opt/bin/ash').exists():Path('/opt/bin/ash').symlink_to('/bin/ash')
        worker=self.base/'worker';ready=self.base/'ready';release=self.base/'release';done=self.base/'done'
        worker.write_text('''#!/bin/sh
"$1" --assert-held "$2" || exit 91
: >"$3"
while [ ! -f "$4" ]; do sleep 0.05; done
: >"$5"
''')
        code=ROOT/'implementation/runtime/app/lib/broray-page.sh'
        p=self.run_scope('/bin/ash','-c',f'. "{code}"; BRORAY_LOG="$7"; broray_system_spawn_worker "$1" "$2" "$3" "$4" "$5" "$6"',
             'parent',str(worker),str(GUARD),str(self.lock),str(ready),str(release),str(done),str(self.base/'worker.log'))
        try:
            self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
            for _ in range(100):
                if ready.exists():break
                time.sleep(0.02)
            self.assertTrue(ready.exists(),(self.base/'worker.log').read_text())
            self.assertEqual(self.run_guard('/bin/true').returncode,75)
        finally:
            release.touch()
            for _ in range(100):
                if done.exists():break
                time.sleep(0.02)
        self.assertTrue(done.exists())
        self.assertEqual(self.run_guard('/bin/true').returncode,0)
    def test_forged_inherited_fd_does_not_authorize(self):
        p=subprocess.run([str(GUARD),'--assert-held',str(self.lock)],env=os.environ|{'BRORAY_OPS_SCOPE_FD':'13'},capture_output=True)
        self.assertNotEqual(p.returncode,0)
    def test_wrong_path_and_symlink_rejected(self):
        script='ln -s "$2" "$3"; "$1" --assert-held "$3"'
        p=self.run_scope('/bin/sh','-c',script,'wrong',str(GUARD),str(self.lock),str(self.base/'alias'))
        self.assertNotEqual(p.returncode,0)

class UninstallAdmission(unittest.TestCase):
    def setUp(self):
        Path('/opt/bin').mkdir(parents=True,exist_ok=True)
        if not Path('/opt/bin/ash').exists():Path('/opt/bin/ash').symlink_to('/bin/ash')
        self.tmp=tempfile.TemporaryDirectory(prefix='uninstall-admission-')
        self.base=Path(self.tmp.name);self.app=self.base/'app';self.state=self.base/'state'
        self.state.mkdir(mode=0o700);(self.app/'run/broray').mkdir(parents=True)
        self.init=self.base/'init';self.init.mkdir();self.order=self.base/'order'
        self.env=os.environ|{'BRORAY_BASE':str(self.app),'BRORAY_STATE_ROOT':str(self.state),
            'BRORAY_TMP_ROOT':str(self.base),'BRORAY_INIT_ROOT':str(self.init),
            'BRORAY_OPS_GUARD':str(GUARD),'BRORAY_GLOBAL_LOCK':str(self.base/'legacy'),
            'BRORAY_COMPACT_GLOBAL_LOCK':str(self.base/'modern'),'BRORAY_COMPACT_REQUEST_LOCK':str(self.base/'request'),
            'ORDER':str(self.order)}
        (self.init/'S22broray-updater').write_text("""#!/bin/ash
case "$1" in
status) exit 0;;
stop)
 [ ! -e "$BRORAY_GLOBAL_LOCK" ] || { echo '{"ok":false,"errorCode":"LEGACY_OPERATION_BUSY"}';exit 75; }
 echo STOP >>"$ORDER"
 [ "${STOP_FAIL:-0}" = 0 ] || exit 75
 echo '{"ok":true,"phase":"SERVICE_STOP_COMPLETED","serviceStopped":true,"platformReady":false,"generationId":"g-fixture"}';;
start) echo RESTORE >>"$ORDER";;
esac
""")
        (self.init/'S22broray-updater').chmod(0o700)
    def tearDown(self):self.tmp.cleanup()
    def run_case(self,extra='',env=None):
        code=ROOT/'implementation/runtime/app/lib/broray-page.sh'
        script=f'. "{code}"\nbroray_system_start_worker() {{ echo ADMITTED >>"$ORDER"; return ${{ADMISSION_RC:-0}}; }}\n'+extra+'\nbroray_system_uninstall_start full "УДАЛИТЬ BROray ПОЛНОСТЬЮ"'
        return subprocess.run([str(GUARD),'--scope',str(self.state/'operations.guard'),'/bin/ash','-c',script],
            env=self.env|(env or {}),capture_output=True,timeout=15)
    def test_stop_precedes_uninstall_admission(self):
        p=self.run_case()
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertEqual(self.order.read_text(),'STOP\nADMITTED\n')
    def test_failed_stop_never_admits_uninstall(self):
        p=self.run_case(env={'STOP_FAIL':'1'})
        self.assertNotEqual(p.returncode,0)
        self.assertEqual(self.order.read_text(),'STOP\n')
    def test_failed_admission_restores_original_running_updater(self):
        p=self.run_case(env={'ADMISSION_RC':'1'})
        self.assertNotEqual(p.returncode,0)
        self.assertEqual(self.order.read_text(),'STOP\nADMITTED\nRESTORE\n')
    def test_existing_fence_prevents_even_updater_stop(self):
        (self.base/'legacy').mkdir();(self.base/'legacy/foreign').write_text('KEEP')
        p=self.run_case()
        self.assertNotEqual(p.returncode,0);self.assertFalse(self.order.exists())
        self.assertEqual((self.base/'legacy/foreign').read_text(),'KEEP')

    def test_worker_finish_restores_only_after_own_fence_is_released(self):
        code=ROOT/'implementation/runtime/app/lib/broray-page.sh'
        script=f'. "{code}"\n'+'''
mkdir "$BRORAY_GLOBAL_LOCK"
broray_system_global_lock_release() { rmdir "$BRORAY_GLOBAL_LOCK"; echo RELEASE >>"$ORDER"; }
broray_system_uninstall_updater_restore() {
  [ ! -e "$BRORAY_GLOBAL_LOCK" ] || return 75
  broray_system_uninstall_guard_assert || return 74
  echo RESTORE >>"$ORDER"
}
broray_system_worker_finish "$BRORAY_BASE/nonexistent-worker" "$BRORAY_BASE/nonexistent-lib"
'''
        p=subprocess.run([str(GUARD),'--scope',str(self.state/'operations.guard'),'/bin/ash','-c',script],
            env=self.env,capture_output=True,timeout=15)
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertEqual(self.order.read_text(),'RELEASE\nRESTORE\n')


    def install_pending_handoff(self):
        # Real admission and native kernel guard; settlement is an explicit
        # boundary double whose readiness becomes invalid as soon as stop runs.
        self.handoff=Path('/opt/var/lib/broray-platform-handoff')
        self.assertFalse(self.handoff.exists())
        self.handoff.mkdir(parents=True)
        (self.handoff/'phase').write_text('boot-pending\n')
        (self.app/'lib').mkdir()
        (self.app/'lib/universal-platform-handoff.sh').write_text("""#!/bin/ash
[ "$1" = settle ] || exit 93
"$BRORAY_OPS_GUARD" --assert-held "$BRORAY_STATE_ROOT/operations.guard" || exit 94
[ ! -f "$ORDER" ] || ! grep -q STOP "$ORDER" || exit 95
echo SETTLE >>"$ORDER"
[ "${SETTLE_FAIL:-0}" = 0 ] || exit 75
""")
        self.addCleanup(__import__('shutil').rmtree,self.handoff)

    def test_boot_settlement_precedes_updater_stop(self):
        self.install_pending_handoff()
        p=self.run_case()
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertEqual(self.order.read_text(),'SETTLE\nSTOP\nADMITTED\n')

    def test_failed_settlement_keeps_updater_running(self):
        self.install_pending_handoff()
        p=self.run_case(env={'SETTLE_FAIL':'1'})
        self.assertNotEqual(p.returncode,0)
        self.assertEqual(self.order.read_text(),'SETTLE\n')

    def test_existing_fence_prevents_settlement(self):
        self.install_pending_handoff()
        (self.base/'legacy').mkdir()
        p=self.run_case()
        self.assertNotEqual(p.returncode,0)
        self.assertFalse(self.order.exists())

if __name__=='__main__':unittest.main(verbosity=2)
