"""Recover only complete staging before any bootguard/live platform mutation."""
import json,os,subprocess,unittest
from test_preflight_bootguard import PreflightBootguard
from test_preflight_admission import CODE,GUARD

class StagedRecovery(PreflightBootguard):
 def discard(self,op=None):
  op=op or self.operation()
  nonce=json.loads((op/'state.json').read_text())['platformPreflight']['stopNonce']
  return self.shell('broray_ops_call platform-preflight-discard-stage "'+op.name+'" "'+nonce+'"')
 def test_stale_complete_stage_retires_only_its_fence(self):
  service,script=self.start_service();original=script.read_bytes()
  prepared=self.stage();self.assertEqual(prepared.returncode,0,prepared.stdout+prepared.stderr)
  op=self.operation();before=self.freeze(op)
  result=self.discard(op)
  self.assertEqual(result.returncode,0,result.stdout+result.stderr)
  reply=json.loads(result.stdout)
  self.assertEqual(reply['phase'],'PREFLIGHT_STAGING_ABORTED')
  for key in ['platformReady','serviceStopped','activationAllowed','signalsAuthorized']:
   self.assertFalse(reply[key])
  self.assertFalse(self.lock.exists());self.assertFalse(self.lock.is_symlink())
  self.assertTrue((op/'retired-lock').is_symlink())
  self.assertEqual(json.loads((op/'state.json').read_text())['state'],'aborted')
  for name,data in before.items():
   if name!='state.json':self.assertEqual((op/name).read_bytes(),data,name)
  self.assertEqual(script.read_bytes(),original);self.assertIsNone(service.poll())
 def test_lost_reply_replay_is_idempotent(self):
  self.start_service();self.assertEqual(self.stage().returncode,0);op=self.operation()
  first=self.discard(op);self.assertEqual(first.returncode,0,first.stdout+first.stderr)
  before=self.freeze(op);again=self.discard(op)
  self.assertEqual(again.returncode,0,again.stdout+again.stderr)
  self.assertTrue(json.loads(again.stdout)['replayed']);self.assertEqual(self.freeze(op),before)
 def test_live_original_owner_cannot_discard(self):
  service,_=self.start_service()
  r=self.stage('broray_ops_call platform-preflight-discard-stage "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_PREFLIGHT_STOP_NONCE"')
  self.assertNotEqual(r.returncode,0);self.assertTrue(self.lock.is_symlink());self.assertIsNone(service.poll())
  self.assertEqual(self.readstate()['state'],'running')
 def test_missing_corrupt_or_extra_evidence_is_preserved(self):
  service,_=self.start_service();self.assertEqual(self.stage().returncode,0);op=self.operation()
  receipt=self.stagepath()/'staged.receipt';saved=receipt.read_bytes()
  for damage in ['missing','corrupt','unknown-helper']:
   with self.subTest(damage=damage):
    if damage=='missing':receipt.unlink()
    elif damage=='corrupt':receipt.write_bytes(b'{broken')
    else:(op/'children.json').write_bytes(b'{}')
    before=self.freeze(op);r=self.discard(op)
    self.assertNotEqual(r.returncode,0);self.assertEqual(self.freeze(op),before)
    self.assertTrue(self.lock.is_symlink());self.assertIsNone(service.poll())
    if damage=='unknown-helper':(op/'children.json').unlink()
    else:receipt.write_bytes(saved);receipt.chmod(0o600)
 def test_changed_platform_and_legacy_fence_are_preserved(self):
  service,script=self.start_service();self.assertEqual(self.stage().returncode,0);op=self.operation()
  before=self.freeze(op);original=script.read_bytes();script.write_bytes(original+b'# foreign\n')
  r=self.discard(op);self.assertNotEqual(r.returncode,0)
  self.assertEqual(script.read_bytes(),original+b'# foreign\n');self.assertEqual(self.freeze(op),before)
  script.write_bytes(original);self.legacy.parent.mkdir(parents=True,exist_ok=True);self.legacy.mkdir()
  r=self.discard(op);self.assertNotEqual(r.returncode,0)
  self.assertTrue(self.legacy.is_dir());self.assertTrue(self.lock.is_symlink());self.assertEqual(self.freeze(op),before)
  self.assertIsNone(service.poll())
 def test_guarded_migration_is_never_discarded(self):
  service,_=self.start_service();r=self.guarded();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  op=self.operation();before=self.freeze(op)
  r=self.discard(op);self.assertNotEqual(r.returncode,0)
  self.assertEqual(self.freeze(op),before);self.assertTrue(self.lock.is_symlink());self.assertIsNone(service.poll())
 def test_pid_reuse_never_authorizes_discard(self):
  self.start_service();self.assertEqual(self.stage().returncode,0);op=self.operation()
  r=self.shell('OPS_PROC=/proc; OPS_APP="$BRORAY_ROOT"; . "$BRORAY_OPS_CODE_ROOT/lib/operation-owner.sh"; broray_ops_capture_owner '+str(os.getpid()));self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  foreign=json.loads(r.stdout);foreign['startTicks']=str(int(foreign['startTicks'])-1)
  owner=json.loads((op/'owner.json').read_text());owner['owner']=foreign
  for f in [op/'owner.json',op/'fence/owner.json']:f.write_text(json.dumps(owner)+'\n')
  before=self.freeze(op);r=self.discard(op)
  self.assertNotEqual(r.returncode,0);self.assertEqual(self.freeze(op),before);self.assertTrue(self.lock.is_symlink())
 def test_retry_after_terminal_state_before_retirement(self):
  self.start_service();self.assertEqual(self.stage().returncode,0);op=self.operation()
  # Enter the real coordinator guard directly for crash injection. The legacy
  # daemon's exact /bin/ash argv must stay unchanged; test-mode client adds a
  # BusyBox applet argument and is not the right transport for this fixture.
  nonce=json.loads((op/'state.json').read_text())['platformPreflight']['stopNonce']
  env={**self.env,'BRORAY_OPS_TEST':'1','BRORAY_OPS_TEST_LAUNCH_CRASH':'platform-staging-aborted'}
  r=subprocess.run([str(GUARD),str(self.state/'operations.guard'),'/bin/ash',str(CODE/'lib/operation-coordinator.sh'),
                    'platform-preflight-discard-stage',op.name,nonce],env=env,capture_output=True,text=True,timeout=25)
  self.assertNotEqual(r.returncode,0)
  self.assertEqual(json.loads((op/'state.json').read_text())['state'],'aborted',r.stdout+r.stderr);self.assertTrue(self.lock.is_symlink())
  r=self.discard(op);self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertFalse(self.lock.is_symlink())

if __name__=='__main__':
 names=[n for n in StagedRecovery.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(StagedRecovery(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)
