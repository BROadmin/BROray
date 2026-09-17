"""Real coordinator/owner/supervisor, private files, fake TLS. No router access."""
import ctypes,json,os,time,unittest
from pathlib import Path
from test_subscription_jobs import SubscriptionJobs
class Jobs(unittest.TestCase):
 clean_fixture=SubscriptionJobs.clean_fixture;states=SubscriptionJobs.states;shell=SubscriptionJobs.shell
 def setUp(self):
  SubscriptionJobs.setUp(self)
  self.dot=self.app/'routes/dot';self.dot.mkdir(parents=True,exist_ok=True)
  self.config={'schemaVersion':3,'requestedIds':['google-primary'],'selectedIds':['google-primary'],'effectiveIds':[],'managed':[],'quarantinedReceipts':[]}
  (self.dot/'config.json').write_text(json.dumps(self.config))
  (self.dot/'state.json').write_text(json.dumps({'schemaVersion':1,'tests':[],'lastError':'KEEP','lastOperation':{'type':'apply','success':False}}))
  (self.dot/'auto-check.json').write_text('{"schemaVersion":1,"enabled":true}')
  fake=self.app/'bin/openssl';fake.write_text('#!/bin/ash\necho called >>"$BRORAY_ROOT/tls-calls"\nexit "${TEST_TLS_RC:-0}"\n');fake.chmod(0o755)
  self.env['BRORAY_DOT_OPENSSL']=str(fake)
 reap_adopted_helpers=SubscriptionJobs.reap_adopted_helpers
 def job(self):
  import subprocess
  p=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-subscription-scheduler'),'--dot-job'],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  until=time.monotonic()+90
  try:
   while True:
    self.reap_adopted_helpers()
    try: out,err=p.communicate(timeout=.1);break
    except subprocess.TimeoutExpired:
     self.assertLess(time.monotonic(),until,'Preserve evidence: job did not complete')
   self.assertEqual(p.returncode,0,(out,err));return p
  finally:
   if p.poll() is None:p.kill();p.communicate(timeout=10)
 def test_success_preserves_router_settings_and_previous_errors(self):
  before=(self.dot/'config.json').read_bytes();self.job();state=json.loads((self.dot/'state.json').read_text())
  self.assertEqual((self.dot/'config.json').read_bytes(),before);self.assertTrue(state['tests'][0]['ok']);self.assertEqual(state['autoCheck']['status'],'success');self.assertEqual(state['lastError'],'KEEP');self.assertEqual(state['lastOperation'],{'type':'apply','success':False});self.assertEqual(self.states()[0]['state'],'completed');self.assertEqual(list((self.app/'tmp').glob('dot-auto-*')),[])
 def test_failed_tls_remains_failed(self):
  self.env['TEST_TLS_RC']='1';self.job();s=json.loads((self.dot/'state.json').read_text());self.assertFalse(s['tests'][0]['ok']);self.assertEqual(s['autoCheck']['status'],'failed');self.assertEqual(s['lastError'],'KEEP')
 def test_paused_automation_does_not_probe(self):
  before=(self.dot/'state.json').read_bytes();self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call pause');self.job();self.assertFalse((self.app/'tls-calls').exists());self.assertEqual((self.dot/'state.json').read_bytes(),before);self.assertEqual(self.states(),[])
 def test_repeated_job_not_duplicate_probe(self):
  self.job();calls=(self.app/'tls-calls').read_bytes();self.job();self.assertEqual((self.app/'tls-calls').read_bytes(),calls)
 def test_disabled_does_not_probe(self):
  (self.dot/'auto-check.json').write_text('{"schemaVersion":1,"enabled":false}');self.job();self.assertFalse((self.app/'tls-calls').exists())
 def test_unknown_global_lock_is_not_removed(self):
  p=Path(self.env['BRORAY_ROUTES_API_LOCK']);p.mkdir();(p/'foreign').write_text('KEEP');self.job();self.assertEqual((p/'foreign').read_text(),'KEEP');self.assertFalse((self.app/'tls-calls').exists())
 def test_setting_saved_by_actual_owner(self):
  before=(self.dot/'config.json').read_bytes();body=self.app/'tmp/setting';body.write_text('{"enabled":false}')
  script='. "$BRORAY_ROOT/lib/operation-job.sh"; . "$BRORAY_ROOT/lib/dot-auto.sh"; broray_job_begin system dot:auto-settings dns-over-tls USER protected || exit $?; trap \'broray_job_exit "$?"\' EXIT; broray_dot_auto_save "$BRORAY_ROOT/tmp/setting"'
  self.shell(script,timeout=60);self.assertFalse(json.loads((self.dot/'auto-check.json').read_text())['enabled']);self.assertEqual((self.dot/'config.json').read_bytes(),before)
 def test_cancel_drains_without_publishing_tests(self):
  import subprocess
  fake=self.app/'bin/openssl';fake.write_text('#!/bin/ash\necho ready >"$BRORAY_ROOT/ready"\nsleep 60\n');fake.chmod(0o755)
  p=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-subscription-scheduler'),'--dot-job'],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  try:
   until=time.monotonic()+35
   while not (self.app/'ready').exists():
    self.assertIsNone(p.poll());self.assertLess(time.monotonic(),until);time.sleep(.1)
   op=self.states()[0]['operationId'];self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call cancel '+op,timeout=30)
   until=time.monotonic()+35
   while True:
    self.reap_adopted_helpers()
    try:out,err=p.communicate(timeout=.1);break
    except subprocess.TimeoutExpired:self.assertLess(time.monotonic(),until)
   self.assertEqual(p.returncode,130,(out,err));self.assertEqual(json.loads((self.dot/'state.json').read_text())['tests'],[]);self.assertEqual(self.states()[0]['state'],'aborted');self.assertEqual(list((self.app/'tmp').glob('dot-auto-*')),[])
  finally:
   if p.poll() is None:p.kill();p.communicate(timeout=10)
if __name__=='__main__':
 assert os.name!='nt';assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
 tests=list(unittest.defaultTestLoader.loadTestsFromTestCase(Jobs));group=os.environ.get('STAGE10_UPDATE_GROUP','all')
 if group=='a': tests=tests[:3]
 elif group=='b': tests=tests[3:]
 selected=[t.id() for t in tests];result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(tests))
 print('STAGE10_JOB_REPORT='+json.dumps({'testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'skipped':len(result.skipped),'selected':selected,'nativeOwnerSupervisor':'real','TLS':'fixture','routerAccessed':False}))
 raise SystemExit(not result.wasSuccessful())
