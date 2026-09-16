"""Focused real-owner activation regression, sharing full server fixtures."""
import ctypes,json,unittest
from test_server_jobs import ServerJobs,ROOT
if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    names=[name for name in unittest.defaultTestLoader.getTestCaseNames(ServerJobs) if name.startswith('test_activation_')]
    suite=unittest.TestSuite(ServerJobs(name) for name in names)
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
    (ROOT/'docs/evidence/activation-prepare-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'tests':names,'environment':'actual Linux owners and supervisor; private server config and restart fixture','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
