"""Canonical preparation must retain its recovery closure before live guards."""
import json,unittest
from test_preflight_bootguard import PreflightBootguard
from test_generation_recovery_code import FILES,CODE

class PreflightRecoveryCode(PreflightBootguard):
 def retained(self):return self.operation()/'platform-recovery-code'
 def test_guard_staging_retains_exact_authenticated_code(self):
  service,_=self.start_service();r=self.guarded();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertTrue((self.retained()/'staged.receipt').is_file(),'recovery coordinator depends on temporary CODE_ROOT')
  for rel in FILES:self.assertEqual((self.retained()/'code'/rel).read_bytes(),(CODE/rel).read_bytes())
  binding=json.loads((self.operation()/'platform-recovery-code.json').read_bytes())
  self.assertEqual(binding['operationId'],self.operation().name);self.assertEqual(binding['stopNonce'],self.bound()['stopNonce'])
  self.assertFalse(binding['processAuthority']);self.assertFalse(binding['activationAllowed']);self.assertIsNone(service.poll());self.assertTrue(self.lock.is_symlink())
 def test_corrupt_retained_code_refuses_replay(self):
  self.start_service();tail='file="$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/platform-recovery-code/code/lib/operation-coordinator.sh"; [ -f "$file" ] || exit 99; printf "{broken" >"$file"; broray_ops_preflight_stage_bootguard'
  r=self.guarded(tail);self.assertNotEqual(r.returncode,0);self.assertNotEqual(r.returncode,99)
  self.assertEqual((self.retained()/'code/lib/operation-coordinator.sh').read_bytes(),b'{broken');self.assertTrue(self.lock.is_symlink())
 def test_missing_whole_closure_and_binding_are_not_recreated(self):
  self.start_service();tail='dir="$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/platform-recovery-code"; [ -f "$dir/staged.receipt" ] || exit 99; rm -rf "$dir"; rm "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/platform-recovery-code.json"; broray_ops_preflight_stage_bootguard'
  r=self.guarded(tail);self.assertNotEqual(r.returncode,0);self.assertNotEqual(r.returncode,99)
  self.assertFalse(self.retained().exists());self.assertFalse((self.operation()/'platform-recovery-code.json').exists());self.assertTrue(self.lock.is_symlink())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(PreflightRecoveryCode(n) for n in PreflightRecoveryCode.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
