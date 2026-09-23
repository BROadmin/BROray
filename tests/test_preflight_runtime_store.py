"""Canonical guard staging retains exact native bytes outside operation history."""
import hashlib,json,stat,unittest
from pathlib import Path
from test_preflight_bootguard import PreflightBootguard
from test_updater_generation import GEN

class PreflightRuntime(PreflightBootguard):
 def setUp(self):
  super().setUp();self.native_sha=hashlib.sha256(Path(GEN).read_bytes()).hexdigest()
  self.store=self.updater/'runtimes';self.entry=self.store/self.native_sha
 def test_guard_staging_retains_exact_runtime_in_live_updater_state(self):
  service,_=self.start_service();r=self.guarded();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertTrue((self.entry/'runtime').is_file(),'canonical staging did not retain permanent runtime')
  self.assertEqual((self.entry/'runtime').read_bytes(),Path(GEN).read_bytes());self.assertEqual(stat.S_IMODE((self.entry/'runtime').stat().st_mode),0o700)
  record=json.loads((self.entry/'identity.json').read_bytes());self.assertEqual(record['runtimeSha256'],self.native_sha);self.assertFalse(record['processAuthority'])
  self.assertNotIn(self.operation(),self.entry.parents);self.assertIsNone(service.poll());self.assertTrue(self.lock.is_symlink());self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT')
 def test_foreign_runtime_entry_refused_before_live_entry_changes(self):
  service,script=self.start_service();original=script.read_bytes();self.entry.mkdir(parents=True,mode=0o700);(self.entry/'foreign').write_bytes(b'KEEP')
  r=self.guarded();self.assertNotEqual(r.returncode,0);self.assertEqual((self.entry/'foreign').read_bytes(),b'KEEP');self.assertEqual(script.read_bytes(),original)
  self.assertIsNone(service.poll());self.assertTrue(self.lock.is_symlink());self.assertFalse((self.guardpath()/'staged.receipt').exists())
 def test_missing_runtime_after_staging_not_reconstructed(self):
  self.start_service();tail='runtime="$BRORAY_OPS_UPDATER_ROOT/runtimes/$(sha256sum "$BRORAY_OPS_GENERATION" | cut -d " " -f 1)/runtime"; [ -f "$runtime" ] || exit 99; rm "$runtime"; broray_ops_preflight_stage_bootguard'
  r=self.guarded(tail);self.assertNotEqual(r.returncode,0);self.assertNotEqual(r.returncode,99,'first staging never retained runtime')
  self.assertFalse((self.entry/'runtime').exists());self.assertTrue(self.lock.is_symlink());self.assertTrue((self.guardpath()/'staged.receipt').exists())
 def test_corrupt_runtime_after_staging_preserved(self):
  self.start_service();tail='runtime="$BRORAY_OPS_UPDATER_ROOT/runtimes/$(sha256sum "$BRORAY_OPS_GENERATION" | cut -d " " -f 1)/runtime"; [ -f "$runtime" ] || exit 99; printf FOREIGN >"$runtime"; broray_ops_preflight_stage_bootguard'
  r=self.guarded(tail);self.assertNotEqual(r.returncode,0);self.assertNotEqual(r.returncode,99)
  self.assertEqual((self.entry/'runtime').read_bytes(),b'FOREIGN');self.assertTrue(self.lock.is_symlink())
 def test_nonprivate_updater_parent_refused_without_permission_repair(self):
  service,script=self.start_service();original=script.read_bytes();self.updater.chmod(0o755)
  r=self.guarded();self.assertNotEqual(r.returncode,0);self.assertEqual(stat.S_IMODE(self.updater.stat().st_mode),0o755)
  self.assertEqual(script.read_bytes(),original);self.assertIsNone(service.poll());self.assertTrue(self.lock.is_symlink())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(PreflightRuntime(n) for n in PreflightRuntime.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
