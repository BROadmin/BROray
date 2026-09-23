"""Bound verification for boot recovery must include coordinator evidence."""
import json,subprocess,unittest
from test_generation_bootguard_binding import GuardBinding

class BoundVerify(GuardBinding):
 def verify(self):return subprocess.run(self.guard_args(verb='guard-verify-bound'),capture_output=True,text=True,timeout=5)
 def test_bound_verify_in_original_boot_is_read_only_not_stopped(self):
  self.assertEqual(self.stage_bound().returncode,0);before=self.inventory(self.home);r=self.verify()
  self.assertEqual(r.returncode,0,r.stderr);proof=json.loads(r.stdout)
  self.assertEqual(proof['phase'],'BOOT_GUARDS_VERIFIED');self.assertFalse(proof['oldBootEnded']);self.assertFalse(proof['activationAllowed']);self.assertFalse(proof['serviceStopped'])
  self.assertEqual(self.inventory(self.home),before)
 def test_bound_verify_refuses_corrupt_binding(self):
  self.assertEqual(self.stage_bound().returncode,0);self.binding.write_bytes(b'{broken');before=self.inventory(self.home)
  self.assertNotEqual(self.verify().returncode,0);self.assertEqual(self.inventory(self.home),before)
 def test_bound_verify_refuses_missing_binding(self):
  self.assertEqual(self.stage_bound().returncode,0);self.binding.unlink();before=self.inventory(self.home)
  self.assertNotEqual(self.verify().returncode,0);self.assertEqual(self.inventory(self.home),before)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(BoundVerify(n) for n in BoundVerify.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
