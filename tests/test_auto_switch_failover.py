"""Run the full activation regression independently of the long job suite."""
import ctypes,json,unittest
from test_auto_switch_jobs import AutoSwitchJobs,ROOT
if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    suite=unittest.TestSuite([AutoSwitchJobs('test_down_monitor_reaches_activation_and_enters_cooldown')])
    result=unittest.TextTestRunner(verbosity=2).run(suite)
    (ROOT/'docs/evidence/auto-switch-failover-tests.json').write_text(json.dumps(dict(
        status='PASS' if result.wasSuccessful() else 'FAIL',testsRun=result.testsRun,
        routerAccessed=False,environment='Production activation with isolated files and fixture network/init'),indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
