"""A genuine new boot must reject truncation of the old immutable ledger."""
import json,unittest
from test_cycle_running_boot_resume import CycleRunningBootResume
class BootRollback(CycleRunningBootResume):
 def test_tail_rollback_after_ready_is_preserved_and_refused(self):
  domain=self.updater/'generations'/self.e['stoppedGeneration']
  saved=(self.updater/'cycles'/('ready-'+self.e['stoppedGeneration']+'.record')).read_text()
  ready=json.loads(saved[saved.index('{'):]);anchor=ready['revision']
  removed=[]
  for p in sorted(domain.glob('revision-*.json')):
   if int(p.name[9:-5])>anchor:removed.append((p,p.read_bytes(),p.stat().st_mode&0o777));p.unlink()
  self.assertTrue(removed,'fixture must contain witnessed history after READY')
  before=self.files(self.op);truncated=self.files(domain)
  try:
   r=self.init('start')
   print('BOOT_ROLLBACK_REPLY '+json.dumps({'rc':r.returncode,'stdout':r.stdout,'stderr':r.stderr,'removed':len(removed)}),flush=True)
   self.assertNotEqual(r.returncode,0,'rollback to an earlier valid ledger prefix must not be admitted')
   self.assertEqual(self.files(domain),truncated);self.assertEqual(self.files(self.op),before)
  finally:
   # Any incorrectly admitted live fixture is drained with the existing full
   # identity/native-generation cleanup before restoring the damaged history.
   if (domain/'boot-ended.receipt').exists():self.stop_created_generation()
   for p,data,mode in removed:p.write_bytes(data);p.chmod(mode)
if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([BootRollback('test_tail_rollback_after_ready_is_preserved_and_refused')]))
 raise SystemExit(not r.wasSuccessful())
