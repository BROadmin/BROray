"""Canonical protected operation owns migration staging; never legacy STOPPED."""
import hashlib,json,subprocess,unittest
from test_preflight_service_binding import ServiceBinding,CODE
from test_updater_generation import GEN

class MigrationStaging(ServiceBinding):
 def setUp(self):
  super().setUp();self.env['BRORAY_OPS_GENERATION']=GEN
  self.env['TEST_SHA']=hashlib.sha256((CODE/'share/updater-platform/SHA256SUMS').read_bytes()).hexdigest()
 def stage(self,tail='',prefix=''):
  return self.run_binding('broray_ops_preflight_stage_legacy || exit $?\n'+tail,prefix)
 def stagepath(self):return self.operation()/'platform-migration'
 def test_live_legacy_staged_with_fence_and_no_stop(self):
  service,script=self.start_service();before=script.read_bytes();r=self.stage()
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);reply=json.loads(r.stdout)
  self.assertEqual(reply['phase'],'REBOOT_REQUIRED');self.assertFalse(reply['serviceStopped']);self.assertFalse(reply['platformReady']);self.assertFalse(reply['activationAllowed'])
  self.assertTrue(self.lock.is_symlink());self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT')
  self.assertEqual(script.read_bytes(),before);self.assertIsNone(service.poll());self.assertTrue((self.stagepath()/'staged.receipt').exists())
 def test_absent_legacy_still_requires_reboot(self):
  r=self.stage();self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(json.loads(r.stdout)['phase'],'REBOOT_REQUIRED');self.assertFalse(json.loads(r.stdout)['serviceStopped'])
 def test_same_owner_repeat_exact_bytes(self):
  self.start_service();r=self.stage('broray_ops_preflight_stage_legacy')
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);rows=[json.loads(x) for x in r.stdout.splitlines()];self.assertEqual(len(rows),2);self.assertEqual(rows[0],rows[1])
 def test_generic_finish_cannot_remove_migration_fence(self):
  self.start_service();r=self.stage('broray_ops_finish completed')
  self.assertNotEqual(r.returncode,0);self.assertTrue(self.lock.is_symlink());self.assertTrue((self.stagepath()/'intent.record').exists())
 def test_legacy_stop_still_refused_after_staging(self):
  service,_=self.start_service();r=self.stage('broray_ops_preflight_stop_service')
  self.assertNotEqual(r.returncode,0);self.assertIsNone(service.poll());self.assertNotEqual(self.readstate()['platformPreflight']['phase'],'STOPPED');self.assertTrue(self.lock.is_symlink())
 def test_foreign_caller_cannot_stage(self):
  r=self.run_binding('broray_ops_call platform-preflight-migration-stage "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" 2147483646 "$BRORAY_PREFLIGHT_STOP_NONCE"')
  self.assertNotEqual(r.returncode,0);self.assertFalse(self.stagepath().exists())
 def test_wrong_nonce_cannot_stage(self):
  r=self.run_binding('broray_ops_call platform-preflight-migration-stage "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" 11111111111111111111111111111111')
  self.assertNotEqual(r.returncode,0);self.assertFalse(self.stagepath().exists())
 def test_unbound_service_cannot_stage(self):
  r=self.shell('broray_ops_preflight_admit "$TEST_SHA" || exit $?; broray_ops_preflight_stop_intent "$TEST_SHA" || exit $?; broray_ops_preflight_stage_legacy')
  self.assertNotEqual(r.returncode,0);self.assertFalse(self.stagepath().exists())
 def test_changed_live_service_cannot_stage(self):
  service,script=self.start_service();r=self.run_binding('printf CHANGED >>"$BRORAY_ROOT/../libexec/broray-updater/broray-updater.sh"; broray_ops_preflight_stage_legacy')
  self.assertNotEqual(r.returncode,0);self.assertIsNone(service.poll());self.assertTrue(script.read_text().endswith('CHANGED'));self.assertFalse(self.stagepath().exists())
 def test_missing_native_does_not_create_stage(self):
  self.env['BRORAY_OPS_GENERATION']=str(self.home/'missing-native');r=self.stage();self.assertNotEqual(r.returncode,0);self.assertFalse(self.stagepath().exists())
 def test_lost_stage_reply_uses_one_exact_intent(self):
  source=(CODE/'lib/operation-client.sh').read_text().replace('broray_ops_call()','broray_ops_call_original()',1)
  shim=self.home/'stage-shim.sh';shim.write_text(source+'''
broray_ops_call() {
 local output rc
 rc=0; output="$(broray_ops_call_original "$@")" || rc=$?
 if [ "$1" = platform-preflight-migration-stage ] && [ "$rc" = 0 ] && [ ! -e "$TEST_HOME/dropped" ]; then
  : >"$TEST_HOME/dropped"; return 0
 fi
 [ -z "$output" ] || printf '%s\\n' "$output"
 return "$rc"
}
''')
  self.start_service();r=self.stage(prefix='. "'+str(shim)+'"');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertTrue((self.home/'dropped').exists());self.assertEqual(len(list(self.stagepath().glob('intent*'))),1);self.assertTrue(self.lock.is_symlink())
 def test_corrupt_stage_retry_preserves_evidence_and_fence(self):
  service,_=self.start_service();r=self.stage('printf "{broken" >"$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/platform-migration/intent.record"; broray_ops_preflight_stage_legacy')
  self.assertNotEqual(r.returncode,0);self.assertEqual((self.stagepath()/'intent.record').read_bytes(),b'{broken');self.assertTrue(self.lock.is_symlink());self.assertIsNone(service.poll())
 def test_live_state_payload_cannot_replace_authenticated_code_payload(self):
  wrong=self.live/'share/updater-platform';wrong.mkdir(parents=True);(wrong/'SHA256SUMS').write_text('untrusted')
  r=self.stage();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual((self.stagepath()/'manifest.record').read_bytes(),(CODE/'share/updater-platform/SHA256SUMS').read_bytes())
 def test_stale_staging_cannot_be_recovered_by_generic_finish(self):
  self.assertEqual(self.stage().returncode,0);op=self.operation();before=self.freeze(op)
  r=self.shell('broray_ops_call recover "'+op.name+'"');self.assertNotEqual(r.returncode,0);self.assertEqual(self.freeze(op),before);self.assertTrue(self.lock.is_symlink())

if __name__=='__main__':
 names=[n for n in MigrationStaging.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(MigrationStaging(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)
