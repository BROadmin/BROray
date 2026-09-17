import ctypes,json,unittest
from test_subscription_jobs import SubscriptionJobs
assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
names=['test_complete_update_uses_real_parser_and_commits_servers','test_paused_scheduler_starts_no_update','test_scheduler_records_child_job_and_real_error']
r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(SubscriptionJobs(n) for n in names))
print('STAGE10_REGRESSION_REPORT='+json.dumps({'testsRun':r.testsRun,'failures':len(r.failures),'errors':len(r.errors),'selected':names,'routerAccessed':False}))
raise SystemExit(not r.wasSuccessful())
