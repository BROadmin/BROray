"""Canonical coordinator may release its fence only after exact live commit proof."""
import json,os,subprocess,unittest
from test_native_platform_retention import PlatformRetention

class PlatformCompletion(PlatformRetention):
 def complete(self,verb='platform-preflight-complete',nonce=None):
  live=self.root/'router';state=live/'opt/var/lib/broray'
  env={**os.environ,'BRORAY_ROOT':str(live/'opt/broray'),'BRORAY_STATE_ROOT':str(state),
   'BRORAY_OPS_CODE_ROOT':str(self.code),'BRORAY_OPS_GUARD':str(self.code/'bin/broray-ops-guard'),
   'BRORAY_ROUTES_API_LOCK':str(live/'opt/var/lock/broray/global-operation.lock'),
   'BRORAY_OPS_UPDATER_ROOT':str(self.updater),'BRORAY_LEGACY_GLOBAL_LOCK':str(live/'tmp/broray-global-operation.lock'),
   'BRORAY_OPS_RAM_ROOT':str(live/'tmp/broray-operations')}
  return subprocess.run([str(self.code/'bin/broray-ops-guard'),str(state/'operations.guard'),
   str(live/'opt/bin/ash'),str(self.code/'lib/operation-coordinator.sh'),verb,self.op.name,nonce or self.e['stopNonce']],
   env=env,capture_output=True,text=True,timeout=30)
 def test_committed_generation_completes_operation_and_exact_replay(self):
  self.prepared();r=self.invoke_phase('recovery-start');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  try:
   r=self.invoke_phase('recovery-commit');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   commit=json.loads(r.stdout);before=self.bytes_now();state=self.op/'state.json';old=state.read_bytes()
   lock=self.root/'router/opt/var/lock/broray/global-operation.lock';self.assertTrue(lock.is_symlink())
   for replay in [False,True]:
    r=self.complete();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
    reply=json.loads(r.stdout);self.assertEqual(reply['phase'],'PREFLIGHT_COMPLETED');self.assertEqual(reply['replayed'],replay)
    self.assertEqual(reply['generationId'],commit['generationId']);self.assertEqual(reply['commitReceiptSha256'],commit['commitReceiptSha256'])
    self.assertTrue(reply['platformReady']);self.assertFalse(reply['activationAllowed'])
    self.assertFalse(lock.exists() or lock.is_symlink());self.assertTrue((self.op/'retired-lock').is_symlink())
    current=json.loads(state.read_bytes());self.assertEqual(current['state'],'completed');self.assertFalse(current['running'])
    self.assertEqual(self.bytes_now(),before);self.assertFalse(self.fetch.exists())
    if replay:self.assertEqual(state.read_bytes(),terminal)
    terminal=state.read_bytes()
   print('PLATFORM_COMPLETION_RECEIPT '+json.dumps({'completed':True,'lostReplyReplayExact':True,'platformUnchanged':True}),flush=True)
  finally:
   # Fixture-only restoration permits the existing protected stop cleanup.
   retired=self.op/'retired-lock'
   if retired.is_symlink() and not lock.is_symlink():retired.rename(lock)
   state.write_bytes(old)
   r=self.invoke_phase('recovery-stop-current');self.assertEqual(r.returncode,0,r.stdout+r.stderr)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([PlatformCompletion('test_committed_generation_completes_operation_and_exact_replay')]))
 raise SystemExit(not r.wasSuccessful())
