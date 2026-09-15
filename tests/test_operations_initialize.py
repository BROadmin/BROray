"""Startup initialization admits read-only queries without recovering any owner."""
import json,shutil,unittest
from test_operations import Operations,WORKSPACE
class Initialize(unittest.TestCase):
    def setUp(self):self.ops=Operations('runTest');self.ops.setUp()
    def tearDown(self):
        assert self.ops.temp.resolve().parent==(WORKSPACE/'.local').resolve()
        shutil.rmtree(self.ops.temp)
    def snapshot(self):return {p.relative_to(self.ops.state).as_posix():p.read_bytes() for p in self.ops.state.rglob('*') if p.is_file() and not p.is_symlink()}
    def test_empty_layout_allows_read_only_status_and_report(self):
        self.assertFalse((self.ops.state/'operations.guard').exists())
        self.assertTrue(self.ops.call('initialize')['ok'])
        before=self.snapshot();status=self.ops.call('status');report=self.ops.call('report')
        self.assertTrue(status['complete']);self.assertEqual(status['operations'],[])
        self.assertTrue(report['snapshotComplete']);self.assertEqual(before,self.snapshot())
    def test_initialization_preserves_live_owner_pause_and_journal(self):
        active=self.ops.begin();self.ops.call('pause');before=self.snapshot()
        self.assertTrue(self.ops.call('initialize')['ok']);self.assertEqual(before,self.snapshot())
        self.assertEqual(self.ops.call('status')['globalFence'],'managed_active')
    def test_initialization_preserves_ambiguous_fence(self):
        self.ops.call('initialize');fence=self.ops.temp/'global.lock';fence.mkdir();(fence/'foreign').write_text('KEEP')
        self.assertTrue(self.ops.call('initialize')['ok'])
        self.assertEqual((fence/'foreign').read_text(),'KEEP')
        self.assertEqual(self.ops.call('status')['globalFence'],'ambiguous')
    def test_unsafe_operations_directory_is_not_followed(self):
        foreign=self.ops.temp/'foreign';foreign.mkdir();(foreign/'canary').write_text('KEEP')
        (self.ops.state/'operations').symlink_to(foreign)
        self.assertEqual(self.ops.call('initialize',expected=1)['errorCode'],'UNSAFE_STATE')
        self.assertEqual((foreign/'canary').read_text(),'KEEP')
if __name__=='__main__':
    r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Initialize))
    (WORKSPACE/'docs/evidence/operations-initialize-tests.json').write_text(json.dumps({'status':'PASS' if r.wasSuccessful() else 'FAIL','testsRun':r.testsRun,'environment':'Linux native guard with private files and synthetic owner','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if r.wasSuccessful() else 1)
