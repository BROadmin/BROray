"""Public restart must resume its pending successor, not stop an unborn one."""
import json,unittest
from test_installed_service_cycles import ServiceCycles
from test_generation_migration_crash import trace_string
from test_service_cycle_crash_support import crash_native_caller

class RestartPending(ServiceCycles):
 def guard_args(self):
  if not getattr(self,'intercept_restart',False):return super().guard_args()
  b=json.loads((self.op/'platform-bootguard.json').read_bytes())
  return [str(self.native),'service-cycle-restart',str(self.root/'router'),self.op.name,b['migrationIntentSha256'],b['stopNonce']]
 def test_restart_replays_successor_intent_after_old_stop(self):
  self.installed();first=self.success('start');before=self.files(self.op)
  self.intercept_restart=True
  def match(pid,regs):return regs.orig_rax==265 and trace_string(pid,regs.r10)=='cycle-00000000000000000001.record'
  out,err=crash_native_caller(self,[265],match,'restart-after-old-stop-before-new-intent-link')
  self.assertEqual(out,b'');pending=self.updater/'cycles/cycle-00000000000000000001.record.pending'
  self.assertTrue(pending.is_file());intent=pending.read_bytes();gid=intent.decode().splitlines()[3]
  self.assertTrue((self.updater/'generations'/first['generationId']/'retirement.receipt').is_file())
  self.assertEqual(self.files(self.op),before)
  again=self.success('restart');self.assertEqual(again['generationId'],gid);self.one_live(gid)
  self.assertEqual((self.updater/'cycles/cycle-00000000000000000001.record').read_bytes(),intent)
  self.assertFalse(pending.exists());self.assertEqual(len(list((self.updater/'cycles').glob('cycle-*.record'))),1)
  self.assertEqual(self.files(self.op),before);self.success('stop');self.assertEqual(self.files(self.op),before)
  print('RESTART_RECOVERY_RECEIPT '+json.dumps({'oldGeneration':first['generationId'],'newGeneration':gid,'sameIntent':True,'completedMigrationUnchanged':True}),flush=True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([RestartPending('test_restart_replays_successor_intent_after_old_stop')]))
 raise SystemExit(not r.wasSuccessful())
