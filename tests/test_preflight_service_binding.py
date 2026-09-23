"""Real /proc service binding, private filesystem; no product stop signals."""
from pathlib import Path
import os,json,subprocess,time,unittest,hashlib
from test_preflight_admission import Admission,CODE,SHA
class ServiceBinding(Admission):
 def start_service(self,ready=True,args=None,name='service'):
  script=self.live.parent/'libexec/broray-updater/broray-updater.sh';script.parent.mkdir(parents=True,exist_ok=True)
  script.write_text('#!/bin/ash\ntrap "exit 0" TERM\nwhile :; do sleep 2; done\n');script.chmod(0o755)
  self.updater.mkdir(parents=True,exist_ok=True)
  p=subprocess.Popen(['/bin/ash',str(script),*(args or ['daemon'])],start_new_session=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  self.processes.append(p);time.sleep(.1);self.assertIsNone(p.poll())
  (self.updater/'daemon.pid').write_text(str(p.pid)+'\n');(self.updater/'daemon.lock').mkdir(exist_ok=True)
  if ready:(self.updater/'daemon.ready').write_text(str(p.pid)+'\n')
  return p,script
 def run_binding(self,tail='',prefix=''):
  body=prefix+'\nbroray_ops_preflight_admit "$TEST_SHA" || exit $?\nbroray_ops_preflight_stop_intent "$TEST_SHA" || exit $?\nbroray_ops_preflight_bind_service || exit $?\n'+tail
  return self.shell(body,timeout=40)
 def bound(self):return json.loads((self.operation()/'platform-service.json').read_text())
 def test_absent_service_is_evidence_not_ready_or_stopped(self):
  p=self.run_binding();self.assertEqual(p.returncode,0,(p.stdout,p.stderr));r=self.bound()
  self.assertIsNone(r['service']['owner']);self.assertFalse(r['service']['readyMarkerMatchesPid']);self.assertFalse(r['signalsAuthorized'])
  self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT');self.assertTrue(self.lock.is_symlink())
 def test_exact_live_service_bound_with_identity_and_hash(self):
  s,script=self.start_service();p=self.run_binding();self.assertEqual(p.returncode,0,(p.stdout,p.stderr));r=self.bound()
  self.assertFalse(r['service']['readinessProven']);self.assertEqual(r['service']['owner']['pid'],s.pid);self.assertTrue(r['service']['owner']['startTicks']);self.assertTrue(r['service']['readyMarkerMatchesPid'])
  row=next(x for x in r['service']['platformFiles'] if x['path'].endswith('/broray-updater.sh'))
  self.assertEqual(row['value']['sha256'],hashlib.sha256(script.read_bytes()).hexdigest());self.assertIsNone(s.poll())
 def test_live_but_not_ready_remains_not_ready(self):
  s,_=self.start_service(ready=False);p=self.run_binding();self.assertEqual(p.returncode,0,p.stderr)
  self.assertFalse(self.bound()['service']['readyMarkerMatchesPid']);self.assertIsNone(s.poll())
 def test_wrong_pid_projection_preserved(self):
  s,_=self.start_service();f=self.updater/'daemon.pid';f.write_text(str(os.getpid())+'\n');before=f.read_bytes()
  p=self.run_binding();self.assertNotEqual(p.returncode,0);self.assertEqual(f.read_bytes(),before);self.assertIsNone(s.poll())
 def test_missing_pid_projection_does_not_mean_absent(self):
  s,_=self.start_service();(self.updater/'daemon.pid').unlink();p=self.run_binding()
  self.assertNotEqual(p.returncode,0);self.assertIsNone(s.poll());self.assertFalse((self.operation()/'platform-service.json').exists())
 def test_stale_pid_projection_is_not_cleaned(self):
  self.updater.mkdir(parents=True);f=self.updater/'daemon.pid';f.write_text('2147483646\n');p=self.run_binding()
  self.assertNotEqual(p.returncode,0);self.assertEqual(f.read_text(),'2147483646\n')
 def test_wrong_ready_generation_refused(self):
  s,_=self.start_service();(self.updater/'daemon.ready').write_text('2147483646\n');p=self.run_binding()
  self.assertNotEqual(p.returncode,0);self.assertIsNone(s.poll())
 def test_duplicate_daemon_refused(self):
  s,script=self.start_service();other=subprocess.Popen(['/bin/ash',str(script),'daemon'],start_new_session=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  self.processes.append(other);time.sleep(.1);p=self.run_binding();self.assertNotEqual(p.returncode,0)
  self.assertIsNone(s.poll());self.assertIsNone(other.poll())
 def test_same_script_non_daemon_refused(self):
  s,_=self.start_service(args=['check']);p=self.run_binding();self.assertNotEqual(p.returncode,0);self.assertIsNone(s.poll())
 def test_unknown_lock_contents_preserved(self):
  s,_=self.start_service();f=self.updater/'daemon.lock/.unknown';f.write_text('KEEP');p=self.run_binding()
  self.assertNotEqual(p.returncode,0);self.assertEqual(f.read_text(),'KEEP');self.assertIsNone(s.poll())
 def test_symlinked_platform_parent_refused(self):
  opt=self.live.parent;foreign=self.home/'foreign';foreign.mkdir();(opt/'libexec').symlink_to(foreign,target_is_directory=True)
  p=self.run_binding();self.assertNotEqual(p.returncode,0);self.assertTrue((opt/'libexec').is_symlink())
 def test_repeat_same_service_is_idempotent(self):
  self.start_service();p=self.run_binding('broray_ops_preflight_bind_service || exit $?')
  self.assertEqual(p.returncode,0,(p.stdout,p.stderr));self.assertEqual(len(list(self.operation().glob('platform-service*'))),1)
 def test_changed_file_cannot_rewrite_saved_service(self):
  self.start_service();p=self.run_binding('printf changed >>"$BRORAY_ROOT/../libexec/broray-updater/broray-updater.sh"; broray_ops_preflight_bind_service')
  self.assertNotEqual(p.returncode,0);r=self.bound();script=self.live.parent/'libexec/broray-updater/broray-updater.sh'
  row=next(x for x in r['service']['platformFiles'] if x['path'].endswith('/broray-updater.sh'));self.assertNotEqual(row['value']['sha256'],hashlib.sha256(script.read_bytes()).hexdigest())
 def test_before_stop_intent_refused(self):
  p=self.shell('broray_ops_preflight_admit "$TEST_SHA" || exit $?; BRORAY_PREFLIGHT_STOP_NONCE=0123456789abcdef0123456789abcdef; broray_ops_preflight_bind_service')
  self.assertNotEqual(p.returncode,0);self.assertFalse((self.operation()/'platform-service.json').exists())
 def test_bound_snapshot_does_not_allow_generic_finish(self):
  p=self.run_binding('broray_ops_finish completed');self.assertNotEqual(p.returncode,0);self.assertTrue(self.lock.is_symlink())
 def test_other_owner_cannot_bind(self):
  p=self.shell('broray_ops_preflight_admit "$TEST_SHA" || exit $?; broray_ops_preflight_stop_intent "$TEST_SHA" || exit $?; broray_ops_call platform-preflight-service-bind "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" 2147483646 "$BRORAY_PREFLIGHT_STOP_NONCE"')
  self.assertNotEqual(p.returncode,0);self.assertFalse((self.operation()/'platform-service.json').exists())
 def test_lost_reply_retries_same_record(self):
  source=(CODE/'lib/operation-client.sh').read_text().replace('broray_ops_call()','broray_ops_call_original()',1)
  shim=self.home/'shim.sh';shim.write_text(source+'''
broray_ops_call() {
 local output rc
 rc=0; output="$(broray_ops_call_original "$@")" || rc=$?
 if [ "$1" = platform-preflight-service-bind ] && [ "$rc" = 0 ] && [ ! -e "$TEST_HOME/dropped" ]; then
  : >"$TEST_HOME/dropped"; return 0
 fi
 [ -z "$output" ] || printf '%s\\n' "$output"
 return "$rc"
}
''')
  p=self.run_binding(prefix='. "'+str(shim)+'"');self.assertEqual(p.returncode,0,(p.stdout,p.stderr));self.assertTrue((self.home/'dropped').exists())
if __name__=='__main__':
 names=sorted(k for k in ServiceBinding.__dict__ if k.startswith('test_'))
 suite=unittest.TestSuite(ServiceBinding(n) for n in names)
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
 print('SERVICE_BINDING_RECEIPT '+json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'signalsSentByProduct':False,'routerAccess':False}),flush=True)
 raise SystemExit(0 if result.wasSuccessful() else 1)
