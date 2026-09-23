import unittest
from test_installed_service_cycles import ServiceCycles
if __name__=="__main__":
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([ServiceCycles("test_corrupt_retired_ledger_and_wrong_native_request_are_preserved")]))
 raise SystemExit(not r.wasSuccessful())
