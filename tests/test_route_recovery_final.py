"""Final route completion/recovery integration after the completion baseline."""
import ctypes,json,subprocess,unittest
from pathlib import Path
from test_route_crash_recovery import ROOT,RouteCrashRecovery
NAMES=['test_crashed_progress_remains_available_for_existing_restore',
       'test_counter_and_resumable_progress_are_preserved',
       'test_finish_retains_fence_when_backend_leaves_running_progress',
       'test_finish_retains_fence_when_backend_leaves_lease',
       'test_interrupted_progress_publication_can_be_retried']
if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    Path('/opt/bin').mkdir(parents=True,exist_ok=True)
    subprocess.run(['/bin/busybox','--install','-s','/opt/bin'],check=True)
    if not Path('/opt/bin/jq').exists():Path('/opt/bin/jq').symlink_to('/usr/bin/jq')
    result=unittest.TextTestRunner(verbosity=2).run(unittest.TestSuite(RouteCrashRecovery(n) for n in NAMES))
    (ROOT/'docs/evidence/route-recovery-final-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'tests':NAMES,'routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
