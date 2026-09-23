import unittest
from test_service_origin_crash import OriginCrash
if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([OriginCrash('test_origin_anchor_pending_replays')]))
 raise SystemExit(not r.wasSuccessful())
