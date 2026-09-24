"""A READY replacement commits exactly once before its protected fence retires."""
import json,unittest
from test_updater_replacement_start import ReplacementStart

class ReplacementCommit(ReplacementStart):
 def test_commit_and_completion_require_exact_readiness_and_preserve_evidence(self):
  self.test_replacement_starts_new_generation_with_bound_readiness()
  run=self.replacement_run;op=self.replacement_operation;ready=self.replacement_ready
  fence=self.root/'opt/var/lock/broray/global-operation.lock'
  state=(op/'state.json').read_bytes()
  before=run('platform-replacement-complete')
  self.assertNotEqual(before.returncode,0,'READY alone must not complete a replacement')
  self.assertEqual((op/'state.json').read_bytes(),state);self.assertEqual(fence.readlink(),op/'fence')
  r=run('platform-replacement-commit')
  print('REPLACEMENT_COMMIT '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);committed=json.loads(r.stdout)
  self.assertEqual(committed['phase'],'COMMITTED');self.assertTrue(committed['platformReady'])
  self.assertFalse(committed['activationAllowed']);self.assertEqual(committed['generationId'],ready['generationId'])
  receipt=op/'platform-replacement-committed.record';body=receipt.read_bytes()
  anchor=op/'platform-replacement-committed.anchor';anchor_body=anchor.read_bytes()
  r=run('platform-replacement-commit');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(json.loads(r.stdout)['commitReceiptSha256'],committed['commitReceiptSha256'])
  self.assertEqual(receipt.read_bytes(),body);self.assertEqual(anchor.read_bytes(),anchor_body)
  self.assertEqual((op/'state.json').read_bytes(),state);self.assertEqual(fence.readlink(),op/'fence')
  try:
   receipt.write_bytes(b'{corrupt commit')
   r=run('platform-replacement-complete');self.assertNotEqual(r.returncode,0)
   self.assertEqual(receipt.read_bytes(),b'{corrupt commit');self.assertEqual((op/'state.json').read_bytes(),state)
   self.assertEqual(fence.readlink(),op/'fence')
   receipt.unlink()
   r=run('platform-replacement-commit');self.assertNotEqual(r.returncode,0)
   self.assertFalse(receipt.exists());self.assertEqual(anchor.read_bytes(),anchor_body)
  finally:receipt.write_bytes(body);receipt.chmod(0o600)
  r=run('platform-replacement-complete')
  print('REPLACEMENT_COMPLETE '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);complete=json.loads(r.stdout)
  self.assertEqual(complete['phase'],'PREFLIGHT_COMPLETED');self.assertTrue(complete['platformReady'])
  self.assertEqual(complete['generationId'],ready['generationId'])
  self.assertFalse(fence.exists());self.assertFalse(fence.is_symlink());self.assertEqual((op/'retired-lock').readlink(),op/'fence')
  state=(op/'state.json').read_bytes();self.assertFalse(json.loads(state)['running'])
  self.assertEqual(json.loads(state)['state'],'completed')
  r=run('platform-replacement-complete');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(json.loads(r.stdout)['generationId'],ready['generationId'])
  self.assertEqual((op/'state.json').read_bytes(),state);self.assertEqual(receipt.read_bytes(),body)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  ReplacementCommit('test_commit_and_completion_require_exact_readiness_and_preserve_evidence')]))
 raise SystemExit(not result.wasSuccessful())
