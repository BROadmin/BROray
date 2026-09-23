"""Interrupt actual public stop after updater retirement, before settlement."""
import json,os,signal,subprocess,time,unittest
from pathlib import Path
from test_public_stop_trace import PublicStopTrace
from test_installed_init_stop import InstalledInitStop

class InterruptedPublicStop(InstalledInitStop):
 def init(self,verb):
  if verb!='stop' or getattr(self,'interrupted',False):return super().init(verb)
  self.interrupted=True
  command=['/bin/ash',str(self.root/'router/opt/etc/init.d/S22broray-updater'),'stop']
  env={**os.environ,'BRORAY_UPDATER_ROOT_PREFIX':str(self.root/'router')}
  traced=PublicStopTrace(self,command,env);cut=False
  try:
   deadline=time.monotonic()+120
   guard=traced.wait_exec(lambda argv:len(argv)>1 and argv[1]==b'service-stop-guard',deadline)
   lock=self.root/'router/opt/var/lock/broray/global-operation.lock'
   self.assertTrue(lock.is_symlink());op=Path(os.readlink(lock)).parent
   state=json.loads((op/'state.json').read_bytes());self.assertTrue(state['running'])
   self.assertEqual(state['platformPreflight']['phase'],'STOPPED')
   self.assertEqual(state['serviceStop']['originOperationId'],self.op.name)
   count=sorted(p.name for p in self.op.parent.glob('op-*'))
   before=self.snapshot();busy=super().init('stop')
   self.assertNotEqual(busy.returncode,0,'live coordinator must be preserved')
   self.assertEqual(self.snapshot(),before,'busy public request mutated state')
   killed=traced.kill_command();cut=True
   self.assertEqual(killed.returncode,-signal.SIGKILL,killed.stdout+killed.stderr)
   print('PUBLIC_STOP_INTERRUPTED '+json.dumps({'operation':op.name,'phase':'STOPPED','ownerExit':killed.returncode,'fencePreserved':lock.is_symlink(),'actualGuardPid':guard,'capturedCommandTreeDrained':not traced.tasks}),flush=True)
   foreign=self.op/'fence/foreign-recovery-fixture'
   self.assertFalse(foreign.exists());foreign.write_bytes(b'PRESERVE-ORIGIN-EVIDENCE');foreign.chmod(0o600)
   try:
    before=self.snapshot();refused=super().init('stop')
    self.assertNotEqual(refused.returncode,0,'corrupt original fence must refuse')
    self.assertEqual(self.snapshot(),before,'refusal changed original/current evidence')
   finally:foreign.unlink()
   result=super().init('stop')
   print('PUBLIC_STOP_RESUMED '+json.dumps({'returnCode':result.returncode,'stdout':result.stdout,'stderr':result.stderr}),flush=True)
   self.assertEqual(sorted(p.name for p in self.op.parent.glob('op-*')),count,'recovery creates another operation')
   return result
  finally:traced.close()

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([InterruptedPublicStop('test_public_stop_preserves_origin_platform_and_foreign_process')]))
 raise SystemExit(not r.wasSuccessful())
