"""Deterministic syscall boundaries, only fixture processes/files in offline VM."""
import ctypes,json,os,signal,subprocess,time,unittest
from test_generation_bootguard import BootGuard,FILES
from test_generation_birth_crash import Registers,trace,traced_start
from test_generation_migration_crash import trace_string

class GuardCrash(BootGuard):
 def intercepted(self,match,change=None):
  p=subprocess.Popen(self.guard_args(),preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  self.processes.append(p);_,status=os.waitpid(p.pid,0);self.assertTrue(os.WIFSTOPPED(status));trace(0x4200,p.pid,0,1)
  entering=True;deadline=time.monotonic()+12;matched=False;at=None
  try:
   while time.monotonic()<deadline:
    trace(24,p.pid);_,status=os.waitpid(p.pid,0)
    self.assertTrue(os.WIFSTOPPED(status),f'exited before selected boundary {status}')
    if os.WSTOPSIG(status)!=(signal.SIGTRAP|0x80):continue
    regs=Registers();trace(12,p.pid,0,ctypes.addressof(regs))
    if match(p.pid,regs,entering):matched=True;at={'syscall':regs.orig_rax,'entry':entering};break
    entering=not entering
   self.assertTrue(matched,'requested syscall boundary not reached')
   if change is None:
    os.kill(p.pid,signal.SIGKILL)
    while True:
     _,status=os.waitpid(p.pid,0)
     if os.WIFSIGNALED(status) or os.WIFEXITED(status):break
     trace(7,p.pid,0,signal.SIGKILL)
    p.returncode=-signal.SIGKILL
   else:
    change();trace(17,p.pid,0,0);p.wait(timeout=5)
   out,err=p.communicate(timeout=3)
   print('BOOT_GUARD_SYSCALL_RESULT '+json.dumps({'boundary':at,'exit':p.returncode,'stdout':out.decode(),'stderr':err.decode()}),flush=True)
   return p.returncode,out,err
  finally:
   if p.returncode is None:
    os.kill(p.pid,signal.SIGKILL)
    try:os.waitpid(p.pid,0)
    except ChildProcessError:pass
    p.returncode=-signal.SIGKILL
   p.stdout.close();p.stderr.close()
 def move_match(self,index,kind,when):
  wanted=FILES[index].split('/')[-1] if kind=='old' else f'.broray-bg-{self.intent_sha}-{index}.candidate'
  def check(pid,r,entry):
   return r.orig_rax==316 and entry==(when=='before') and trace_string(pid,r.rsi)==wanted
  return check
 def crash_move(self,index,kind,when):
  self.intercepted(self.move_match(index,kind,when))
  before=self.inventory(self.home);r=self.guard();self.assertNotEqual(r.returncode,0);self.assertEqual(self.inventory(self.home),before)
  self.assertFalse((self.guards/'staged.receipt').exists())
  for i,body in self.original.items():
   self.assertTrue(any(p.is_file() and not p.is_symlink() and p.read_bytes()==body for p in self.home.rglob('*')),f'original bytes lost: {i}')
 def test_foreign_original_at_move_is_preserved(self):
  p=self.live_root/FILES[1]
  rc,out,err=self.intercepted(self.move_match(1,'old','before'),lambda:p.write_bytes(b'FOREIGN-DISPLACED'))
  self.assertNotEqual(rc,0);self.assertFalse((self.guards/'staged.receipt').exists())
  displaced=p.parent/f'.broray-bg-{self.intent_sha}-1.previous';self.assertEqual(displaced.read_bytes(),b'FOREIGN-DISPLACED')
  self.assertFalse(p.exists());self.assertEqual((self.live_root/FILES[5]).read_bytes(),self.original[5])
 def test_foreign_destination_at_install_is_never_overwritten(self):
  p=self.live_root/FILES[1]
  rc,out,err=self.intercepted(self.move_match(1,'new','before'),lambda:p.write_bytes(b'FOREIGN-DESTINATION'))
  self.assertNotEqual(rc,0);self.assertEqual(p.read_bytes(),b'FOREIGN-DESTINATION');self.assertFalse((self.guards/'staged.receipt').exists())
 def test_corrupt_anchor_during_install_is_preserved(self):
  anchor=self.guards/'intent.record'
  rc,out,err=self.intercepted(self.move_match(1,'new','before'),lambda:anchor.write_bytes(b'{broken'))
  self.assertNotEqual(rc,0);self.assertEqual(anchor.read_bytes(),b'{broken');self.assertFalse((self.guards/'staged.receipt').exists())
 def test_prior_displaced_inode_change_prevents_final_receipt(self):
  old=(self.live_root/FILES[1]).parent/f'.broray-bg-{self.intent_sha}-1.previous'
  rc,out,err=self.intercepted(self.move_match(5,'new','before'),lambda:old.write_bytes(b'LATE-OLD-WRITER'))
  self.assertNotEqual(rc,0,'a late legacy writer invalidated old inode evidence before publication')
  self.assertEqual(old.read_bytes(),b'LATE-OLD-WRITER');self.assertFalse((self.guards/'staged.receipt').exists())

for index in [1,5]:
 for kind in ['old','new']:
  for when in ['before','after']:
   def test(self,index=index,kind=kind,when=when):self.crash_move(index,kind,when)
   setattr(GuardCrash,f'test_crash_entry_{index}_{kind}_{when}',test)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(GuardCrash(n) for n in GuardCrash.__dict__ if n.startswith('test_')))
 raise SystemExit(not result.wasSuccessful())
