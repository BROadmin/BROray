"""Real ash/JQ and files; native job owner is tested separately. No networking."""
import json,os,shutil,subprocess,tempfile,time,unittest
from pathlib import Path
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
class Auto(unittest.TestCase):
 def setUp(self):
  self.t=tempfile.TemporaryDirectory(prefix='broray-dot-auto-');self.addCleanup(self.t.cleanup)
  self.app=Path(self.t.name)/'app';shutil.copytree(ROOT/'runtime/app/lib',self.app/'lib')
  for d in ['tmp','bin','routes/dot','config/subscriptions']: (self.app/d).mkdir(parents=True,exist_ok=True)
  self.dot=self.app/'routes/dot';self.now=int(time.time());self.ids=['google-primary']
  self.write('config.json',{'schemaVersion':3,'requestedIds':self.ids,'selectedIds':self.ids,'effectiveIds':[],'managed':[],'quarantinedReceipts':[]})
  self.write('state.json',{'schemaVersion':1,'tests':[],'lastError':'KEEP_LAST_APPLY_ERROR','lastOperation':{'type':'apply','success':False}})
  self.write('auto-check.json',{'schemaVersion':1,'enabled':True})
  self.env={**os.environ,'BRORAY_ROOT':str(self.app),'BRORAY_BASE':str(self.app),'BRORAY_STATE_ROOT':str(self.app/'state'),'PATH':str(self.app/'bin')+':/usr/bin:/bin:/usr/sbin:/sbin'}
  shutil.copyfile(ROOT/'runtime/app/bin/broray-subscription-scheduler',self.app/'bin/broray-subscription-scheduler')
 def write(self,name,data): (self.dot/name).write_text(json.dumps(data),encoding='utf-8')
 def read(self,name): return json.loads((self.dot/name).read_text())
 def shell(self,code,*args,library=True):
  prefix='. "$BRORAY_ROOT/lib/dot-auto.sh"\n' if library else ''
  return subprocess.run(['/bin/ash','-c',prefix+code,'qa',*map(str,args)],env=self.env,capture_output=True,timeout=25)
 def due(self): return self.shell('broray_dot_auto_due').returncode==0
 def view(self):
  p=self.shell('broray_dot_auto_view');self.assertEqual(p.returncode,0,p.stderr);return json.loads(p.stdout)
 def test_regression_scheduler_runs_without_subscriptions(self):
  p=self.app/'lib/service-lifecycle.sh';p.write_text('broray_service_run_job() { printf "%s\\n" "$2" >>"$BRORAY_ROOT/calls"; }\n')
  p=self.shell('"/bin/ash" "$BRORAY_ROOT/bin/broray-subscription-scheduler" --once',library=False)
  self.assertEqual(p.returncode,0,p.stderr);self.assertTrue((self.app/'calls').exists(),'DoT was not scheduled');self.assertEqual((self.app/'calls').read_text().strip(),'--dot-job')
 def test_enabled_first_check_due(self): self.assertTrue(self.due())
 def test_disabled_not_due(self): self.write('auto-check.json',{'schemaVersion':1,'enabled':False});self.assertFalse(self.due())
 def test_missing_settings_defaults_off(self): (self.dot/'auto-check.json').unlink();self.assertFalse(self.due());self.assertFalse(self.view()['enabled'])
 def test_invalid_settings_unknown(self): self.write('auto-check.json',{'schemaVersion':1,'enabled':'true'});self.assertFalse(self.due());self.assertFalse(self.view()['settingsValid'])
 def test_empty_selection_no_probes(self): self.write('config.json',{'schemaVersion':3,'requestedIds':[],'selectedIds':[]});self.assertFalse(self.due())
 def test_recovery_marker_blocks(self): self.write('transaction-recovery-required.json',{});before=(self.dot/'state.json').read_bytes();self.assertFalse(self.due());self.assertEqual((self.dot/'state.json').read_bytes(),before)
 def test_symlink_settings_not_followed(self):
  source=self.dot/'auto-check.json';target=self.app/'foreign.json';source.rename(target);source.symlink_to(target);self.assertFalse(self.due());self.assertFalse(self.view()['settingsValid'])
 def test_recent_manual_test_delays(self): self.write('state.json',{'schemaVersion':1,'tests':[{'id':'google-primary','testedEpoch':self.now,'ok':True}]});self.assertFalse(self.due())
 def test_recent_failure_does_not_spin(self): self.write('state.json',{'schemaVersion':1,'tests':[{'id':'google-primary','testedEpoch':self.now,'ok':False}]});self.assertFalse(self.due())
 def test_old_failure_retries(self): self.write('state.json',{'schemaVersion':1,'tests':[{'id':'google-primary','testedEpoch':self.now-301,'ok':False}]});self.assertTrue(self.due())
 def test_future_timestamp_does_not_stall(self): self.write('state.json',{'schemaVersion':1,'tests':[{'id':'google-primary','testedEpoch':self.now+900,'ok':True}]});self.assertTrue(self.due())
 def test_partial_set_due(self):
  self.write('config.json',{'schemaVersion':3,'requestedIds':['google-primary','quad9'],'selectedIds':['google-primary','quad9']});self.write('state.json',{'schemaVersion':1,'tests':[{'id':'google-primary','testedEpoch':self.now,'ok':True}]});self.assertTrue(self.due())
 def test_recent_manual_test_delays_override(self):
  self.write('state.json',{'schemaVersion':1,'tests':[{'id':'other','testedEpoch':self.now,'ok':True}]});self.assertTrue(self.due())
 def test_recent_internal_attempt_delays(self): self.write('state.json',{'schemaVersion':1,'tests':[],'autoCheck':{'selectedIds':self.ids,'lastAttemptEpoch':self.now,'status':'error'}});self.assertFalse(self.due())
 def test_old_internal_attempt_due(self): self.write('state.json',{'schemaVersion':1,'tests':[],'autoCheck':{'selectedIds':self.ids,'lastAttemptEpoch':self.now-301,'status':'error'}});self.assertTrue(self.due())
 def test_bad_state_preserved(self): (self.dot/'state.json').write_text('broken');self.assertFalse(self.due());self.assertEqual((self.dot/'state.json').read_text(),'broken')
 def test_pause_projection(self):
  p=self.app/'state';p.mkdir();(p/'background-automation.json').write_text('{"paused":true}');self.assertTrue(self.view()['paused'])
 def test_save_changes_only_setting(self):
  p=self.app/'tmp/body';p.write_text('{"enabled":false}');before={n:(self.dot/n).read_bytes() for n in ['config.json','state.json']}
  r=self.shell('broray_job_checkpoint() { return 0; }; broray_dot_auto_save "$1"',p);self.assertEqual(r.returncode,0,r.stderr);self.assertFalse(self.read('auto-check.json')['enabled']);self.assertEqual((self.dot/'auto-check.json').stat().st_mode&0o777,0o600)
  self.assertEqual(before,{n:(self.dot/n).read_bytes() for n in before})
 def test_save_invalid_flag_no_change(self):
  p=self.app/'tmp/body';p.write_text('{"enabled":"false"}');before=(self.dot/'auto-check.json').read_bytes();r=self.shell('broray_job_checkpoint() { return 0; }; broray_dot_auto_save "$1"',p);self.assertEqual(r.returncode,64);self.assertEqual((self.dot/'auto-check.json').read_bytes(),before)
 def test_save_cancelled_no_change(self):
  p=self.app/'tmp/body';p.write_text('{"enabled":false}');before=(self.dot/'auto-check.json').read_bytes();r=self.shell('broray_job_checkpoint() { return 130; }; broray_dot_auto_save "$1"',p);self.assertEqual(r.returncode,130);self.assertEqual((self.dot/'auto-check.json').read_bytes(),before)
 def probe(self,rc=0,tamper=False):
  work=self.app/'tmp/dot-auto-qa';work.mkdir();(work/'operation-id').write_text('op-qa');(work/'request.json').write_text(json.dumps({'serverIds':self.ids,'allowUntested':False}))
  p=self.shell('. "$BRORAY_ROOT/lib/routes-dot.sh"; broray_dot_entries_for_request "$1/request.json" "$1/entries.json"',work);self.assertEqual(p.returncode,0,p.stderr)
  if tamper:
   entries=json.loads((work/'entries.json').read_text());entries[0]['address']='127.0.0.1';(work/'entries.json').write_text(json.dumps(entries))
  fake=self.app/'bin/openssl';fake.write_text('#!/bin/ash\nprintf "%s\\n" "$*" >>"$BRORAY_ROOT/tls-calls"\nexit '+str(rc)+'\n');fake.chmod(0o755)
  self.env.update({'BRORAY_OPS_SUPERVISED':'ptrace/1','BRORAY_BACKGROUND_OPERATION_ID':'op-qa','BRORAY_DOT_OPENSSL':str(fake)})
  before={n:(self.dot/n).read_bytes() for n in ['config.json','state.json']}
  p=self.shell('"/bin/ash" "$BRORAY_ROOT/lib/dot-auto-probe.sh" "$1"',work)
  self.assertEqual(before,{n:(self.dot/n).read_bytes() for n in before})
  return p,work
 def test_probe_success_validates_certificate_name(self):
  p,w=self.probe();self.assertEqual(p.returncode,0,p.stderr);results=json.loads((w/'results.json').read_text());self.assertTrue(results[0]['ok']);self.assertEqual(results[0]['sni'],'dns.google');calls=(self.app/'tls-calls').read_text();self.assertIn('-verify_hostname dns.google -verify_return_error',calls)
 def test_probe_failure_not_success(self):
  p,w=self.probe(rc=1);self.assertEqual(p.returncode,0,p.stderr);r=json.loads((w/'results.json').read_text())[0];self.assertFalse(r['ok']);self.assertEqual(r['status'],'failed')
 def test_probe_tamper_no_connection(self):
  p,w=self.probe(tamper=True);self.assertNotEqual(p.returncode,0);self.assertFalse((self.app/'tls-calls').exists())
 def test_probe_requires_supervision(self):
  p=self.shell('"/bin/ash" "$BRORAY_ROOT/lib/dot-auto-probe.sh" "$BRORAY_ROOT/tmp"');self.assertEqual(p.returncode,73)
 def test_due_subscription_does_not_skip_dot_cycle(self):
  (self.app/'config/subscriptions/qa.json').write_text('{"enabled":true,"autoUpdateEnabled":true,"nextUpdateEpoch":1}')
  (self.app/'lib/service-lifecycle.sh').write_text('broray_service_run_job() { printf "%s\\n" "$2" >>"$BRORAY_ROOT/calls"; }\n')
  p=self.shell('/bin/ash "$BRORAY_ROOT/bin/broray-subscription-scheduler" --once',library=False);self.assertEqual(p.returncode,0,p.stderr);self.assertEqual((self.app/'calls').read_text().splitlines(),['--job','--dot-job'])
 def test_saving_does_not_accept_selection(self):
  p=self.app/'tmp/body';p.write_text('{"enabled":true,"serverIds":["quad9"]}');before=(self.dot/'config.json').read_bytes();r=self.shell('broray_job_checkpoint() { return 0; }; broray_dot_auto_save "$1"',p);self.assertEqual(r.returncode,64);self.assertEqual((self.dot/'config.json').read_bytes(),before)
 def api_denied(self,method='POST',header='operations',origin='http://evil.invalid',auth='1'):
  target=self.app/'web-new/api/routes';target.mkdir(parents=True)
  for name in ['dot-common.sh','dot-auto-settings.cgi']:shutil.copyfile(ROOT/'runtime/app/web-new/api/routes'/name,target/name)
  (target.parent/'auth-common.sh').write_text('broray_api_error() { printf "%s\\n" "$2"; exit 1; }\nbroray_api_require_method() { [ "$REQUEST_METHOD" = "$1" ] || broray_api_error 405 METHOD_REJECTED; }\nbroray_api_require_session() { [ "$TEST_AUTH" = 1 ] || broray_api_error 401 AUTH_REQUIRED; }\n')
  self.env.update({'REQUEST_METHOD':method,'HTTP_X_BRORAY_REQUEST':header,'HTTP_ORIGIN':origin,'HTTP_HOST':'router.invalid','TEST_AUTH':auth})
  before=(self.dot/'auto-check.json').read_bytes();r=self.shell('/bin/ash "$BRORAY_ROOT/web-new/api/routes/dot-auto-settings.cgi"',library=False);self.assertNotEqual(r.returncode,0);self.assertEqual((self.dot/'auto-check.json').read_bytes(),before);return r.stdout.decode()
 def test_settings_api_rejects_wrong_origin(self): self.assertIn('ORIGIN_REJECTED',self.api_denied())
 def test_settings_api_requires_custom_header(self): self.assertIn('CSRF_REJECTED',self.api_denied(header=''))
 def test_settings_api_requires_session(self): self.assertIn('AUTH_REQUIRED',self.api_denied(auth='0'))
 def test_settings_api_rejects_get(self): self.assertIn('METHOD_REJECTED',self.api_denied(method='GET'))
if __name__=='__main__':
 tests=list(unittest.defaultTestLoader.loadTestsFromTestCase(Auto))
 if os.environ.get('STAGE10_REPRO')=='1':tests=[t for t in tests if 'test_regression_' in t.id()]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(tests))
 print('STAGE10_UNIT_REPORT='+json.dumps({'testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'skipped':len(result.skipped),'routerAccessed':False,'owner':'stubbed where needed','TLS':'synthetic openssl executable'}))
 raise SystemExit(not result.wasSuccessful())
