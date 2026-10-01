"""Field failures reproduced with Linux owners; no browser or router access."""
import json,time,unittest,subprocess
from test_startup_sidecars import Sidecar,ROOT
from test_supervisor import Supervisor

class ReconcilePreflight(Sidecar,unittest.TestCase):
    service='interface-reconcile'
    def setUp(self):
        super().setUp()
        from test_bounded_execution import install_bounded_helper
        install_bounded_helper(self.app)
        self.env['BRORAY_OPS_SUPERVISOR']=str(ROOT/'.local/bin/linux-supervisor')
    def test_invalid_interface_preflight_leaves_no_global_lock(self):
        self.fixture('fixture-interface','echo "$1" >>"$BRORAY_ROOT/tmp/interface.calls"\nexit 1\n')
        p=self.direct();out,err=p.communicate(timeout=30)
        self.assertNotEqual(p.returncode,0,(out,err))
        self.assertFalse((self.temp/'global.lock').is_symlink())
        self.assertNotIn('repair',(self.app/'tmp/interface.calls').read_text().splitlines())
    def recover(self):
        return subprocess.run(['/bin/ash','-c','. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call recover'],env=self.env,capture_output=True,timeout=30)
    def failed_mutation(self):
        self.fixture('fixture-interface','if [ "$1" = ownership-check ] && [ ! -e "$BRORAY_ROOT/tmp/mutated" ]; then exit 0; fi\n[ "$1" != repair ] || touch "$BRORAY_ROOT/tmp/mutated"\nexit 1\n')
        p=self.direct();out,err=p.communicate(timeout=40)
        self.assertEqual(p.returncode,75,(out,err))
        return next((self.state/'operations').glob('*/state.json')).parent
    def test_failed_mutation_recovers_only_after_configuration_is_confirmed(self):
        operation=self.failed_mutation()
        refused=self.recover()
        self.assertNotEqual(refused.returncode,0,refused.stdout)
        self.assertTrue((self.temp/'global.lock').is_symlink())
        self.fixture('fixture-interface','[ "$1" = ownership-check ] || exit 99\nexit 0\n')
        recovered=self.recover()
        self.assertEqual(recovered.returncode,0,(recovered.stdout,recovered.stderr))
        self.assertFalse((self.temp/'global.lock').is_symlink())
        self.assertEqual(json.loads((operation/'state.json').read_text())['state'],'recovered')
    def test_unsupervised_same_boot_is_not_blindly_unlocked(self):
        operation=self.failed_mutation()
        (operation/'interface-supervision.json').unlink()
        self.fixture('fixture-interface','exit 0\n')
        self.assertNotEqual(self.recover().returncode,0)
        self.assertTrue((self.temp/'global.lock').is_symlink())
    def test_pending_interface_transaction_is_preserved(self):
        self.failed_mutation()
        marker=self.app/'config/interface.json.operation';marker.parent.mkdir(exist_ok=True);marker.write_text('pending')
        self.fixture('fixture-interface','exit 0\n')
        self.assertNotEqual(self.recover().returncode,0)
        self.assertEqual(marker.read_text(),'pending')
        self.assertTrue((self.temp/'global.lock').is_symlink())
    def test_previous_boot_legacy_operation_requires_valid_configuration(self):
        operation=self.failed_mutation()
        (operation/'interface-supervision.json').unlink()
        for path in [operation/'owner.json',operation/'fence/owner.json']:
            owner=json.loads(path.read_text())
            owner['owner']['bootId']='00000000-0000-0000-0000-000000000001'
            path.write_text(json.dumps(owner)+'\n')
        self.assertNotEqual(self.recover().returncode,0)
        self.assertTrue((self.temp/'global.lock').is_symlink())
        self.fixture('fixture-interface','[ "$1" = ownership-check ] || exit 99\nexit 0\n')
        recovered=self.recover()
        self.assertEqual(recovered.returncode,0,(recovered.stdout,recovered.stderr))
        self.assertFalse((self.temp/'global.lock').is_symlink())
        self.assertEqual(json.loads((operation/'state.json').read_text())['state'],'recovered')

class SupervisorLatency(unittest.TestCase):
    setUp=Supervisor.setUp
    tearDown=Supervisor.tearDown
    launch=Supervisor.launch
    verify_gone=Supervisor.verify_gone
    def test_small_processes_do_not_pay_poll_sleep_per_event(self):
        start=time.monotonic()
        p=self.launch('i=0; while [ "$i" -lt 100 ]; do /bin/true; i=$((i+1)); done',timeout=30)
        out,err=p.communicate(timeout=35)
        elapsed=time.monotonic()-start
        self.assertEqual(p.returncode,0,(out,err))
        self.verify_gone()
        print('SUPERVISOR_100_PROCESSES_SECONDS='+str(round(elapsed,3)),flush=True)
        self.assertLess(elapsed,3.0,'ptrace event polling adds latency to every parser subprocess')

if __name__=='__main__':
    suite=unittest.TestSuite(unittest.defaultTestLoader.loadTestsFromTestCase(c) for c in [ReconcilePreflight,SupervisorLatency])
    r=unittest.TextTestRunner(verbosity=2).run(suite)
    (ROOT/'docs/evidence/field-followup-tests.json').write_text(json.dumps({'status':'PASS' if r.wasSuccessful() else 'FAIL','testsRun':r.testsRun,'routerAccessed':False,'webuiTested':False})+'\n')
    raise SystemExit(0 if r.wasSuccessful() else 1)
