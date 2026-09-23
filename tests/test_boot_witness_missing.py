import unittest
from test_cycle_boot_witness_tamper import BootWitnessTamper
if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([BootWitnessTamper('test_missing_last_witness_refuses_without_new_generation')]))
 raise SystemExit(not r.wasSuccessful())
