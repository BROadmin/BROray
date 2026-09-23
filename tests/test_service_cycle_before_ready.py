import unittest
from test_service_cycle_reply_crash import ReplyCrash
if __name__=="__main__":
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([ReplyCrash('test_caller_dies_after_writer_birth_before_readiness')]))
 raise SystemExit(not r.wasSuccessful())
