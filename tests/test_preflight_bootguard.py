"""Only the authenticated protected owner can stage the legacy boot guards."""
import hashlib,json,shutil,subprocess,unittest
from test_preflight_migration_staging import MigrationStaging,CODE

class PreflightBootguard(MigrationStaging):
 def setUp(self):
  super().setUp()
  # Real updater ensure_layout makes STATE_ROOT private before daemon start.
  # A generic mkdir's 0755 is not the installed updater's ownership contract.
  self.updater.mkdir(parents=True,exist_ok=True,mode=0o700)
  payload=CODE/'share/updater-platform/opt'
  for rel in ['bin/broray-updaterctl','etc/init.d/S22broray-updater','libexec/broray-updater/broray-compat.sh','libexec/broray-updater/broray-migrate-legacy.sh','libexec/broray-updater/broray-updater.sh','libexec/broray-updater/minisign','libexec/broray-updater/xray-wrapper']:
   target=self.live.parent/rel;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(payload/rel,target);target.chmod(0o755)
 def guarded(self,tail='',prefix=''):
  return self.run_binding('broray_ops_preflight_stage_bootguard || exit $?\n'+tail,prefix)
 def guardpath(self):return self.operation()/'platform-bootguard'
 def test_live_owner_guarded_without_signal_or_stopped(self):
  service,script=self.start_service();original=script.read_bytes();r=self.guarded()
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);reply=json.loads(r.stdout)
  self.assertEqual(reply['phase'],'BOOT_GUARDS_STAGED');self.assertFalse(reply['serviceStopped']);self.assertFalse(reply['platformReady']);self.assertFalse(reply['activationAllowed']);self.assertFalse(reply['signalsAuthorized'])
  self.assertIsNone(service.poll());self.assertTrue(self.lock.is_symlink());self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT')
  self.assertEqual((self.guardpath()/'before-5').read_bytes(),original);self.assertTrue((self.guardpath()/'runtime').exists())
  self.assertIn('MIGRATION_ACTIVATION_PENDING',script.read_text())
  binding=json.loads((self.operation()/'platform-bootguard.json').read_text());self.assertEqual(binding['operationId'],self.operation().name)
  self.assertEqual(binding['migrationIntentSha256'],hashlib.sha256((self.stagepath()/'intent.record').read_bytes()).hexdigest())
 def test_repeat_preserves_binding_and_live_legacy(self):
  service,_=self.start_service();r=self.guarded('broray_ops_preflight_stage_bootguard')
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);rows=[json.loads(x) for x in r.stdout.splitlines()];self.assertEqual(len(rows),2);self.assertEqual(rows[0],rows[1]);self.assertIsNone(service.poll())
 def test_generic_finish_cannot_clear_guarded_transaction(self):
  self.start_service();r=self.guarded('broray_ops_finish completed');self.assertNotEqual(r.returncode,0);self.assertTrue(self.lock.is_symlink());self.assertTrue((self.guardpath()/'staged.receipt').exists())
 def test_wrong_nonce_no_entry_change(self):
  service,script=self.start_service();before=script.read_bytes()
  r=self.run_binding('broray_ops_call platform-preflight-bootguard-stage "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" 11111111111111111111111111111111')
  self.assertNotEqual(r.returncode,0);self.assertEqual(script.read_bytes(),before);self.assertFalse(self.guardpath().exists());self.assertIsNone(service.poll())
 def test_foreign_owner_no_entry_change(self):
  service,script=self.start_service();before=script.read_bytes()
  r=self.run_binding('broray_ops_call platform-preflight-bootguard-stage "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" 2147483646 "$BRORAY_PREFLIGHT_STOP_NONCE"')
  self.assertNotEqual(r.returncode,0);self.assertEqual(script.read_bytes(),before);self.assertFalse(self.guardpath().exists());self.assertIsNone(service.poll())
 def test_corrupt_binding_cannot_be_replaced(self):
  self.start_service();r=self.guarded('printf "{broken" >"$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/platform-bootguard.json"; broray_ops_preflight_stage_bootguard')
  self.assertNotEqual(r.returncode,0);self.assertEqual((self.operation()/'platform-bootguard.json').read_bytes(),b'{broken');self.assertTrue(self.lock.is_symlink())
 def test_missing_guard_not_recreated_by_coordinator(self):
  service,script=self.start_service();r=self.guarded('rm "$BRORAY_ROOT/../libexec/broray-updater/broray-updater.sh"; broray_ops_preflight_stage_bootguard')
  self.assertNotEqual(r.returncode,0);self.assertFalse(script.exists());self.assertTrue(self.lock.is_symlink());self.assertIsNone(service.poll())
 def test_lost_reply_same_binding_no_second_mutation(self):
  source=(CODE/'lib/operation-client.sh').read_text().replace('broray_ops_call()','broray_ops_call_original()',1)
  shim=self.home/'guard-shim.sh';shim.write_text(source+'''
broray_ops_call() {
 local output rc
 rc=0; output="$(broray_ops_call_original "$@")" || rc=$?
 if [ "$1" = platform-preflight-bootguard-stage ] && [ "$rc" = 0 ] && [ ! -e "$TEST_HOME/dropped" ]; then
  : >"$TEST_HOME/dropped"; return 0
 fi
 [ -z "$output" ] || printf '%s\\n' "$output"
 return "$rc"
}
''')
  self.start_service();r=self.guarded(prefix='. "'+str(shim)+'"');self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertTrue((self.home/'dropped').exists());self.assertTrue(self.lock.is_symlink())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(PreflightBootguard(n) for n in PreflightBootguard.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
