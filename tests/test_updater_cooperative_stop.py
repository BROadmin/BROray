"""Exact updater orchestration: completed cooperative stops, not transport retries."""
from pathlib import Path
import json,os, subprocess, tempfile, unittest
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
SOURCE=ROOT/'runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-updater.sh'

class CooperativeStop(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name)
  source=SOURCE.read_text();a=source.index('\nservices_stop_captured()\n');b=source.index('\nservices_start_captured()\n',a)
  self.functions=source[a:b]
  (self.root/'services.tsv').write_text('S24broray\trunning\n')
  self.env=os.environ|{'CURRENT_OPERATION_DIR':str(self.root),'CURRENT_OPERATION_LOG':str(self.root/'log'),'CURRENT_OPERATION_ID':'update-test'}
 def invoke(self, completed='true', drain=0, first_rc=75, extra=''):
  script='''set -u
count=0
service_call() {
 count=$((count+1)); printf 'call:%s:%s:%s\n' "$count" "$1" "$2" >>"$CURRENT_OPERATION_DIR/events"
 SERVICE_CALL_COMPLETED=COMPLETED
 if [ "$count" = 1 ]; then return FIRST_RC; fi
 return 0
}
service_wait_cooperative_stop() { printf 'drain\n' >>"$CURRENT_OPERATION_DIR/events"; return DRAIN; }
service_stop_continue_intent() { printf 'intent:%s:%s\n' "$1" "$2" >>"$CURRENT_OPERATION_DIR/events"; return 0; }
'''.replace('=COMPLETED','='+completed).replace('FIRST_RC',str(first_rc)).replace('DRAIN',str(drain))
  # Overrides replace observations only; the real loop must preserve ordering.
  overrides=script[script.index('count=0'):]
  p=subprocess.run(['/bin/ash','-c','set -u\n'+self.functions+'\n'+overrides+'\n'+extra+'\nservices_stop_captured'],env=self.env,capture_output=True,text=True,timeout=6)
  events=(self.root/'events').read_text().splitlines()
  return p,events
 def test_completed_cooperative_stop_drains_before_bound_continuation(self):
  p,events=self.invoke();self.assertEqual(p.returncode,0,p.stderr)
  self.assertEqual(events,['call:1:stop:S24broray','drain','intent:S24broray:1','call:2:stop:S24broray'])
 def test_unconfirmed_reply_is_not_a_completed_stop(self):
  p,events=self.invoke(completed='false');self.assertNotEqual(p.returncode,0);self.assertEqual(events,['call:1:stop:S24broray'])
 def test_unknown_identity_or_timeout_never_reissues_stop(self):
  p,events=self.invoke(drain=75);self.assertNotEqual(p.returncode,0);self.assertEqual(events,['call:1:stop:S24broray','drain'])
 def test_other_service_error_is_not_retried(self):
  p,events=self.invoke(first_rc=74);self.assertNotEqual(p.returncode,0);self.assertEqual(events,['call:1:stop:S24broray'])
 def test_success_does_not_repeat(self):
  p,events=self.invoke(first_rc=0);self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(events,['call:1:stop:S24broray'])
 def test_failed_durable_intent_never_reissues_stop(self):
  p,events=self.invoke(extra='service_stop_continue_intent(){ return 74; }');self.assertNotEqual(p.returncode,0);self.assertEqual(events,['call:1:stop:S24broray','drain'])
 def test_two_pending_sidecars_have_two_explicit_continuations(self):
  extra='''service_call(){ count=$((count+1)); SERVICE_CALL_COMPLETED=true; printf 'call:%s\n' "$count" >>"$CURRENT_OPERATION_DIR/events"; [ "$count" -ge 3 ] || return 75; }'''
  p,events=self.invoke(extra=extra);self.assertEqual(p.returncode,0,p.stderr)
  self.assertEqual(events,['call:1','drain','intent:S24broray:1','call:2','drain','intent:S24broray:2','call:3'])
 def test_continuations_are_bounded(self):
  extra='''service_call(){ count=$((count+1)); SERVICE_CALL_COMPLETED=true; printf 'call:%s\n' "$count" >>"$CURRENT_OPERATION_DIR/events"; return 75; }'''
  p,events=self.invoke(extra=extra);self.assertNotEqual(p.returncode,0)
  self.assertEqual(len([x for x in events if x.startswith('call:')]),3)

