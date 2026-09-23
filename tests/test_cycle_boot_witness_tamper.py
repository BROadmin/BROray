"""A new boot must not accept missing or changed independent ledger witnesses."""
import hashlib,json,unittest
from test_cycle_running_boot_resume import CycleRunningBootResume

class BootWitnessTamper(CycleRunningBootResume):
 def witness_target(self):
  gid=self.e['stoppedGeneration'];w=self.updater/'starts'/gid/'ledger-witnesses'
  self.assertTrue(w.is_dir());items=sorted(w.glob('revision-*.json'))
  self.assertTrue(items,'live exported generation must contain revision witnesses')
  return gid,w,items[-1]
 def refuse_preserving(self,mutate):
  gid,w,target=self.witness_target();original=target.read_bytes();mode=target.stat().st_mode&0o777
  generations={p.name for p in (self.updater/'generations').iterdir() if p.is_dir()}
  mutate(target);tampered=self.files(self.updater)
  try:
   r=self.init('start');print('BOOT_WITNESS_REFUSAL '+json.dumps({'rc':r.returncode,'stderr':r.stderr,'target':target.name}),flush=True)
   self.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
   self.assertEqual(self.files(self.updater),tampered,'refusal must not rewrite or repair tampered evidence')
   self.assertEqual({p.name for p in (self.updater/'generations').iterdir() if p.is_dir()},generations)
   self.assertFalse((self.updater/'generations'/gid/'boot-ended.receipt').exists())
  finally:
   target.write_bytes(original);target.chmod(mode)
 def test_missing_last_witness_refuses_without_new_generation(self):
  self.refuse_preserving(lambda p:p.unlink())
 def test_changed_last_witness_refuses_without_repair(self):
  self.refuse_preserving(lambda p:p.write_bytes(b'FOREIGN-WITNESS\n'))

if __name__=='__main__':
 names=['test_missing_last_witness_refuses_without_new_generation','test_changed_last_witness_refuses_without_repair']
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(BootWitnessTamper(n) for n in names))
 raise SystemExit(not r.wasSuccessful())
