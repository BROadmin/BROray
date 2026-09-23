import unittest
from test_cycle_boot_witness_tamper import BootWitnessTamper
if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([BootWitnessTamper('test_changed_last_witness_refuses_without_repair')]))
 raise SystemExit(not r.wasSuccessful())
