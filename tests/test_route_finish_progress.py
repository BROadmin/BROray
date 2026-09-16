"""Targeted baseline for a backend leaving progress after releasing its lease."""
import ctypes,json,subprocess,unittest
from pathlib import Path
from test_route_crash_recovery import ROOT,RouteCrashRecovery

if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    Path('/opt/bin').mkdir(parents=True,exist_ok=True)
    subprocess.run(['/bin/busybox','--install','-s','/opt/bin'],check=True)
    if not Path('/opt/bin/jq').exists():Path('/opt/bin/jq').symlink_to('/usr/bin/jq')
    suite=unittest.TestSuite([RouteCrashRecovery('test_finish_retains_fence_when_backend_leaves_running_progress')])
    result=unittest.TextTestRunner(verbosity=2).run(suite)
    (ROOT/'docs/evidence/route-finish-progress-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
