"""A deleted post-mutation binding must remain evidence of an incomplete state."""
import unittest
from test_generation_bootguard_binding import GuardBinding

class BindingLoss(GuardBinding):
 def test_missing_binding_after_staging_is_never_recreated(self):
  self.assertEqual(self.stage_bound().returncode,0)
  self.binding.unlink();before=self.inventory(self.home);r=self.bind()
  self.assertNotEqual(r.returncode,0,'native binding replay recreated evidence after platform mutation')
  self.assertFalse(self.binding.exists());self.assertEqual(self.inventory(self.home),before)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([BindingLoss('test_missing_binding_after_staging_is_never_recreated')]))
 raise SystemExit(not r.wasSuccessful())
