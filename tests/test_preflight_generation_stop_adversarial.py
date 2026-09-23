"""Target preservation and lost replies on the canonical generation-stop path."""
import json,subprocess,unittest
from test_preflight_generation_stop import GenerationStop,CODE

class GenerationStopAdversarial(GenerationStop):
 def prepared(self,tail):
  return self.shell('broray_ops_preflight_admit "$TEST_SHA" || exit $?\nbroray_ops_preflight_stop_intent "$TEST_SHA" || exit $?\n'+tail,timeout=45)
 def intact(self):
  self.assertEqual(self.latest()['state'],'RUNNING');self.assertFalse(self.latest()['termSent']);self.assertTrue(self.lock.is_symlink())
  self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT')
 def test_wrong_manifest_never_signals(self):
  self.env['TEST_CURRENT_SHA']='0'*64;p=self.stop();self.assertNotEqual(p.returncode,0);self.assertIn('PLATFORM_CHANGED',p.stdout);self.intact()
 def test_changed_platform_mode_never_signals(self):
  file=self.root/'opt/etc/init.d/S22broray-updater';file.chmod(0o700);p=self.stop()
  self.assertNotEqual(p.returncode,0);self.assertIn('PLATFORM_CHANGED',p.stdout);self.assertEqual(file.stat().st_mode&0o777,0o700);self.intact()
 def test_wrong_nonce_never_signals(self):
  p=self.prepared('broray_ops_call platform-preflight-stop-generation "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" 11111111111111111111111111111111 "$TEST_GENERATION" "$TEST_CURRENT_SHA"')
  self.assertNotEqual(p.returncode,0);self.assertIn('OWNER_CHANGED',p.stdout);self.intact()
 def test_foreign_pid_projection_never_signals_either_process(self):
  foreign=subprocess.Popen(['/bin/ash','-c','while :; do sleep 2; done'],start_new_session=True);self.processes.append(foreign)
  marker=self.updater/'daemon.pid';marker.write_text(str(foreign.pid)+'\n');before=marker.read_bytes();p=self.stop()
  self.assertNotEqual(p.returncode,0);self.assertIn('UPDATER_SERVICE_UNCONFIRMED',p.stdout);self.assertIsNone(foreign.poll());self.assertEqual(marker.read_bytes(),before);self.intact()
 def test_foreign_script_argument_is_not_exempted_with_supervisor(self):
  script=self.root/'opt/libexec/broray-updater/broray-updater.sh'
  foreign=subprocess.Popen(['/bin/ash','-c','while :; do sleep 2; done',str(script),'daemon'],start_new_session=True);self.processes.append(foreign)
  p=self.stop();self.assertNotEqual(p.returncode,0);self.assertIn('UPDATER_SERVICE_UNCONFIRMED',p.stdout);self.assertIsNone(foreign.poll());self.intact()
 def test_corrupt_generation_evidence_never_becomes_stopped(self):
  anchor=self.domain/'state.json';anchor.write_bytes(b'{broken');p=self.stop()
  self.assertNotEqual(p.returncode,0);self.assertEqual(anchor.read_bytes(),b'{broken');self.assertNotEqual(self.native.wait(timeout=3),0)
  self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT');self.assertTrue(self.lock.is_symlink())
 def test_legacy_or_unknown_record_cannot_authorize_new_path(self):
  p=self.prepared('printf "{unknown" >"$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/platform-service.json"\nbroray_ops_preflight_stop_generation "$TEST_GENERATION" "$TEST_CURRENT_SHA"')
  self.assertNotEqual(p.returncode,0);self.assertEqual((self.operation()/'platform-service.json').read_bytes(),b'{unknown');self.intact()
 def test_completed_stop_reply_replays_without_new_native_revision(self):
  self.env['TEST_DOMAIN']=str(self.domain)
  p=self.stop('sha256sum "$TEST_DOMAIN"/revision-*.json >"$TEST_HOME/before-replay"\nbroray_ops_preflight_stop_generation "$TEST_GENERATION" "$TEST_CURRENT_SHA" || exit $?\nsha256sum "$TEST_DOMAIN"/revision-*.json >"$TEST_HOME/after-replay"')
  self.assertEqual(p.returncode,0,p.stdout+p.stderr);self.assertEqual((self.home/'before-replay').read_bytes(),(self.home/'after-replay').read_bytes())
  replies=[json.loads(row) for row in p.stdout.splitlines()];self.assertEqual(len(replies),2);self.assertEqual(replies[0],replies[1]);self.assertTrue(self.lock.is_symlink())
 def test_lost_stopped_reply_is_idempotent(self):
  source=(CODE/'lib/operation-client.sh').read_text().replace('broray_ops_call()','broray_ops_call_original()',1)
  shim=self.home/'lost-reply.sh';shim.write_text(source+'''
broray_ops_call() {
 local output rc
 rc=0; output="$(broray_ops_call_original "$@")" || rc=$?
 if [ "$1" = platform-preflight-stop-generation ] && [ "$rc" = 0 ] && [ ! -e "$TEST_HOME/dropped" ] &&
   printf '%s\\n' "$output" | jq -e '.phase=="STOPPED"' >/dev/null; then
  : >"$TEST_HOME/dropped"; return 0
 fi
 [ -z "$output" ] || printf '%s\\n' "$output"
 return "$rc"
}
''')
  p=self.stop(prefix='. "'+str(shim)+'"');self.assertEqual(p.returncode,0,p.stdout+p.stderr);self.assertTrue((self.home/'dropped').exists());self.assertEqual(json.loads(p.stdout)['phase'],'STOPPED')
  self.assertEqual(self.latest()['stopOperationId'],self.operation().name);self.assertEqual(self.latest()['children'],[])

if __name__=='__main__':
 names=[n for n in GenerationStopAdversarial.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(GenerationStopAdversarial(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)
