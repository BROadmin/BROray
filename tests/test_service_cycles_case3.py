import unittest
from test_installed_service_cycles import ServiceCycles
if __name__=="__main__":
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([ServiceCycles("test_parallel_starts_and_foreign_xray_process_are_safe")]))
 raise SystemExit(not r.wasSuccessful())
