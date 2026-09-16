"""Subscription WebUI admission must keep its own cancellable job type."""
import ctypes,json,os,unittest
from test_subscription_jobs import ROOT,SubscriptionJobs


class SubscriptionApiScope(unittest.TestCase):
    setUp=SubscriptionJobs.setUp
    clean_fixture=SubscriptionJobs.clean_fixture
    shell=SubscriptionJobs.shell

    def test_subscription_web_job_is_not_a_protected_route_job(self):
        result=self.shell('''
. "$BRORAY_ROOT/web-new/api/subscriptions/common.sh"
broray_subscriptions_api_lock refresh
jq '{scope,cancelability,operation}' "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json"
''')
        self.assertEqual(json.loads(result.stdout),{'scope':'system','cancelability':'cooperative','operation':'subscriptions:refresh'})


if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SubscriptionApiScope))
    (ROOT/'docs/evidence/subscription-api-scope-tests.json').write_text(json.dumps({
        'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
        'environment':'Production WebUI admission and actual Linux owner','routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
