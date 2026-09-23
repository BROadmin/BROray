"""Saved F04 against current source; a delay is never proof of abandonment."""
import json,unittest
from test_updater_daemon_exclusion import probe
from test_updater_preflight import UpdaterPreflight

class PublisherPreservation(UpdaterPreflight):
 def test_original_f04_live_publisher(self):
  result=probe['publisher'](self);print('F04_CURRENT_SOURCE '+json.dumps(result),flush=True)
  self.assertTrue(result['emptyFenceBeforeCleanup']);self.assertTrue(result['publisherAliveWhenFenceDeleted']);self.assertFalse(result['fenceDeleted']);self.assertFalse(result['bugReproduced'])
 def test_ownerless_empty_fence_is_preserved(self):
  source,env=probe['library'](self);self.assertEqual(probe['call_lib'](env,'ensure_layout').returncode,0)
  lock=self.root/'opt/var/lib/broray-updater/request.lock';lock.mkdir();r=probe['call_lib'](env,'request_lock_recover_abandoned')
  self.assertTrue(lock.is_dir());self.assertEqual(list(lock.iterdir()),[])
 def test_pid_without_complete_owner_is_preserved(self):
  source,env=probe['library'](self);self.assertEqual(probe['call_lib'](env,'ensure_layout').returncode,0)
  lock=self.root/'opt/var/lib/broray-updater/request.lock';lock.mkdir();(lock/'pid').write_text('2147483646\n')
  before={p.name:p.read_bytes() for p in lock.iterdir()};r=probe['call_lib'](env,'request_lock_recover_abandoned')
  self.assertTrue(lock.is_dir());self.assertEqual({p.name:p.read_bytes() for p in lock.iterdir()},before)

if __name__=='__main__':
 names=[n for n in PublisherPreservation.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(PublisherPreservation(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)
