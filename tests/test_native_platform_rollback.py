"""Rollback restores exact guarded bytes without restarting an unproven legacy service."""
import json,unittest,shutil
from test_native_platform_install_safety import InstallFixture,FILES

class PlatformRollback(InstallFixture):
 def setUp(self):
  super().setUp();self.addCleanup(self.clear_rollback)
 def clear_rollback(self):
  p=self.op/'platform-rollback'
  if p.exists():shutil.rmtree(p)
  for name in ['platform-rollback.record','platform-rollback.record.pending']:
   p=self.op/name
   if p.exists():p.unlink()
 def test_exact_guarded_rollback_replays_and_retains_recovery_fence(self):
  self.ready();before=self.snapshot();r=self.install();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  r=self.invoke_phase('recovery-rollback');self.assertEqual(r.returncode,75,r.stdout+r.stderr)
  reply=json.loads(r.stdout);self.assertEqual(reply['phase'],'NEEDS_RECOVERY');self.assertTrue(reply['platformRestored']);self.assertFalse(reply['serviceStateRestored']);self.assertFalse(reply['activationAllowed'])
  after=self.snapshot()
  for path,value in before.items():self.assertEqual(after[path],value,path)
  self.assertTrue((self.op/'fence').is_dir());self.assertFalse((self.updater/'generations').exists())
  r=self.invoke_phase('recovery-rollback');self.assertEqual(r.returncode,75,r.stdout+r.stderr);self.assertTrue(json.loads(r.stdout)['replayed']);self.assertEqual(self.snapshot(),after)
  print('ROLLBACK_RECEIPT '+json.dumps({'filesRestored':7,'serviceRestarted':False,'replayExact':True,'fenceRetained':True,'phase':'NEEDS_RECOVERY'}),flush=True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([PlatformRollback('test_exact_guarded_rollback_replays_and_retains_recovery_fence')]))
 raise SystemExit(not r.wasSuccessful())
