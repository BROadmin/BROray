"""Startup recovery must recognize a prior kernel boot without changing pause."""
import json,shutil,unittest
from test_operations import Operations,WORKSPACE
class BootRecovery(unittest.TestCase):
    def setUp(self):self.ops=Operations('runTest');self.ops.setUp()
    def tearDown(self):shutil.rmtree(self.ops.temp)
    def test_previous_boot_cooperative_fence_recovers_during_initialization(self):
        a=self.ops.begin();self.ops.call('pause')
        (self.ops.proc/'sys/kernel/random/boot_id').write_text('boot-two\n')
        self.ops.call('initialize')
        s=self.ops.call('status');self.assertEqual(s['globalFence'],'absent')
        self.assertTrue(s['automationPaused'])
        self.assertEqual(json.loads(self.ops.opfile(a,'state.json').read_bytes())['state'],'recovered')
    def test_same_boot_dead_owner_is_left_for_explicit_recovery(self):
        self.ops.begin();self.ops.set_owner({'status':'absent'})
        self.ops.call('initialize');self.assertEqual(self.ops.call('status')['globalFence'],'managed_stale')
    def test_prior_boot_protected_commit_stays_fenced(self):
        self.ops.begin('protected');(self.ops.proc/'sys/kernel/random/boot_id').write_text('boot-two\n')
        self.ops.call('initialize');self.assertEqual(self.ops.call('status')['globalFence'],'managed_stale')
    def test_updater_fence_blocks_startup_recovery(self):
        self.ops.begin();(self.ops.proc/'sys/kernel/random/boot_id').write_text('boot-two\n')
        (self.ops.temp/'updater/request.lock').mkdir(parents=True)
        self.ops.call('initialize');self.assertEqual(self.ops.call('status')['globalFence'],'managed_stale')
    def test_missing_boot_preserves_fence(self):
        self.ops.begin();(self.ops.proc/'sys/kernel/random/boot_id').unlink()
        self.ops.call('initialize');self.assertEqual(self.ops.call('status')['globalFence'],'ambiguous')
if __name__=='__main__':
    r=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(BootRecovery))
    (WORKSPACE/'docs/evidence/boot-recovery-tests.json').write_text(json.dumps({'status':'PASS' if r.wasSuccessful() else 'FAIL','testsRun':r.testsRun,'syntheticBoot':True,'routerAccessed':False})+'\n')
    raise SystemExit(0 if r.wasSuccessful() else 1)
