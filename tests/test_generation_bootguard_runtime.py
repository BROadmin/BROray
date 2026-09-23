"""Recovery must retain exact native bytes independently of a temporary source slot."""
import hashlib,subprocess,unittest
from pathlib import Path
from test_generation_bootguard import BootGuard,GEN

class RuntimeRetention(BootGuard):
 def test_runtime_survives_without_original_program_path(self):
  r=self.guard();self.assertEqual(r.returncode,0,r.stderr)
  runtime=self.guards/'runtime';self.assertTrue(runtime.is_file(),'BOOT_GUARD_RECOVERY_RUNTIME_MISSING')
  self.assertEqual(runtime.read_bytes(),Path(GEN).read_bytes());self.assertEqual(runtime.stat().st_mode&0o777,0o700)
  # Invoke the retained image directly; do not mutate the shared fixture binary.
  args=self.guard_args('guard-verify');args[0]=str(runtime)
  check=subprocess.run(args,capture_output=True,text=True,timeout=5);self.assertEqual(check.returncode,0,check.stderr)
 def test_corrupt_runtime_cannot_be_replaced_by_retry(self):
  self.assertEqual(self.guard().returncode,0);(self.guards/'runtime').write_bytes(b'FOREIGN')
  before=self.inventory(self.home);self.assertNotEqual(self.guard().returncode,0);self.assertEqual(self.inventory(self.home),before)
 def test_missing_runtime_cannot_be_recreated(self):
  self.assertEqual(self.guard().returncode,0);(self.guards/'runtime').unlink()
  before=self.inventory(self.home);self.assertNotEqual(self.guard().returncode,0);self.assertEqual(self.inventory(self.home),before)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(RuntimeRetention(n) for n in RuntimeRetention.__dict__ if n.startswith('test_')))
 raise SystemExit(not result.wasSuccessful())
