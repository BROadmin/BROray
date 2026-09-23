"""Saved F01 and exact preservation of the actual /tmp legacy fence."""
import json,unittest
from test_updater_daemon_exclusion import probe
from test_updater_preflight import UpdaterPreflight

class ActualLegacyFence(UpdaterPreflight):
 def test_original_f01_actual_legacy_path(self):
  r=probe['legacy'](self);print('F01_CURRENT_SOURCE '+json.dumps(r),flush=True)
  self.assertFalse(r['bugReproduced']);self.assertNotEqual(r['returncode'],0);self.assertFalse(r['platformChanged']);self.assertTrue(r['legacyOwnerAlive']);self.assertTrue(r['actualLegacyLockExists'])
 def test_ownerless_actual_legacy_fence_preserved(self):
  lock=self.root/'tmp/broray-global-operation.lock';lock.mkdir(parents=True);before=self.snapshot();r=self.call()
  self.assertNotEqual(r.returncode,0);self.assertEqual(self.snapshot(),before);self.assertTrue(lock.is_dir());self.assertEqual(list(lock.iterdir()),[]);self.assertFalse(self.log.exists())
 def test_symlink_actual_legacy_fence_preserved(self):
  target=self.home/'foreign-lock';target.mkdir();(target/'KEEP').write_text('unchanged\n');lock=self.root/'tmp/broray-global-operation.lock';lock.parent.mkdir(parents=True);lock.symlink_to(target)
  before=self.snapshot();r=self.call();self.assertNotEqual(r.returncode,0);self.assertEqual(self.snapshot(),before);self.assertTrue(lock.is_symlink());self.assertEqual((target/'KEEP').read_text(),'unchanged\n');self.assertFalse(self.log.exists())

if __name__=='__main__':
 names=[n for n in ActualLegacyFence.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(ActualLegacyFence(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)
