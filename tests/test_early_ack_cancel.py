"""A rejected start must not leave a fence after its live owner exits."""
import json,shutil,unittest
from test_operations import Operations,WORKSPACE
class EarlyAckCancel(unittest.TestCase):
    def setUp(self):self.ops=Operations('runTest');self.ops.setUp()
    def tearDown(self):shutil.rmtree(self.ops.temp)
    def test_cancelled_unacknowledged_start_retires_its_fence(self):
        o=self.ops;a=o.begin(ack=False);o.call('stop-background')
        self.assertEqual(o.call('ack',a['operationId'],a['token'],'900001',expected=2)['errorCode'],'CANCELLED')
        s=o.call('status');self.assertEqual(s['globalFence'],'absent');self.assertTrue(s['automationPaused'])
        state=json.loads(o.opfile(a,'state.json').read_bytes())
        self.assertEqual(state['state'],'aborted');self.assertFalse(state['acknowledged'])
        self.assertEqual(state['errorCode'],'CANCELLED')
    def test_confirmed_live_job_is_not_retired_by_repeated_ack(self):
        o=self.ops;a=o.begin();o.call('stop-background')
        o.call('ack',a['operationId'],a['token'],'900001')
        self.assertEqual(o.call('status')['globalFence'],'managed_active')
    def test_cancelled_start_does_not_allow_foreign_owner_to_retire(self):
        o=self.ops;a=o.begin(ack=False);o.call('stop-background');o.set_owner({**o.owner,'startTicks':'999'})
        self.assertEqual(o.call('ack',a['operationId'],a['token'],'900001',expected=2)['errorCode'],'OWNER_CHANGED')
        self.assertTrue((o.temp/'global.lock').is_symlink())
if __name__=='__main__':
    r=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(EarlyAckCancel))
    (WORKSPACE/'docs/evidence/early-ack-cancel-tests.json').write_text(json.dumps({'status':'PASS' if r.wasSuccessful() else 'FAIL','testsRun':r.testsRun,'routerAccessed':False})+'\n')
    raise SystemExit(0 if r.wasSuccessful() else 1)
