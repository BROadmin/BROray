"""Real second-boot fixtures: legacy shutdown may remove live PID projections.

Immutable staged evidence must remain intact. No current process is signalled.
"""
import json,subprocess,unittest
import test_native_legacy_retirement_evidence as legacy

class LegacyShutdown(legacy.LegacyRetirementEvidence):
 def stage_evidence(self):
  return {p.name:p.read_bytes() for p in (self.op/'platform-legacy-control').iterdir() if p.is_file()}
 def missing_then_retire(self,names):
  evidence=self.stage_evidence()
  for name in names:
   p=self.updater/name
   if p.is_dir():p.rmdir()
   else:p.unlink()
  before=self.snapshot();r=self.inspect()
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  result=json.loads(r.stdout);self.assertTrue(result['oldBootEnded'])
  self.assertFalse(result['serviceStopped']);self.assertEqual(self.snapshot(),before)
  self.first();after=self.snapshot();r=self.retire()
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertTrue(json.loads(r.stdout)['replayed'])
  self.assertEqual(self.snapshot(),after);self.assertEqual(self.stage_evidence(),evidence)
  for name in names:self.assertFalse((self.retired/'objects'/name).exists())
 def test_ready_removed_by_old_shutdown_after_real_boot(self):self.missing_then_retire(['daemon.ready'])
 def test_pid_removed_by_old_shutdown_after_real_boot(self):self.missing_then_retire(['daemon.pid'])
 def test_empty_lock_removed_by_old_shutdown_after_real_boot(self):self.missing_then_retire(['daemon.lock'])
 def test_all_live_projections_removed_after_real_boot(self):self.missing_then_retire(['daemon.pid','daemon.ready','daemon.lock'])
 def test_missing_immutable_ready_copy_is_not_recreated(self):
  (self.updater/'daemon.ready').unlink();p=self.op/'platform-legacy-control/file-1';p.unlink();self.refuse();self.assertFalse(p.exists())
 def test_changed_surviving_ready_is_preserved(self):
  p=self.updater/'daemon.ready';p.write_bytes(b'999999\n');self.refuse();self.assertEqual(p.read_bytes(),b'999999\n')
 def test_missing_retired_ready_evidence_is_not_shutdown(self):
  self.first();p=self.retired/'objects/daemon.ready';p.unlink();self.refuse();self.assertFalse(p.exists())

def load_tests(loader,tests,pattern):
 return unittest.TestSuite(LegacyShutdown(n) for n in LegacyShutdown.__dict__ if n.startswith('test_'))

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(load_tests(None,None,None))
 raise SystemExit(not r.wasSuccessful())
