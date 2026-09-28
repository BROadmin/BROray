"""Typed service-only rollback retry, preserving all ambiguous failures.

Real updater dispatch/state/queue cleanup; service/slot health are controlled
fixtures here. The same health functions are exercised on the physical router.
"""
from pathlib import Path
import json,os,subprocess,tempfile,unittest
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
UPDATER=ROOT/'runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-updater.sh'

class ServiceRollbackRecovery(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='service-recover-');self.addCleanup(self.tmp.cleanup)
  self.root=Path(self.tmp.name);self.app=self.root/'opt/broray';self.current=self.app/'current';self.current.mkdir(parents=True)
  self.op=self.root/'opt/var/lib/broray/operations/reinstall-fixture';self.op.mkdir(parents=True)
  self.up=self.root/'opt/var/lib/broray-updater';(self.up/'queue').mkdir(parents=True)
  self.lock=self.up/'request.lock';self.lock.mkdir();(self.lock/'operation-id').write_text('reinstall-fixture\n')
  (self.current/'.broray-slot').write_text('old-slot\n');target=self.app/'releases/new-slot';target.mkdir(parents=True);(target/'.broray-slot').write_text('new-slot\n')
  (self.op.parent.parent/'last-operation').write_text('reinstall-fixture\n')
  for n,b in [('previous-slot','old-slot\n'),('target','new-slot\n'),('rollback-reason','RECOVERY_TARGET_HEALTH_FAILED\n'),('switch.phase','rollback-previous-active\n'),('mutation.started','yes\n')]: (self.op/n).write_text(b)
  req=json.dumps({'schemaVersion':1,'operationId':'reinstall-fixture','operation':'reinstall','target':{}})
  (self.op/'request.json').write_text(req);(self.up/'queue/reinstall-fixture.json').write_text(req)
  (self.op/'services.tsv').write_text(''.join(x+'\trunning\n' for x in ['S23broray-monitor','S24broray','S25broray-web','S27broray-auto-switch','S28broray-subscriptions']))
  self.state=dict(schemaVersion=1,operationId='reinstall-fixture',operation='reinstall',state='error',stage='rollback-failed',error='ROLLBACK_SERVICE_FAILED',running=False,mutationStarted=True,rollbackPerformed=True)
  self.write_state();self.calls=self.root/'calls'
 def write_state(self):(self.op/'state.json').write_text(json.dumps(self.state))
 def invoke(self,healthy=True):
  text=UPDATER.read_text();self.assertTrue(text.endswith('main "$@"\n'))
  tail='''
services_start_captured() { printf 'start\\n' >>"$CALLS"; }
slot_health() { printf 'health\\n' >>"$CALLS"; [ "$HEALTHY" = true ]; }
recover_incomplete
'''
  script=self.root/'driver.sh';script.write_text(text[:-len('main "$@"\n')]+tail)
  return subprocess.run(['/bin/ash',str(script)],env=os.environ|{'BRORAY_UPDATER_ROOT_PREFIX':str(self.root),'CALLS':str(self.calls),'HEALTHY':str(healthy).lower()},capture_output=True,text=True,timeout=10)
 def test_exact_service_failure_recovers_and_reply_replay_has_no_service_action(self):
  r=self.invoke();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  state=json.loads((self.op/'state.json').read_bytes());self.assertEqual(state['stage'],'rolled-back');self.assertFalse(state['running']);self.assertTrue(state['rollbackPerformed'])
  self.assertFalse(self.lock.exists());self.assertFalse((self.up/'queue/reinstall-fixture.json').exists())
  self.assertEqual(self.calls.read_text(),'start\nhealth\n')
  self.assertEqual(json.loads((self.op/'service-recovery.before.json').read_bytes()),self.state)
  r=self.invoke();self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.calls.read_text(),'start\nhealth\n')
 def test_failed_health_preserves_failed_state_and_fence(self):
  before=(self.op/'state.json').read_bytes();r=self.invoke(False);self.assertNotEqual(r.returncode,0)
  self.assertEqual((self.op/'state.json').read_bytes(),before);self.assertTrue(self.lock.exists());self.assertTrue((self.up/'queue/reinstall-fixture.json').exists())
  self.assertEqual(self.calls.read_text(),'start\nhealth\n')
 def test_wrong_layout_or_foreign_request_has_no_action(self):
  for kind in ['slot','queue','lock','services','seed']:
   with self.subTest(kind=kind):
    changed={'slot':self.current/'.broray-slot','queue':self.up/'queue/reinstall-fixture.json','lock':self.lock/'operation-id','services':self.op/'services.tsv','seed':self.op/'state-seed.created'}[kind]
    before=changed.read_bytes() if changed.exists() else None;changed.write_text('unknown\n')
    try:
     state=(self.op/'state.json').read_bytes();r=self.invoke();self.assertNotEqual(r.returncode,0);self.assertFalse(self.calls.exists());self.assertEqual((self.op/'state.json').read_bytes(),state);self.assertTrue(self.lock.exists())
    finally:
     if before is None:changed.unlink()
     else:changed.write_bytes(before)
 def test_other_failure_or_incomplete_proof_remains_blocked(self):
  for change in [{'error':'ROLLBACK_LAYOUT_FAILED'},{'error':'ROLLBACK_HEALTH_FAILED'},{'rollbackPerformed':False},{'mutationStarted':False},{'schemaVersion':7}]:
   with self.subTest(change=change):
    old=self.state.copy();self.state.update(change);self.write_state()
    r=self.invoke();self.assertNotEqual(r.returncode,0);self.assertFalse(self.calls.exists());self.assertTrue(self.lock.exists());self.assertEqual(json.loads((self.op/'state.json').read_bytes()),self.state)
    self.state=old

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(ServiceRollbackRecovery));raise SystemExit(not r.wasSuccessful())
