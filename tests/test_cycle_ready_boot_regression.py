"""The original ready-boot assertions with the current real RAM fixture."""
import unittest
from test_cycle_unready_boot_resume import UnreadyBootResume
from test_cycle_running_boot_resume import CycleRunningBootResume

class ReadyBootRegression(UnreadyBootResume):
 def test_original_ready_boot_contract(self):
  self.assertFalse(self.e.get('unreadyBoot'))
  CycleRunningBootResume.test_new_boot_starts_fresh_generation_preserving_retired_history(self)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([ReadyBootRegression('test_original_ready_boot_contract')]))
 raise SystemExit(not r.wasSuccessful())
