import unittest
from test_installed_service_cycles import ServiceCycles

if __name__=="__main__":
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([ServiceCycles('test_multiple_cycles_idempotent_start_and_public_restart')]))
 raise SystemExit(not result.wasSuccessful())