class ServiceCompletion(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.home=Path(self.tmp.name)
  text=SOURCE.read_text();a=text.index('\nservice_call()\n');b=text.index('\nservices_capture()\n',a);self.code=text[a:b]
  (self.home/'init').mkdir();(self.home/'init/S24broray').write_text('#!/bin/ash\nexit 0\n')
  (self.home/'state.json').write_text(json.dumps(dict(operationId='update-test',stage='stopping')))
  native=self.home/'native';native.write_text('''#!/bin/ash
case "$CASE" in
 transport) echo 'SERVICE_CONNECTION_UNCONFIRMED'; exit 75 ;;
 corrupt) echo '{broken'; exit 75 ;;
 wrong) jq -nc '{requestId:"wrong",exitCode:75,ok:false}'; exit 75 ;;
 completed) jq -nc --arg id "${11}" '{requestId:$id,exitCode:75,ok:false}'; exit 75 ;;
 success) jq -nc --arg id "${11}" '{requestId:$id,exitCode:0,ok:true}'; exit 0 ;;
esac
''');native.chmod(0o755)
  self.env=os.environ|dict(BRORAY_UPDATER_GENERATION='fixture',BRORAY_UPDATER_SERVICE_HOST='host',BRORAY_UPDATER_GENERATION_NATIVE=str(native),BRORAY_UPDATER_GENERATION_ROOT='root',BRORAY_UPDATER_MANIFEST_SHA256='a'*64,BRORAY_UPDATER_SERVICE_INTERPRETER='/bin/ash',BRORAY_UPDATER_SERVICE_INTERPRETER_SHA256='b'*64,CURRENT_OPERATION_ID='update-test',CURRENT_OPERATION_DIR=str(self.home),CURRENT_PATH=str(self.home))
 def invoke(self,kind,expected):
  p=subprocess.run(['/bin/ash','-c','set -u\n'+self.code+'\ncurrent_slot(){ echo fixture-slot; }; service_call stop S24broray; rc=$?; echo COMPLETED=$SERVICE_CALL_COMPLETED; exit "$rc"'],env=self.env|{'CASE':kind},capture_output=True,text=True,timeout=6)
  self.assertEqual(p.returncode,0 if kind=='success' else 75);self.assertIn('COMPLETED='+str(expected).lower(),p.stdout)
 def test_completed_nonzero_is_distinct_from_transport_failure(self):self.invoke('completed',True)
 def test_transport_failure_never_authorizes_continuation(self):self.invoke('transport',False)
 def test_corrupt_reply_never_authorizes_continuation(self):self.invoke('corrupt',False)
 def test_wrong_request_never_authorizes_continuation(self):self.invoke('wrong',False)
 def test_success_remains_success(self):self.invoke('success',True)

class ContinuationIntent(unittest.TestCase):
 def test_distinct_durable_request_states_and_wrong_stage_refusal(self):
  with tempfile.TemporaryDirectory() as directory:
   home=Path(directory);op=home/'opt/var/lib/broray/operations/update-test';op.mkdir(parents=True)
   state=dict(schemaVersion=1,operationId='update-test',operation='update',state='running',stage='stopping',mutationStarted=False,rollbackPerformed=False)
   (op/'state.json').write_text(json.dumps(state))
   source=SOURCE.read_text();self.assertTrue(source.endswith('main "$@"\n'))
   tail='''
operation_select update-test || exit 1
service_stop_continue_intent S24broray 1 || exit 1
cp "$CURRENT_OPERATION_DIR/state.json" "$CURRENT_OPERATION_DIR/first.json"
service_stop_continue_intent S24broray 2 || exit 1
'''
   driver=home/'driver.sh';driver.write_text(source[:-len('main "$@"\n')]+tail)
   env=os.environ|{'BRORAY_UPDATER_ROOT_PREFIX':str(home)}
   p=subprocess.run(['/bin/ash',str(driver)],env=env,capture_output=True,text=True,timeout=10)
   self.assertEqual(p.returncode,0,p.stderr);self.assertNotEqual((op/'first.json').read_bytes(),(op/'state.json').read_bytes())
   state['stage']='switching';(op/'state.json').write_text(json.dumps(state));before=(op/'state.json').read_bytes()
   p=subprocess.run(['/bin/ash',str(driver)],env=env,capture_output=True,text=True,timeout=10)
   self.assertNotEqual(p.returncode,0);self.assertEqual((op/'state.json').read_bytes(),before)

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
