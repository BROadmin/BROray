"""Failed pre-switch compensation retains evidence until verified restoration."""
import json,os,subprocess,unittest
from test_updater_service_rollback_recovery import ServiceRollbackRecovery,UPDATER

class PreSwitchRestore(unittest.TestCase):
 def setUp(self):
  self.fixture=ServiceRollbackRecovery();self.fixture.setUp();self.addCleanup(self.fixture.doCleanups)
  f=self.fixture
  for name in ['mutation.started','switch.phase','rollback-reason']:(f.op/name).unlink()
  f.state.update(error='PRE_SWITCH_SERVICE_RESTORE_FAILED',mutationStarted=False,rollbackPerformed=False)
  f.write_state();(f.app/'lib').mkdir();(f.app/'lib/service-lifecycle.sh').touch()
 def invoke(self,healthy=True,drained=True):
  f=self.fixture;text=UPDATER.read_text();self.assertTrue(text.endswith('main "$@"\n'))
  tail='''
service_wait_cooperative_stop(){ printf 'drain:%s\n' "$1" >>"$CALLS"; [ "$DRAINED" = true ]; }
services_start_captured(){ printf 'start\n' >>"$CALLS"; }
services_health_captured(){ printf 'health\n' >>"$CALLS"; [ "$HEALTHY" = true ]; }
recover_incomplete
'''
  script=f.root/'driver.sh';script.write_text(text[:-len('main "$@"\n')]+tail)
  return subprocess.run(['/bin/ash',str(script)],env=os.environ|{'BRORAY_UPDATER_ROOT_PREFIX':str(f.root),'CALLS':str(f.calls),'HEALTHY':str(healthy).lower(),'DRAINED':str(drained).lower()},capture_output=True,text=True,timeout=10)
 def test_exact_preswitch_failure_restores_before_unlock(self):
  f=self.fixture;r=self.invoke();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  state=json.loads((f.op/'state.json').read_bytes());self.assertEqual(state['stage'],'services-restored')
  self.assertFalse(state['mutationStarted']);self.assertFalse(state['rollbackPerformed'])
  self.assertFalse(f.lock.exists());self.assertFalse((f.up/'queue/reinstall-fixture.json').exists())
  self.assertEqual((f.current/'.broray-slot').read_text(),'old-slot\n')
  calls=f.calls.read_text();self.assertIn('drain:S24broray',calls);self.assertTrue(calls.endswith('start\nhealth\n'))
  r=self.invoke();self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(f.calls.read_text(),calls)
 def test_failed_health_retains_exact_state_and_fence(self):
  f=self.fixture;before=(f.op/'state.json').read_bytes();r=self.invoke(healthy=False)
  self.assertNotEqual(r.returncode,0);self.assertEqual((f.op/'state.json').read_bytes(),before);self.assertTrue(f.lock.exists())
 def test_unknown_drain_blocks_start(self):
  f=self.fixture;r=self.invoke(drained=False);self.assertNotEqual(r.returncode,0)
  self.assertNotIn('start\n',f.calls.read_text());self.assertTrue(f.lock.exists())
 def test_changed_layout_and_foreign_fence_have_no_action(self):
  f=self.fixture
  for path,value in [(f.current/'.broray-slot','foreign'),(f.lock/'operation-id','foreign'),(f.op/'mutation.started','yes'),(f.op/'switch.phase','unknown')]:
   with self.subTest(path=path.name):
    before=path.read_bytes() if path.exists() else None;path.write_text(value)
    r=self.invoke();self.assertNotEqual(r.returncode,0);self.assertFalse(f.calls.exists());self.assertTrue(f.lock.exists())
    if before is None:path.unlink()
    else:path.write_bytes(before)

class StopAbort(unittest.TestCase):
 def test_stop_failure_must_not_discard_failed_restoration(self):
  text=UPDATER.read_text();a=text.index('    if ! services_stop_captured; then');b=text.index('\n    state_seed "$slot_root"',a)
  branch=text[a:b]
  a=text.index('\nstate_seed_abort_before_switch()\n');b=text.index('\nservice_call()\n',a)
  funcs=text[a:b]
  code='''set -u
operation=update
CURRENT_OPERATION_LOG=/dev/null
services_stop_captured(){ return 1; }
services_start_captured(){ printf 'RESTORE_FAILED\n'; return 1; }
state_seed_rollback(){ return 0; }
status_write(){ printf 'STATE=%s/%s/%s\n' "$2" "$3" "$6"; }
operation_log(){ :; }
operation_cleanup_terminal(){ printf 'UNSAFE_UNLOCK\n'; }
'''+funcs+'\nprobe(){\n'+branch+'\n}\nprobe\n'
  r=subprocess.run(['/bin/ash','-c',code],capture_output=True,text=True,timeout=5)
  self.assertNotEqual(r.returncode,0);self.assertNotIn('UNSAFE_UNLOCK',r.stdout)
  self.assertIn('STATE=error/rollback-failed/PRE_SWITCH_SERVICE_RESTORE_FAILED',r.stdout)

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
