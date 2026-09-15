"""Baseline crash after the real auto-state write, before leaving committing."""
import ctypes,json,unittest
from test_auto_switch_jobs import AutoSwitchJobs,ROOT
class PublicationBaseline(unittest.TestCase):
    setUp=AutoSwitchJobs.setUp
    shell=AutoSwitchJobs.shell
    collect=AutoSwitchJobs.collect
    states=AutoSwitchJobs.states
    reap_adopted_helpers=AutoSwitchJobs.reap_adopted_helpers
    once=AutoSwitchJobs.once
    def test_completed_atomic_write_does_not_leave_permanent_fence(self):
        file=self.app/'bin/broray-server-auto-switch'
        source=file.read_text()
        anchor='    broray_job_checkpoint working || exit $?\n    S_LOADED_SIGNATURE="$CURRENT_SIGNATURE"'
        self.assertEqual(source.count(anchor),1)
        file.write_text(source.replace(anchor,'    kill -KILL "$$"\n'+anchor))
        self.shell(self.once(),expected=137)
        state=self.states()[0]
        self.assertEqual(state['phase'],'committing')
        cache=self.app/'run/server-auto-switch-state.json'
        self.assertTrue(cache.is_file())
        self.assertEqual(json.loads(cache.read_text())['backgroundOperationId'],state['operationId'])
        # Desired recovery. Baseline rejects this with PROTECTED; freeze FAIL.
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call recover')
        self.assertFalse((self.temp/'global.lock').is_symlink())
if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(PublicationBaseline))
    (ROOT/'docs/evidence/atomic-publication-baseline-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'real Linux processes; controlled producer self-crash after real atomic write','routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
